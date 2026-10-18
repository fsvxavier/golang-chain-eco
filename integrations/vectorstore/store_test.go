package vectorstore_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
)

var docs = []documents.Document{
	{ID: "a", PageContent: "cats purr", Metadata: map[string]any{"src": "x"}},
	{ID: "b", PageContent: "rockets fly"},
}

type rec struct{ method, path, body string }

// fake serves canned responses by "METHOD path-substring" and records requests.
func fake(t *testing.T, routes map[string]string) (*httptest.Server, func() []rec) {
	var mu sync.Mutex
	var log []rec
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		log = append(log, rec{r.Method, r.URL.RequestURI(), string(b)})
		mu.Unlock()
		best, bestLen := "", -1
		for k := range routes {
			m, sub, _ := strings.Cut(k, " ")
			if m == r.Method && strings.Contains(r.URL.RequestURI(), sub) && len(sub) > bestLen {
				best, bestLen = k, len(sub)
			}
		}
		if bestLen >= 0 {
			if routes[best] == "404" {
				w.WriteHeader(404)
				return
			}
			fmt.Fprint(w, routes[best])
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []rec { mu.Lock(); defer mu.Unlock(); return append([]rec(nil), log...) }
}

func has(log []rec, method, sub string) *rec {
	for i := range log {
		if log[i].method == method && strings.Contains(log[i].path, sub) {
			return &log[i]
		}
	}
	return nil
}

// roundTrip runs add + search + delete against be and checks the hit.
func roundTrip(t *testing.T, be vectorstore.Backend) {
	t.Helper()
	ctx := context.Background()
	s := vectorstore.New(embeddings.Fake{Dim: 8}, be)
	ids, err := s.AddDocuments(ctx, docs)
	if err != nil || len(ids) != 2 || ids[0] != "a" {
		t.Fatalf("add: %v %v", ids, err)
	}
	hits, err := s.SimilaritySearchWithScore(ctx, "cats purr", 2, nil)
	if err != nil || len(hits) == 0 || hits[0].Document.PageContent != "cats purr" || hits[0].Document.ID != "a" {
		t.Fatalf("search: %+v %v", hits, err)
	}
	mm, err := s.MaxMarginalRelevance(ctx, "cats purr", 1, 5, 0.5, nil)
	if err != nil || len(mm) != 1 {
		t.Fatalf("mmr: %v %v", mm, err)
	}
	only, _ := s.SimilaritySearchWithScore(ctx, "cats purr", 2, vectorstores.MetadataEquals(map[string]any{"src": "nope"}))
	if len(only) != 0 {
		t.Fatalf("filter must drop everything: %+v", only)
	}
	if err := s.Delete(ctx, []string{"a"}); err != nil {
		t.Fatal(err)
	}
}

func TestQdrant(t *testing.T) {
	srv, log := fake(t, map[string]string{
		"GET /collections/docs": "404",
		"POST /points/search":   `{"result":[{"score":0.99,"vector":[1,0,0,0,0,0,0,0],"payload":{"_id":"a","page_content":"cats purr","metadata":{"src":"x"}}}]}`,
	})
	roundTrip(t, &vectorstore.Qdrant{URL: srv.URL, Collection: "docs", APIKey: "k"})
	if has(log(), "PUT", "/collections/docs") == nil || has(log(), "PUT", "/points?wait=true") == nil || has(log(), "POST", "/points/delete") == nil {
		t.Fatalf("missing calls: %+v", log())
	}
}

func TestChroma(t *testing.T) {
	srv, log := fake(t, map[string]string{
		"POST /collections":            `{"id":"cid"}`,
		"POST /collections/cid/query":  `{"ids":[["a"]],"documents":[["cats purr"]],"metadatas":[[{"src":"x"}]],"distances":[[0.01]],"embeddings":[[[1,0,0,0,0,0,0,0]]]}`,
		"POST /collections/cid/upsert": `{}`,
	})
	roundTrip(t, &vectorstore.Chroma{URL: srv.URL, Collection: "docs"})
	if r := has(log(), "POST", "/cid/upsert"); r == nil || !strings.Contains(r.body, `"cats purr"`) {
		t.Fatalf("upsert: %+v", log())
	}
}

func TestPinecone(t *testing.T) {
	srv, log := fake(t, map[string]string{
		"POST /query": `{"matches":[{"id":"a","score":0.9,"values":[1,0,0,0,0,0,0,0],"metadata":{"text":"cats purr","src":"x"}}]}`,
	})
	roundTrip(t, &vectorstore.Pinecone{URL: srv.URL, APIKey: "k", Namespace: "ns"})
	if r := has(log(), "POST", "/vectors/upsert"); r == nil || !strings.Contains(r.body, `"namespace":"ns"`) {
		t.Fatalf("%+v", log())
	}
}

func TestElasticsearch(t *testing.T) {
	srv, log := fake(t, map[string]string{
		"HEAD /docs":         "404",
		"POST /docs/_search": `{"hits":{"hits":[{"_id":"a","_score":0.8,"_source":{"vector":[1,0,0,0,0,0,0,0],"text":"cats purr","metadata":{"src":"x"}}}]}}`,
		"POST /_bulk":        `{"errors":false}`,
	})
	roundTrip(t, &vectorstore.Elasticsearch{URL: srv.URL, Index: "docs", APIKey: "k"})
	if r := has(log(), "PUT", "/docs"); r == nil || !strings.Contains(r.body, `"dims":8`) {
		t.Fatalf("index creation: %+v", log())
	}
}

func TestWeaviate(t *testing.T) {
	srv, log := fake(t, map[string]string{
		"GET /v1/schema/Docs": "404",
		"POST /v1/graphql":    `{"data":{"Get":{"Docs":[{"text":"cats purr","metadata":"{\"src\":\"x\"}","docId":"a","_additional":{"distance":0.1,"vector":[1,0,0,0,0,0,0,0]}}]}}}`,
		"POST /v1/batch":      `[{"result":{}}]`,
	})
	roundTrip(t, &vectorstore.Weaviate{URL: srv.URL, Class: "Docs"})
	if has(log(), "POST", "/v1/schema") == nil || has(log(), "DELETE", "/v1/objects/Docs/") == nil {
		t.Fatalf("%+v", log())
	}
}

func TestMilvus(t *testing.T) {
	srv, _ := fake(t, map[string]string{
		"POST /entities/search": `{"code":0,"data":[{"id":"a","distance":0.9,"text":"cats purr","metadata":{"src":"x"},"vector":[1,0,0,0,0,0,0,0]}]}`,
		"POST /collections":     `{"code":0}`,
		"POST /entities":        `{"code":0}`,
	})
	roundTrip(t, &vectorstore.Milvus{URL: srv.URL, Collection: "docs"})
}

// ---- pgvector over a recording fake database/sql driver

type pgDriver struct {
	mu   sync.Mutex
	exec []string
}

func (d *pgDriver) Open(string) (driver.Conn, error) { return &pgConn{d}, nil }

type pgConn struct{ d *pgDriver }

func (c *pgConn) Prepare(q string) (driver.Stmt, error) { return &pgStmt{c.d, q}, nil }
func (c *pgConn) Close() error                          { return nil }
func (c *pgConn) Begin() (driver.Tx, error)             { return pgTx{}, nil }

type pgTx struct{}

func (pgTx) Commit() error   { return nil }
func (pgTx) Rollback() error { return nil }

type pgStmt struct {
	d *pgDriver
	q string
}

func (s *pgStmt) Close() error  { return nil }
func (s *pgStmt) NumInput() int { return -1 }
func (s *pgStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.d.mu.Lock()
	s.d.exec = append(s.d.exec, fmt.Sprint(s.q, args))
	s.d.mu.Unlock()
	return driver.RowsAffected(1), nil
}
func (s *pgStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.d.mu.Lock()
	s.d.exec = append(s.d.exec, fmt.Sprint(s.q, args))
	s.d.mu.Unlock()
	return &pgRows{}, nil
}

type pgRows struct{ done bool }

func (r *pgRows) Columns() []string {
	return []string{"id", "content", "metadata", "embedding", "score"}
}
func (r *pgRows) Close() error { return nil }
func (r *pgRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	b, _ := json.Marshal(map[string]any{"src": "x"})
	copy(dest, []driver.Value{"a", "cats purr", string(b), "[1,0,0,0,0,0,0,0]", 0.97})
	return nil
}

func TestPGVector(t *testing.T) {
	d := &pgDriver{}
	sql.Register("pgfake", d)
	db, _ := sql.Open("pgfake", "")
	roundTrip(t, &vectorstore.PGVector{DB: db, Table: "docs"})
	joined := strings.Join(d.exec, "\n")
	for _, want := range []string{"CREATE EXTENSION IF NOT EXISTS vector", "vector(8)", "ON CONFLICT (id) DO UPDATE", "<=>", "DELETE FROM docs WHERE id IN ($1)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	bad := &vectorstore.PGVector{DB: db, Table: "x; DROP TABLE y"}
	if _, err := vectorstore.New(embeddings.Fake{Dim: 8}, bad).AddDocuments(context.Background(), docs); err == nil {
		t.Fatal("table name must be validated against SQL injection")
	}
}
