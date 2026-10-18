package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Couchbase stores vectors as JSON documents in a bucket and ranks them with
// SQL++ (N1QL) array arithmetic: an exact cosine scan that needs no vector
// index and works on every Couchbase 7.x, including Community. Use a
// dedicated bucket (or scope.collection via Keyspace) per store.
type Couchbase struct {
	QueryURL string // query service, e.g. http://localhost:8093
	Username string
	Password string
	Keyspace string // `bucket` or `bucket`.`scope`.`collection`, backticked as needed (default `default`)
	HTTP     *http.Client
}

func (c *Couchbase) keyspace() (string, error) {
	k := c.Keyspace
	if k == "" {
		k = "default"
	}
	for _, part := range strings.Split(k, ".") {
		part = strings.Trim(part, "`")
		if part == "" || strings.ContainsAny(part, "`;\\\n") {
			return "", fmt.Errorf("couchbase: invalid keyspace %q", c.Keyspace)
		}
	}
	return "`" + strings.Join(strings.Split(strings.ReplaceAll(k, "`", ""), "."), "`.`") + "`", nil
}

func (c *Couchbase) query(ctx context.Context, stmt string, params map[string]any) ([]json.RawMessage, error) {
	body := map[string]any{"statement": stmt}
	for k, v := range params {
		body["$"+k] = v
	}
	b, _ := json.Marshal(body)
	raw, err := post(ctx, c.HTTP, strings.TrimRight(c.QueryURL, "/")+"/query/service", "application/json", b, c.Username, c.Password, nil)
	var out struct {
		Results []json.RawMessage
		Errors  []struct {
			Code int
			Msg  string
		}
	}
	if jerr := json.Unmarshal(raw, &out); jerr == nil && len(out.Errors) > 0 {
		return nil, fmt.Errorf("couchbase: %d: %s", out.Errors[0].Code, out.Errors[0].Msg)
	}
	if err != nil {
		return nil, err
	}
	return out.Results, nil
}

func (c *Couchbase) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	ks, err := c.keyspace()
	if err != nil {
		return err
	}
	if _, err := c.query(ctx, "CREATE PRIMARY INDEX IF NOT EXISTS ON "+ks, nil); err != nil {
		return err
	}
	for _, r := range recs {
		md := r.Metadata
		if md == nil {
			md = map[string]any{}
		}
		if _, err := c.query(ctx, "UPSERT INTO "+ks+" (KEY, VALUE) VALUES ($id, {\"type\":\"lcg_chunk\",\"text\":$text,\"metadata\":$md,\"embedding\":$emb})",
			map[string]any{"id": r.ID, "text": r.Text, "md": md, "emb": r.Vector}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Couchbase) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	ks, err := c.keyspace()
	if err != nil {
		return nil, err
	}
	rows, err := c.query(ctx, fmt.Sprintf(`SELECT META(d).id AS id, d.text, d.metadata, d.embedding,
		ARRAY_SUM(ARRAY v * $q[i] FOR i:v IN d.embedding END) /
		(SQRT(ARRAY_SUM(ARRAY v * v FOR v IN d.embedding END)) * SQRT(ARRAY_SUM(ARRAY v * v FOR v IN $q END))) AS score
		FROM %s d WHERE d.type = "lcg_chunk" ORDER BY score DESC LIMIT %d`, ks, k), map[string]any{"q": vec})
	if err != nil {
		if strings.Contains(err.Error(), "12003") || strings.Contains(err.Error(), "no index available") {
			return nil, nil // keyspace missing or never written
		}
		return nil, err
	}
	hits := make([]Hit, 0, len(rows))
	for _, r := range rows {
		var row struct {
			ID        string         `json:"id"`
			Text      string         `json:"text"`
			Metadata  map[string]any `json:"metadata"`
			Embedding []float32      `json:"embedding"`
			Score     float64        `json:"score"`
		}
		if err := json.Unmarshal(r, &row); err != nil {
			return nil, err
		}
		hits = append(hits, Hit{Record{row.ID, row.Embedding, row.Text, row.Metadata}, row.Score})
	}
	return hits, nil
}

func (c *Couchbase) Delete(ctx context.Context, ids []string) error {
	ks, err := c.keyspace()
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = c.query(ctx, "DELETE FROM "+ks+" d WHERE META(d).id IN $ids", map[string]any{"ids": ids})
	return err
}
