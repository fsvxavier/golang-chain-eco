package conformance

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

// RAG exercises the rag package over a vector store: incremental indexing,
// retrieval of the right chunk, metadata filters, removal of edited sources
// and a cited answer. newStore must return an empty store on every call.
func RAG(t *testing.T, newStore func() vectorstores.VectorStore) {
	ctx := context.Background()
	store := newStore()
	ix := rag.NewIndexer(store, nil)

	corpus := func(rust string) documents.Loader {
		return documents.LoaderFunc(func(context.Context) ([]documents.Document, error) {
			texts := map[string]string{
				"go.txt":      "Go is a compiled language with goroutines for concurrency.",
				"rust.txt":    rust,
				"cooking.txt": "Bake the bread in a hot oven until golden brown.",
				"sea.txt":     "The sea tides follow the moon and the wind.",
			}
			var out []documents.Document
			for _, src := range []string{"go.txt", "rust.txt", "cooking.txt", "sea.txt"} {
				out = append(out, documents.Document{PageContent: texts[src], Metadata: map[string]any{"source": src}})
			}
			return out, nil
		})
	}
	const oldRust = "Rust guarantees memory safety with its borrow checker."
	const newRust = "Rust is a systems language with zero-cost abstractions."

	// (1) Index 4 sources and retrieve the right chunk.
	st, err := ix.Index(ctx, corpus(oldRust))
	if err != nil || st.Added != 4 || st.Skipped != 0 || st.Removed != 0 {
		t.Fatalf("first index: stats=%+v err=%v, want Added=4", st, err)
	}
	p := &rag.Pipeline{Store: store, Docs: ix.Documents(), K: 2}
	srcs, err := p.Retrieve(ctx, knowledge.Question{Text: "Which language has goroutines for concurrency?"})
	if err != nil || len(srcs) == 0 || srcs[0].Metadata["source"] != "go.txt" {
		t.Fatalf("retrieve: %+v err=%v, want go.txt first", srcs, err)
	}

	// (2) A metadata filter restricts results to the matching source.
	srcs, err = p.Retrieve(ctx, knowledge.Question{
		Text:   "goroutines concurrency",
		Filter: vectorstores.MetadataEquals(map[string]any{"source": "rust.txt"}),
	})
	if err != nil || len(srcs) == 0 {
		t.Fatalf("filtered retrieve: %+v err=%v", srcs, err)
	}
	for _, s := range srcs {
		if s.Metadata["source"] != "rust.txt" {
			t.Fatalf("filter leaked %v: %+v", s.Metadata["source"], srcs)
		}
	}

	// (3) Re-indexing an unchanged corpus skips everything.
	st, err = ix.Index(ctx, corpus(oldRust))
	if err != nil || st.Added != 0 || st.Skipped != 4 || st.Removed != 0 {
		t.Fatalf("unchanged reindex: stats=%+v err=%v, want Skipped=4 only", st, err)
	}

	// (4) Editing one document removes its old chunk and adds the new one.
	st, err = ix.Index(ctx, corpus(newRust))
	if err != nil || st.Removed != 1 || st.Added != 1 || st.Skipped != 3 {
		t.Fatalf("edit reindex: stats=%+v err=%v, want Removed=1 Added=1 Skipped=3", st, err)
	}
	p.Docs = ix.Documents()
	srcs, err = p.Retrieve(ctx, knowledge.Question{Text: "guarantees memory safety with its borrow checker"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs {
		if strings.Contains(s.Content, "borrow checker") {
			t.Fatalf("old text still retrievable after edit: %+v", srcs)
		}
	}
	srcs, err = p.Retrieve(ctx, knowledge.Question{Text: "systems language with zero-cost abstractions"})
	if err != nil || len(srcs) == 0 || srcs[0].Content != newRust {
		t.Fatalf("new text not retrieved: %+v err=%v", srcs, err)
	}

	// (5) An answer carries its sources and the rag strategy.
	p.Model = &models.Fake{Func: func([]messages.Message, models.Options) (messages.Message, error) {
		return messages.AI("ok"), nil
	}}
	ans, err := p.Answer(ctx, knowledge.Question{Text: "Which language has goroutines?"})
	if err != nil || ans.Strategy != "rag" || ans.Text != "ok" || len(ans.Sources) == 0 {
		t.Fatalf("answer: %+v err=%v", ans, err)
	}
}
