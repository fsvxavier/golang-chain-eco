package permit_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/integrations/permit"
)

func TestFilter(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		var b struct {
			User     struct{ Key string }
			Action   string
			Resource struct{ Type, Key, Tenant string }
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b.User.Key != "alice" || b.Action != "read" || b.Resource.Type != "doc" || b.Resource.Tenant != "default" {
			http.Error(w, "bad request", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"allow": b.Resource.Key != "secret"})
	}))
	defer srv.Close()
	c := &permit.Client{PDPURL: srv.URL, APIKey: "pk"}
	inner := retrievers.Func(func(context.Context, string) ([]documents.Document, error) {
		return []documents.Document{
			{PageContent: "ok", Metadata: map[string]any{"id": "open"}},
			{PageContent: "no", Metadata: map[string]any{"id": "secret"}},
			{PageContent: "anon"},
		}, nil
	})
	r := permit.FilterRetriever(inner, c, "alice", "read", "doc", func(d documents.Document) string { s, _ := d.Metadata["id"].(string); return s })
	docs, err := r.Invoke(context.Background(), "q")
	if err != nil || len(docs) != 1 || docs[0].PageContent != "ok" || auth != "Bearer pk" {
		t.Fatalf("%+v %v %s", docs, err, auth)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 500) }))
	defer bad.Close()
	r = permit.FilterRetriever(inner, &permit.Client{PDPURL: bad.URL}, "alice", "read", "doc", func(documents.Document) string { return "x" })
	if docs, err := r.Invoke(context.Background(), "q"); err == nil || len(docs) != 0 {
		t.Fatalf("PDP failure must fail closed: %+v %v", docs, err)
	}
}
