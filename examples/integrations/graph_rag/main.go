// graph_rag: retrieval that follows links. The best match for the question
// is one page; its "links" metadata pulls in related pages that a plain
// similarity search would miss. Runs offline (fake embeddings).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/integrations/graphretriever"
)

func main() {
	ctx := context.Background()
	vs := vectorstores.NewMemory(embeddings.Fake{Dim: 128})
	pages := []documents.Document{
		{ID: "intro", PageContent: "goroutines overview", Metadata: map[string]any{"id": "intro", "links": []any{"channels", "scheduler"}}},
		{ID: "channels", PageContent: "channels send values between goroutines", Metadata: map[string]any{"id": "channels", "links": []any{"select"}}},
		{ID: "scheduler", PageContent: "the runtime scheduler multiplexes goroutines onto threads", Metadata: map[string]any{"id": "scheduler"}},
		{ID: "select", PageContent: "select waits on several channel operations", Metadata: map[string]any{"id": "select"}},
		{ID: "css", PageContent: "unrelated page about cascading style sheets", Metadata: map[string]any{"id": "css"}},
	}
	if _, err := vs.AddDocuments(ctx, pages); err != nil {
		log.Fatal(err)
	}

	show := func(title string, r retrievers.Retriever) {
		docs, err := r.Invoke(ctx, "goroutines overview")
		if err != nil {
			log.Fatal(err)
		}
		demo.Title(title)
		for _, d := range docs {
			fmt.Printf("  - %s\n", d.ID)
		}
	}
	show("plain similarity (top 2)", retrievers.FromVectorStore(vs, retrievers.VectorStoreOptions{K: 2}))
	show("graph traversal (start 1, 2 hops)", &graphretriever.Retriever{
		Store: vs, K: 6, StartK: 1, Depth: 2,
		Edges: []graphretriever.Edge{{From: "links", To: "id"}},
	})
}
