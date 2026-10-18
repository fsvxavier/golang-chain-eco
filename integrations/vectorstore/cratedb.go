package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CrateDB stores vectors in a FLOAT_VECTOR column and searches with
// knn_match (HNSW). CrateDB ranks by euclidean similarity, so Score is the
// engine's _score (higher is closer), not a cosine value.
type CrateDB struct {
	URL      string // http://localhost:4200
	Username string
	Password string
	Table    string // default langchain_embedding
	HTTP     *http.Client
}

func (c *CrateDB) table() (string, error) {
	t := c.Table
	if t == "" {
		t = "langchain_embedding"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("cratedb: invalid table name %q", t)
	}
	return t, nil
}

func (c *CrateDB) sql(ctx context.Context, stmt string, args ...any) ([][]json.RawMessage, error) {
	if args == nil {
		args = []any{} // CrateDB rejects "args": null
	}
	body, _ := json.Marshal(map[string]any{"stmt": stmt, "args": args})
	raw, err := post(ctx, c.HTTP, strings.TrimRight(c.URL, "/")+"/_sql", "application/json", body, c.Username, c.Password, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Rows [][]json.RawMessage `json:"rows"`
	}
	return out.Rows, json.Unmarshal(raw, &out)
}

func (c *CrateDB) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	t, err := c.table()
	if err != nil {
		return err
	}
	if _, err := c.sql(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, content TEXT, metadata OBJECT(DYNAMIC), embedding FLOAT_VECTOR(%d))", t, len(recs[0].Vector))); err != nil {
		return err
	}
	for _, r := range recs {
		md := r.Metadata
		if md == nil {
			md = map[string]any{}
		}
		if _, err := c.sql(ctx, fmt.Sprintf("INSERT INTO %s (id, content, metadata, embedding) VALUES (?, ?, ?, ?) ON CONFLICT (id) DO UPDATE SET content = excluded.content, metadata = excluded.metadata, embedding = excluded.embedding", t),
			r.ID, r.Text, md, r.Vector); err != nil {
			return err
		}
	}
	_, err = c.sql(ctx, "REFRESH TABLE "+t)
	return err
}

func (c *CrateDB) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := c.table()
	if err != nil {
		return nil, err
	}
	rows, err := c.sql(ctx, fmt.Sprintf("SELECT id, content, metadata, embedding, _score FROM %s WHERE knn_match(embedding, ?, ?) ORDER BY _score DESC LIMIT %d", t, k), vec, k)
	if err != nil {
		if strings.Contains(err.Error(), "RelationUnknown") {
			return nil, nil
		}
		return nil, err
	}
	var hits []Hit
	for _, r := range rows {
		var h Hit
		_ = json.Unmarshal(r[0], &h.ID)
		_ = json.Unmarshal(r[1], &h.Text)
		_ = json.Unmarshal(r[2], &h.Metadata)
		_ = json.Unmarshal(r[3], &h.Vector)
		_ = json.Unmarshal(r[4], &h.Score)
		hits = append(hits, h)
	}
	return hits, nil
}

func (c *CrateDB) Delete(ctx context.Context, ids []string) error {
	t, err := c.table()
	if err != nil || len(ids) == 0 {
		return err
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if _, err := c.sql(ctx, fmt.Sprintf("DELETE FROM %s WHERE id IN (%s)", t, ph), args...); err != nil {
		return err
	}
	_, err = c.sql(ctx, "REFRESH TABLE "+t)
	return err
}
