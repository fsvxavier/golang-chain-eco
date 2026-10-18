package vectara_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectara"
)

func TestSearchAndIndex(t *testing.T) {
	var gotKey, gotPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath = r.Header.Get("x-api-key"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/v2/corpora/kb/query" {
			w.Write([]byte(`{"search_results":[{"text":"cats purr","score":0.8,"document_id":"d1","part_metadata":{"page":2},"document_metadata":{"src":"x"}}]}`))
			return
		}
		w.WriteHeader(201)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := &vectara.Client{APIKey: "vk", CorpusKey: "kb", BaseURL: srv.URL}
	ctx := context.Background()
	if err := c.Index(ctx, "d1", "cats purr", map[string]any{"src": "x"}); err != nil || gotPath != "/v2/corpora/kb/documents" || body["id"] != "d1" || body["type"] != "core" {
		t.Fatal(err, gotPath, body)
	}
	docs, err := c.Retriever(3).Invoke(ctx, "what do cats do")
	if err != nil || len(docs) != 1 || docs[0].PageContent != "cats purr" || docs[0].Metadata["page"].(float64) != 2 || docs[0].Metadata["src"] != "x" || gotKey != "vk" {
		t.Fatalf("%+v %v", docs, err)
	}
	if body["query"] != "what do cats do" || body["search"].(map[string]any)["limit"].(float64) != 3 {
		t.Fatal(body)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 403) }))
	defer bad.Close()
	if _, err := (&vectara.Client{CorpusKey: "kb", BaseURL: bad.URL}).Search(ctx, "q", 1); err == nil {
		t.Fatal("403 must fail")
	}
}
