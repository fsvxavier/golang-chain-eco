package vectorstore_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// ClickHouse / CrateDB / Solr / ArcadeDB real-service tests.
func TestClickHouseReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.ClickHouse{URL: need(t, "CLICKHOUSE_URL"), Username: os.Getenv("CLICKHOUSE_USER"), Password: os.Getenv("CLICKHOUSE_PASS"),
		Table: fmt.Sprintf("t%d", time.Now().UnixNano())})
}

func TestCrateDBReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.CrateDB{URL: need(t, "CRATEDB_URL"), Table: fmt.Sprintf("t%d", time.Now().UnixNano())})
}

func TestSolrReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.Solr{URL: need(t, "SOLR_URL"), Core: os.Getenv("SOLR_CORE")})
}

func TestArcadeDBReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.ArcadeDB{URL: need(t, "ARCADEDB_URL"), Database: fmt.Sprintf("d%d", time.Now().UnixNano()),
		Username: "root", Password: os.Getenv("ARCADEDB_PASS")})
}

func TestInfinispanReal(t *testing.T) {
	conformance.Vector(t, &vectorstore.Infinispan{URL: need(t, "INFINISPAN_URL"), Username: os.Getenv("INFINISPAN_USER"), Password: os.Getenv("INFINISPAN_PASS"),
		Cache: fmt.Sprintf("c%d", time.Now().UnixNano())})
}

// COUCHBASE_URL=http://localhost:8091 (admin API) with COUCHBASE_USER/PASS; the test initialises a
// fresh Community node (services, credentials, bucket) and queries port 8093.
func TestCouchbaseReal(t *testing.T) {
	admin := need(t, "COUCHBASE_URL")
	user, pass := os.Getenv("COUCHBASE_USER"), os.Getenv("COUCHBASE_PASS")
	form := func(path string, kv ...string) {
		t.Helper()
		v := url.Values{}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		req, _ := http.NewRequest("POST", admin+path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(user, pass)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode/100 != 2 && !strings.Contains(string(b), "already exists") && !strings.Contains(string(b), "Unauthorized") && !strings.Contains(string(b), "is provisioned") { // re-runs hit already-initialised nodes
			t.Fatalf("%s: %d %s", path, resp.StatusCode, b)
		}
	}
	form("/pools/default", "memoryQuota", "256", "indexMemoryQuota", "256")
	form("/node/controller/setupServices", "services", "kv,n1ql,index")
	form("/settings/indexes", "storageMode", "forestdb") // required before any GSI index; Community only allows forestdb
	form("/settings/web", "port", "8091", "username", user, "password", pass)
	if r, err := http.Get(strings.Replace(admin, "://", "://"+user+":"+pass+"@", 1) + "/pools/default/buckets/lcg"); err != nil || r.StatusCode != 200 {
		form("/pools/default/buckets", "name", "lcg", "ramQuota", "128", "flushEnabled", "1")
	}
	be := &vectorstore.Couchbase{QueryURL: strings.Replace(admin, ":8091", ":8093", 1), Username: user, Password: pass, Keyspace: "lcg"}
	if u := os.Getenv("COUCHBASE_QUERY_URL"); u != "" {
		be.QueryURL = u
	}
	// the bucket takes a moment to become queryable
	var err error
	for range 40 {
		if err = be.Upsert(context.Background(), []vectorstore.Record{{ID: "warm", Vector: []float32{1, 0, 0}}}); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = be.Delete(context.Background(), []string{"warm", "a", "x", "y", "z"}) // leftovers of earlier runs
	conformance.Vector(t, be)
}

// Azure AI Search: contract test against a fake service (no live Azure here).
func TestAzureAISearch(t *testing.T) {
	var created bool
	srv, log := fake(t, map[string]string{
		"GET /indexes/docs?":             "404",
		"PUT /indexes/docs":              `{}`,
		"POST /indexes/docs/docs/index":  `{"value":[{"key":"YQ","status":true}]}`,
		"POST /indexes/docs/docs/search": `{"value":[{"@search.score":0.91,"rid":"a","content":"cats","metadata":"{\"k\":\"v\"}","embedding":[1,0]}]}`,
	})
	defer srv.Close()
	be := &vectorstore.AzureAISearch{Endpoint: srv.URL, APIKey: "k", Index: "docs"}
	ctx := context.Background()
	// GET on the index answers 404, so Upsert creates it with PUT
	if err := be.Upsert(ctx, []vectorstore.Record{{ID: "a", Vector: []float32{1, 0}, Text: "cats", Metadata: map[string]any{"k": "v"}}}); err != nil {
		t.Fatal(err)
	}
	hits, err := be.Query(ctx, []float32{1, 0}, 3)
	if err != nil || len(hits) != 1 || hits[0].ID != "a" || hits[0].Score != 0.91 || hits[0].Metadata["k"] != "v" {
		t.Fatalf("%+v %v", hits, err)
	}
	if err := be.Delete(ctx, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range log() {
		if r.method == "PUT" && strings.Contains(r.body, `"hnsw"`) && strings.Contains(r.body, `"dimensions":2`) {
			created = true
		}
	}
	if !created {
		t.Fatalf("index definition not sent: %+v", log())
	}
	last := log()[len(log())-1]
	if !strings.Contains(last.body, `"delete"`) || !strings.Contains(last.body, `"YQ"`) || !strings.Contains(last.path, "api-version=2024-07-01") {
		t.Fatalf("%+v", last)
	}
}
