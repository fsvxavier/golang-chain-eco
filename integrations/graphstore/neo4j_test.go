package graphstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
	"github.com/fsvxavier/golang-chain-eco/kag"
)

// fakeNeo4j records every statement the adapter sends and answers with rows.
type fakeNeo4j struct {
	requests atomic.Int32
	stmts    []string
	params   []map[string]any
	rows     string
	status   int
}

func (f *fakeNeo4j) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Statement  string         `json:"statement"`
			Parameters map[string]any `json:"parameters"`
		}
		_ = json.Unmarshal(body, &in)
		f.stmts = append(f.stmts, in.Statement)
		f.params = append(f.params, in.Parameters)
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = io.WriteString(w, `{"errors":[{"code":"Neo.ClientError.Statement.SyntaxError","message":"boom"}]}`)
			return
		}
		rows := f.rows
		if rows == "" {
			rows = "[]"
		}
		_, _ = fmt.Fprintf(w, `{"data":{"fields":[],"values":%s}}`, rows)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUpsertUsesParameters(t *testing.T) {
	f := &fakeNeo4j{}
	srv := f.serve(t)
	g := &Neo4j{URL: srv.URL, Username: "u", Password: "p"}
	secret := "O'Hara `x` ) DETACH DELETE n // 42"
	if err := g.UpsertEntities(context.Background(), []kag.Entity{{ID: "e1", Type: "Person", Name: secret, Props: map[string]any{"k": secret}, Vector: []float32{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	if len(f.stmts) == 0 {
		t.Fatal("no statement sent")
	}
	for _, s := range f.stmts {
		if strings.Contains(s, secret) || strings.Contains(s, "e1") {
			t.Fatalf("value leaked into statement text: %s", s)
		}
	}
	found := false
	for _, p := range f.params {
		if strings.Contains(fmt.Sprint(p), secret) {
			found = true
		}
	}
	if !found {
		t.Fatalf("values not sent as parameters: %+v", f.params)
	}
}

func TestRelationTypeValidated(t *testing.T) {
	f := &fakeNeo4j{}
	srv := f.serve(t)
	g := &Neo4j{URL: srv.URL}
	bad := []string{"X`) DETACH DELETE n //", "knows", "KNOWS-X", ""}
	for _, typ := range bad {
		err := g.UpsertRelations(context.Background(), []kag.Relation{
			{From: "a", Type: "KNOWS", To: "b"},
			{From: "a", Type: typ, To: "b"},
		})
		if err == nil {
			t.Fatalf("type %q accepted", typ)
		}
	}
	if n := f.requests.Load(); n != 0 {
		t.Fatalf("hostile relation type reached the server: %d requests", n)
	}
	if _, err := g.Path(context.Background(), "a", "b", 2); err != nil {
		t.Fatal(err)
	}
}

func TestLabelValidated(t *testing.T) {
	f := &fakeNeo4j{}
	srv := f.serve(t)
	g := &Neo4j{URL: srv.URL, Label: "Entity) DETACH DELETE n //"}
	if err := g.UpsertEntities(context.Background(), []kag.Entity{{ID: "x", Name: "x"}}); err == nil {
		t.Fatal("hostile label accepted")
	}
	if f.requests.Load() != 0 {
		t.Fatal("request sent with invalid label")
	}
}

func TestNeighborsParsesRows(t *testing.T) {
	f := &fakeNeo4j{rows: `[["a","KNOWS","b","{\"w\":1}","doc1"],["c","KNOWS","a",null,"doc2"]]`}
	srv := f.serve(t)
	g := &Neo4j{URL: srv.URL}
	rels, err := g.Neighbors(context.Background(), "a", nil, kag.Both)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 {
		t.Fatalf("got %+v", rels)
	}
	if rels[0].From != "a" || rels[0].To != "b" || rels[0].Type != "KNOWS" || rels[0].Source != "doc1" || rels[0].Props["w"] != float64(1) {
		t.Fatalf("row 0: %+v", rels[0])
	}
	if rels[1].From != "c" || rels[1].Props != nil {
		t.Fatalf("row 1: %+v", rels[1])
	}
}

func TestHTTPErrorSurfaces(t *testing.T) {
	f := &fakeNeo4j{status: http.StatusBadRequest}
	srv := f.serve(t)
	g := &Neo4j{URL: srv.URL}
	if _, _, err := g.Entity(context.Background(), "a"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want server error, got %v", err)
	}
}

func TestPathRejectsBadHops(t *testing.T) {
	f := &fakeNeo4j{}
	srv := f.serve(t)
	g := &Neo4j{URL: srv.URL}
	if paths, err := g.Path(context.Background(), "a", "b", 0); err != nil || paths != nil || f.requests.Load() != 0 {
		t.Fatalf("maxHops 0: %v %v requests=%d", paths, err, f.requests.Load())
	}
	if _, err := g.Path(context.Background(), "a", "b", 2); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stmts[0], "*1..2]") {
		t.Fatalf("hops not interpolated as literal: %s", f.stmts[0])
	}
}

// TestNeo4jReal runs the shared graph suite against a live Neo4j Query API.
func TestNeo4jReal(t *testing.T) {
	url := os.Getenv("NEO4J_URL")
	if url == "" {
		t.Skip("NEO4J_URL not set")
	}
	label := fmt.Sprintf("G%d", time.Now().UnixNano())
	g := &Neo4j{URL: url, Username: os.Getenv("NEO4J_USER"), Password: os.Getenv("NEO4J_PASS"),
		Database: os.Getenv("NEO4J_DB"), Label: label}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = g.run(ctx, fmt.Sprintf("MATCH (n:%s) DETACH DELETE n", label), nil)
		_, _ = g.run(ctx, fmt.Sprintf("DROP CONSTRAINT %s IF EXISTS", constraintName(label)), nil)
	})
	conformance.Graph(t, g)
}
