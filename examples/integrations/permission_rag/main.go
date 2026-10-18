// permission_rag: a retriever that only returns what the caller may read.
// A stand-in policy server plays the role of Permit.io's PDP here; with
// SpiceDB the code is the same using integrations/spicedb.FilterRetriever.
// Runs offline.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/integrations/permit"
)

func main() {
	// who may read what (the "policy")
	acl := map[string]map[string]bool{"alice": {"handbook": true, "salaries": true}, "bob": {"handbook": true}}
	pdp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			User     struct{ Key string }
			Resource struct{ Key string }
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		json.NewEncoder(w).Encode(map[string]bool{"allow": acl[q.User.Key][q.Resource.Key]})
	}))
	defer pdp.Close()

	ctx := context.Background()
	vs := vectorstores.NewMemory(embeddings.Fake{Dim: 128})
	_, err := vs.AddDocuments(ctx, []documents.Document{
		{PageContent: "company handbook: working hours and holidays", Metadata: map[string]any{"doc": "handbook"}},
		{PageContent: "salary bands for every level at the company", Metadata: map[string]any{"doc": "salaries"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	inner := retrievers.FromVectorStore(vs, retrievers.VectorStoreOptions{K: 5})
	idOf := func(d documents.Document) string { s, _ := d.Metadata["doc"].(string); return s }

	for _, user := range []string{"alice", "bob"} {
		r := permit.FilterRetriever(inner, &permit.Client{PDPURL: pdp.URL, APIKey: "demo"}, user, "read", "doc", idOf)
		docs, err := r.Invoke(ctx, "company policies")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s sees %d document(s):\n", user, len(docs))
		for _, d := range docs {
			fmt.Printf("  - %s\n", idOf(d))
		}
	}
}
