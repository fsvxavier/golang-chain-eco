package vectorstore_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

// Real-service tests; each skips unless its variable is set:
//
//	TYPESENSE_URL (+TYPESENSE_KEY), NEO4J_URL (+NEO4J_USER/NEO4J_PASS), SURREAL_URL (+SURREAL_USER/SURREAL_PASS).
func need(t *testing.T, name string) string {
	v := os.Getenv(name)
	if v == "" {
		t.Skip(name + " not set")
	}
	return v
}

func TestTypesenseReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.Typesense{URL: need(t, "TYPESENSE_URL"), APIKey: os.Getenv("TYPESENSE_KEY"),
		Collection: fmt.Sprintf("lcg_%d", time.Now().UnixNano())})
}

func TestNeo4jReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.Neo4j{URL: need(t, "NEO4J_URL"), Username: os.Getenv("NEO4J_USER"), Password: os.Getenv("NEO4J_PASS"),
		Label: fmt.Sprintf("T%d", time.Now().UnixNano()), Index: fmt.Sprintf("i%d", time.Now().UnixNano())})
}

func TestSurrealReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.SurrealDB{URL: need(t, "SURREAL_URL"), Namespace: "t", Database: "t",
		Username: os.Getenv("SURREAL_USER"), Password: os.Getenv("SURREAL_PASS"), Table: fmt.Sprintf("c%d", time.Now().UnixNano())})
}

func TestUpstash(t *testing.T) {
	srv, log := fake(t, map[string]string{
		"POST /upsert/ns": `{"result":"Success"}`,
		"POST /query/ns":  `{"result":[{"id":"a","score":0.8,"vector":[1,0],"metadata":{"text":"cats","metadata":{"k":"v"}}}]}`,
		"POST /delete/ns": `{"result":{"deleted":1}}`,
	})
	defer srv.Close()
	be := &vectorstore.Upstash{URL: srv.URL, Token: "tok", Namespace: "ns", HTTP: http.DefaultClient}
	ctx := context.Background()
	if err := be.Upsert(ctx, []vectorstore.Record{{ID: "a", Vector: []float32{1, 0}, Text: "cats"}}); err != nil {
		t.Fatal(err)
	}
	hits, err := be.Query(ctx, []float32{1, 0}, 2)
	if err != nil || len(hits) != 1 || hits[0].Text != "cats" || hits[0].Metadata["k"] != "v" || hits[0].Score != 0.8 {
		t.Fatalf("%+v %v", hits, err)
	}
	if err := be.Delete(ctx, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if got := log(); len(got) != 3 || !strings.Contains(got[1].body, `"topK":2`) {
		t.Fatalf("%+v", got)
	}
}

func TestTypesense(t *testing.T) {
	srv, log := fake(t, map[string]string{
		"POST /collections/docs/documents/import": `{"success":true}`,
		"POST /collections":                       `{"name":"docs"}`,
		"POST /multi_search":                      `{"results":[{"hits":[{"document":{"id":"a","text":"cats","metadata":"{\"k\":\"v\"}","embedding":[1,0]},"vector_distance":0.25}]}]}`,
		"DELETE /collections/docs/documents":      `{"num_deleted":1}`,
	})
	defer srv.Close()
	be := &vectorstore.Typesense{URL: srv.URL, APIKey: "k", Collection: "docs"}
	ctx := context.Background()
	if err := be.Upsert(ctx, []vectorstore.Record{{ID: "a", Vector: []float32{1, 0}, Text: "cats", Metadata: map[string]any{"k": "v"}}}); err != nil {
		t.Fatal(err)
	}
	hits, err := be.Query(ctx, []float32{1, 0}, 2)
	if err != nil || len(hits) != 1 || hits[0].Score != 0.75 || hits[0].Metadata["k"] != "v" || hits[0].Text != "cats" {
		t.Fatalf("%+v %v", hits, err)
	}
	if err := be.Delete(ctx, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	got := log()
	if len(got) != 4 || !strings.Contains(got[2].body, "k:2") || !strings.Contains(got[3].path, "filter_by=") {
		t.Fatalf("%+v", got)
	}
}

func TestTypesenseImportError(t *testing.T) {
	srv, _ := fake(t, map[string]string{"POST /collections": `{}`,
		"POST /collections/docs/documents/import": `{"success":false,"error":"bad doc"}`})
	defer srv.Close()
	err := (&vectorstore.Typesense{URL: srv.URL, Collection: "docs"}).Upsert(context.Background(), []vectorstore.Record{{ID: "a", Vector: []float32{1}}})
	if err == nil || !strings.Contains(err.Error(), "bad doc") {
		t.Fatal(err)
	}
}
