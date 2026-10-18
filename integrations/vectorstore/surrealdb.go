package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// SurrealDB stores vectors in a table through the HTTP /sql endpoint.
// Search is an exact scan ranked by vector::similarity::cosine, so it works
// on every SurrealDB version; it is O(n) per query.
type SurrealDB struct {
	URL       string // http://localhost:8000
	Namespace string
	Database  string
	Username  string
	Password  string
	Table     string // default "chunk"
	HTTP      *http.Client
}

func (s *SurrealDB) table() (string, error) {
	t := s.Table
	if t == "" {
		t = "chunk"
	}
	if !ident.MatchString(t) {
		return "", fmt.Errorf("surrealdb: invalid table name %q", t)
	}
	return t, nil
}

func (s *SurrealDB) sql(ctx context.Context, q string) ([]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.URL, "/")+"/sql", bytes.NewBufferString(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Surreal-NS", s.Namespace)
	req.Header.Set("Surreal-DB", s.Database)
	req.Header.Set("NS", s.Namespace) // pre-2.0 spelling
	req.Header.Set("DB", s.Database)
	req.SetBasicAuth(s.Username, s.Password)
	hc := s.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("surrealdb: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var stmts []struct {
		Status string
		Result json.RawMessage
	}
	if err := json.Unmarshal(raw, &stmts); err != nil {
		return nil, fmt.Errorf("surrealdb: bad response: %s", strings.TrimSpace(string(raw)))
	}
	out := make([]json.RawMessage, len(stmts))
	for i, st := range stmts {
		if st.Status != "OK" {
			return nil, fmt.Errorf("surrealdb: %s", strings.TrimSpace(string(st.Result)))
		}
		out[i] = st.Result
	}
	return out, nil
}

func jstr(v any) string { b, _ := json.Marshal(v); return string(b) }

func (s *SurrealDB) Upsert(ctx context.Context, recs []Record) error {
	t, err := s.table()
	if err != nil {
		return err
	}
	if !ident.MatchString(s.Namespace) || !ident.MatchString(s.Database) {
		return fmt.Errorf("surrealdb: invalid namespace or database name")
	}
	var q strings.Builder
	// create the namespace/database on first use (needs a root/owner user)
	fmt.Fprintf(&q, "DEFINE NAMESPACE IF NOT EXISTS %s;\nUSE NS %s;\nDEFINE DATABASE IF NOT EXISTS %s;\nUSE DB %s;\n", s.Namespace, s.Namespace, s.Database, s.Database)
	fmt.Fprintf(&q, "DEFINE TABLE IF NOT EXISTS %s SCHEMALESS;\n", t)
	for _, r := range recs {
		md := r.Metadata
		if md == nil {
			md = map[string]any{}
		}
		// delete+create instead of UPSERT type::thing(...): that function was
		// renamed in SurrealDB 3, this form works on every version
		fmt.Fprintf(&q, "DELETE %s WHERE rid = %s;\nCREATE %s CONTENT %s;\n", t, jstr(r.ID), t,
			jstr(map[string]any{"rid": r.ID, "text": r.Text, "metadata": md, "embedding": r.Vector}))
	}
	_, err = s.sql(ctx, q.String())
	return err
}

func (s *SurrealDB) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	t, err := s.table()
	if err != nil {
		return nil, err
	}
	res, err := s.sql(ctx, fmt.Sprintf("SELECT rid, text, metadata, embedding, vector::similarity::cosine(embedding, %s) AS score FROM %s ORDER BY score DESC LIMIT %d;", jstr(vec), t, k))
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
		return nil, err
	}
	var rows []struct {
		RID, Text string
		Metadata  map[string]any
		Embedding []float32
		Score     float64
	}
	if len(res) > 0 {
		if err := json.Unmarshal(res[0], &rows); err != nil {
			return nil, err
		}
	}
	hits := make([]Hit, len(rows))
	for i, r := range rows {
		hits[i] = Hit{Record{r.RID, r.Embedding, r.Text, r.Metadata}, r.Score}
	}
	return hits, nil
}

func (s *SurrealDB) Delete(ctx context.Context, ids []string) error {
	t, err := s.table()
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = s.sql(ctx, fmt.Sprintf("DELETE %s WHERE rid IN %s;", t, jstr(ids)))
	return err
}
