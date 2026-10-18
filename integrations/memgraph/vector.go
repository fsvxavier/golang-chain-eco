package memgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
)

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Vector is a vectorstore.Backend on Memgraph's vector index (Memgraph 3+):
// one node per record. Hits carry no embedding (MMR re-embeds the texts).
type Vector struct {
	C        *Client
	Label    string // default "Chunk"
	Index    string // default "lcg_vector"
	Capacity int    // index capacity, default 100000
}

var _ vectorstore.Backend = (*Vector)(nil)

func (v *Vector) names() (label, index string, err error) {
	label, index = v.Label, v.Index
	if label == "" {
		label = "Chunk"
	}
	if index == "" {
		index = "lcg_vector"
	}
	if !identRe.MatchString(label) || !identRe.MatchString(index) {
		return "", "", fmt.Errorf("memgraph: invalid label or index name")
	}
	return label, index, nil
}

func (v *Vector) Upsert(ctx context.Context, recs []vectorstore.Record) error {
	if len(recs) == 0 {
		return nil
	}
	label, index, err := v.names()
	if err != nil {
		return err
	}
	capacity := v.Capacity
	if capacity <= 0 {
		capacity = 100000
	}
	if _, _, err := v.C.Run(ctx, fmt.Sprintf(`CREATE VECTOR INDEX %s ON :%s(embedding) WITH CONFIG {"dimension": %d, "capacity": %d, "metric": "cos"}`,
		index, label, len(recs[0].Vector), capacity), nil); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return err
	}
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, _, err := v.C.Run(ctx, fmt.Sprintf("MERGE (n:%s {id: $id}) SET n.text = $text, n.metadata = $md, n.embedding = $emb", label),
			map[string]any{"id": r.ID, "text": r.Text, "md": string(md), "emb": r.Vector}); err != nil {
			return err
		}
	}
	return nil
}

func (v *Vector) Query(ctx context.Context, vec []float32, k int) ([]vectorstore.Hit, error) {
	_, index, err := v.names()
	if err != nil {
		return nil, err
	}
	_, rows, err := v.C.Run(ctx, "CALL vector_search.search($index, $k, $vec) YIELD node, similarity RETURN node.id, node.text, node.metadata, similarity ORDER BY similarity DESC",
		map[string]any{"index": index, "k": k, "vec": vec})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			return nil, nil
		}
		return nil, err
	}
	var hits []vectorstore.Hit
	for _, r := range rows {
		if len(r) < 4 {
			continue
		}
		var h vectorstore.Hit
		h.ID, _ = r[0].(string)
		h.Text, _ = r[1].(string)
		md, _ := r[2].(string)
		_ = json.Unmarshal([]byte(md), &h.Metadata)
		h.Score, _ = r[3].(float64)
		hits = append(hits, h)
	}
	return hits, nil
}

func (v *Vector) Delete(ctx context.Context, ids []string) error {
	label, _, err := v.names()
	if err != nil || len(ids) == 0 {
		return err
	}
	// Deleted nodes linger in the vector index and break later searches, so
	// drop the indexed property first.
	if _, _, err = v.C.Run(ctx, fmt.Sprintf("MATCH (n:%s) WHERE n.id IN $ids REMOVE n.embedding", label), map[string]any{"ids": ids}); err != nil {
		return err
	}
	_, _, err = v.C.Run(ctx, fmt.Sprintf("MATCH (n:%s) WHERE n.id IN $ids DETACH DELETE n", label), map[string]any{"ids": ids})
	return err
}
