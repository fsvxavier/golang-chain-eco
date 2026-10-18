package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Neo4j stores vectors on nodes with a native vector index, through the
// Query API (HTTP, Neo4j 5.19+ / Aura).
type Neo4j struct {
	URL      string // http://localhost:7474 or the Aura query API host
	Username string
	Password string
	Database string // default "neo4j"
	Label    string // node label, default "Chunk"
	Index    string // vector index name, default "lcg_vector"
	HTTP     *http.Client
}

func (n *Neo4j) names() (label, index string, err error) {
	label, index = n.Label, n.Index
	if label == "" {
		label = "Chunk"
	}
	if index == "" {
		index = "lcg_vector"
	}
	if !ident.MatchString(label) || !ident.MatchString(index) {
		return "", "", fmt.Errorf("neo4j: invalid label or index name")
	}
	return label, index, nil
}

// run executes one Cypher statement and returns its rows.
func (n *Neo4j) run(ctx context.Context, stmt string, params map[string]any) ([][]json.RawMessage, error) {
	db := n.Database
	if db == "" {
		db = "neo4j"
	}
	body, _ := json.Marshal(map[string]any{"statement": stmt, "parameters": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(n.URL, "/")+"/db/"+url.PathEscape(db)+"/query/v2", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(n.Username, n.Password)
	hc := n.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Data struct {
			Values [][]json.RawMessage `json:"values"`
		} `json:"data"`
		Errors []struct{ Code, Message string } `json:"errors"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("neo4j: %s: %s", out.Errors[0].Code, out.Errors[0].Message)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("neo4j: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return out.Data.Values, nil
}

func (n *Neo4j) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	label, index, err := n.names()
	if err != nil {
		return err
	}
	if _, err := n.run(ctx, fmt.Sprintf("CREATE VECTOR INDEX %s IF NOT EXISTS FOR (n:%s) ON (n.embedding) OPTIONS {indexConfig: {`vector.dimensions`: %d, `vector.similarity_function`: 'cosine'}}",
		index, label, len(recs[0].Vector)), nil); err != nil {
		return err
	}
	rows := make([]map[string]any, len(recs))
	for i, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		rows[i] = map[string]any{"id": r.ID, "text": r.Text, "metadata": string(md), "embedding": r.Vector}
	}
	if _, err := n.run(ctx, fmt.Sprintf("UNWIND $rows AS r MERGE (n:%s {id: r.id}) SET n.text = r.text, n.metadata = r.metadata, n.embedding = r.embedding", label),
		map[string]any{"rows": rows}); err != nil {
		return err
	}
	_, err = n.run(ctx, "CALL db.awaitIndexes(60)", nil)
	return err
}

func (n *Neo4j) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	_, index, err := n.names()
	if err != nil {
		return nil, err
	}
	rows, err := n.run(ctx, "CALL db.index.vector.queryNodes($index, $k, $vec) YIELD node, score RETURN node.id, node.text, node.metadata, node.embedding, score",
		map[string]any{"index": index, "k": k, "vec": vec})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchIndex") || strings.Contains(err.Error(), "There is no such vector schema index") {
			return nil, nil
		}
		return nil, err
	}
	var hits []Hit
	for _, r := range rows {
		if len(r) < 5 {
			continue
		}
		var h Hit
		var md string
		_ = json.Unmarshal(r[0], &h.ID)
		_ = json.Unmarshal(r[1], &h.Text)
		_ = json.Unmarshal(r[2], &md)
		_ = json.Unmarshal(r[3], &h.Vector)
		_ = json.Unmarshal(r[4], &h.Score)
		_ = json.Unmarshal([]byte(md), &h.Metadata)
		hits = append(hits, h)
	}
	return hits, nil
}

func (n *Neo4j) Delete(ctx context.Context, ids []string) error {
	label, _, err := n.names()
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = n.run(ctx, fmt.Sprintf("MATCH (n:%s) WHERE n.id IN $ids DETACH DELETE n", label), map[string]any{"ids": ids})
	return err
}
