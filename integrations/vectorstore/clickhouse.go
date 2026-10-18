package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ClickHouse stores vectors in a ReplacingMergeTree table through the HTTP
// interface and ranks them with cosineDistance (exact scan). chDB, the
// in-process ClickHouse engine, runs the same SQL.
type ClickHouse struct {
	URL      string // http://localhost:8123
	Database string // default "default"
	Username string
	Password string
	Table    string // default langchain_embedding
	HTTP     *http.Client
}

func (c *ClickHouse) table() (string, error) {
	t := c.Table
	if t == "" {
		t = "langchain_embedding"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("clickhouse: invalid table name %q", t)
	}
	return t, nil
}

func (c *ClickHouse) exec(ctx context.Context, query string, body []byte) ([]byte, error) {
	q := url.Values{"query": {query}}
	if c.Database != "" {
		q.Set("database", c.Database)
	}
	return post(ctx, c.HTTP, strings.TrimRight(c.URL, "/")+"/?"+q.Encode(), "text/plain", body, c.Username, c.Password, nil)
}

func (c *ClickHouse) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	t, err := c.table()
	if err != nil {
		return err
	}
	if _, err := c.exec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id String, content String, metadata String, embedding Array(Float32), ts DateTime64(3) DEFAULT now64(3)) ENGINE = ReplacingMergeTree(ts) ORDER BY id", t), nil); err != nil {
		return err
	}
	var nd bytes.Buffer
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		line, _ := json.Marshal(map[string]any{"id": r.ID, "content": r.Text, "metadata": string(md), "embedding": r.Vector})
		nd.Write(line)
		nd.WriteByte('\n')
	}
	_, err = c.exec(ctx, "INSERT INTO "+t+" (id, content, metadata, embedding) FORMAT JSONEachRow", nd.Bytes())
	return err
}

func (c *ClickHouse) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := c.table()
	if err != nil {
		return nil, err
	}
	vb, _ := json.Marshal(vec)
	raw, err := c.exec(ctx, fmt.Sprintf("SELECT id, content, metadata, embedding, 1 - cosineDistance(embedding, %s) AS score FROM %s FINAL ORDER BY score DESC LIMIT %d FORMAT JSONEachRow", vb, t, k), nil)
	if err != nil {
		if strings.Contains(err.Error(), "UNKNOWN_TABLE") {
			return nil, nil
		}
		return nil, err
	}
	var hits []Hit
	for _, l := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if len(l) == 0 {
			continue
		}
		var row struct {
			ID, Content, Metadata string
			Embedding             []float32
			Score                 float64
		}
		// column names are lowercase in the output
		var m map[string]json.RawMessage
		if err := json.Unmarshal(l, &m); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(m["id"], &row.ID)
		_ = json.Unmarshal(m["content"], &row.Content)
		_ = json.Unmarshal(m["metadata"], &row.Metadata)
		_ = json.Unmarshal(m["embedding"], &row.Embedding)
		_ = json.Unmarshal(m["score"], &row.Score)
		h := Hit{Record: Record{ID: row.ID, Text: row.Content, Vector: row.Embedding}, Score: row.Score}
		_ = json.Unmarshal([]byte(row.Metadata), &h.Metadata)
		hits = append(hits, h)
	}
	return hits, nil
}

func (c *ClickHouse) Delete(ctx context.Context, ids []string) error {
	t, err := c.table()
	if err != nil || len(ids) == 0 {
		return err
	}
	lits := make([]string, len(ids))
	for i, id := range ids {
		lits[i] = "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(id) + "'"
	}
	_, err = c.exec(ctx, fmt.Sprintf("ALTER TABLE %s DELETE WHERE id IN (%s) SETTINGS mutations_sync = 1", t, strings.Join(lits, ",")), nil)
	return err
}
