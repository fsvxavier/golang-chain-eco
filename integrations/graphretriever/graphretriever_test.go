package graphretriever_test

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/integrations/graphretriever"
)

func TestTraversal(t *testing.T) {
	ctx := context.Background()
	vs := vectorstores.NewMemory(embeddings.Fake{})
	_, err := vs.AddDocuments(ctx, []documents.Document{
		{ID: "a", PageContent: "alpha intro", Metadata: map[string]any{"id": "a", "mentions": []any{"b", "c"}}},
		{ID: "b", PageContent: "beta details", Metadata: map[string]any{"id": "b", "mentions": []any{"d"}}},
		{ID: "c", PageContent: "gamma details", Metadata: map[string]any{"id": "c"}},
		{ID: "d", PageContent: "delta deep", Metadata: map[string]any{"id": "d"}},
		{ID: "z", PageContent: "unrelated zeta", Metadata: map[string]any{"id": "z"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	edges := []graphretriever.Edge{{From: "mentions", To: "id"}}

	// depth 1 from "alpha intro": a plus its direct mentions b and c, never z or d
	r := &graphretriever.Retriever{Store: vs, Edges: edges, K: 10, StartK: 1, Depth: 1}
	docs, err := r.Invoke(ctx, "alpha intro")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range docs {
		got[d.ID] = true
	}
	if !got["a"] || !got["b"] || !got["c"] || got["d"] || got["z"] {
		t.Fatalf("depth 1: %v", got)
	}

	// depth 2 reaches d through b
	r.Depth = 2
	docs, _ = r.Invoke(ctx, "alpha intro")
	got = map[string]bool{}
	for _, d := range docs {
		got[d.ID] = true
	}
	if !got["d"] || got["z"] {
		t.Fatalf("depth 2: %v", got)
	}

	// K caps the result and the start document comes first
	r.K = 2
	docs, _ = r.Invoke(ctx, "alpha intro")
	if len(docs) != 2 || docs[0].ID != "a" {
		t.Fatalf("K cap: %+v", docs)
	}
}
