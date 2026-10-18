package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
)

// FalkorDB is a vectorstore.Backend on FalkorDB (a graph database that runs
// as a Redis module): one node per record with a native vector index.
// Query vectors come back without embeddings (MMR re-embeds the texts).
type FalkorDB struct {
	C     *Client
	Graph string // default "lcg"
	Label string // default "Chunk"
}

var _ vectorstore.Backend = (*FalkorDB)(nil)

func (f *FalkorDB) names() (string, string) {
	g, l := f.Graph, f.Label
	if g == "" {
		g = "lcg"
	}
	if l == "" {
		l = "Chunk"
	}
	return g, l
}

// lit renders v as a Cypher literal (JSON strings/numbers/lists are valid Cypher).
func lit(v any) string { b, _ := json.Marshal(v); return string(b) }

func (f *FalkorDB) query(ctx context.Context, q string) ([]any, error) {
	g, _ := f.names()
	res, err := f.C.Do(ctx, "GRAPH.QUERY", g, q)
	if err != nil {
		return nil, err
	}
	arr, _ := res.([]any)
	return arr, nil
}

func (f *FalkorDB) Upsert(ctx context.Context, recs []vectorstore.Record) error {
	if len(recs) == 0 {
		return nil
	}
	_, l := f.names()
	if !identRe.MatchString(l) {
		return fmt.Errorf("falkordb: invalid label %q", l)
	}
	// creating an index that already exists is an error we can ignore
	if _, err := f.query(ctx, fmt.Sprintf("CREATE VECTOR INDEX FOR (c:%s) ON (c.embedding) OPTIONS {dimension:%d, similarityFunction:'cosine'}", l, len(recs[0].Vector))); err != nil &&
		!strings.Contains(strings.ToLower(err.Error()), "already") {
		return err
	}
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, err := f.query(ctx, fmt.Sprintf("MERGE (c:%s {id:%s}) SET c.text=%s, c.metadata=%s, c.embedding=vecf32(%s)",
			l, lit(r.ID), lit(r.Text), lit(string(md)), lit(r.Vector))); err != nil {
			return err
		}
	}
	return nil
}

func (f *FalkorDB) Query(ctx context.Context, vec []float32, k int) ([]vectorstore.Hit, error) {
	_, l := f.names()
	if !identRe.MatchString(l) {
		return nil, fmt.Errorf("falkordb: invalid label %q", l)
	}
	res, err := f.query(ctx, fmt.Sprintf("CALL db.idx.vector.queryNodes('%s', 'embedding', %d, vecf32(%s)) YIELD node, score RETURN node.id, node.text, node.metadata, score",
		l, k, lit(vec)))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "index") {
			return nil, nil // nothing indexed yet
		}
		return nil, err
	}
	if len(res) < 2 {
		return nil, nil
	}
	rows, _ := res[1].([]any)
	var hits []vectorstore.Hit
	for _, r := range rows {
		c, _ := r.([]any)
		if len(c) < 4 {
			continue
		}
		var h vectorstore.Hit
		h.ID, _ = c[0].(string)
		h.Text, _ = c[1].(string)
		md, _ := c[2].(string)
		_ = json.Unmarshal([]byte(md), &h.Metadata)
		var dist float64
		fmt.Sscan(fmt.Sprint(c[3]), &dist)
		h.Score = 1 - dist // FalkorDB returns cosine distance
		hits = append(hits, h)
	}
	return hits, nil
}

func (f *FalkorDB) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, l := f.names()
	if !identRe.MatchString(l) {
		return fmt.Errorf("falkordb: invalid label %q", l)
	}
	_, err := f.query(ctx, fmt.Sprintf("MATCH (c:%s) WHERE c.id IN %s DELETE c", l, lit(ids)))
	return err
}
