package rag_test

import (
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

// TestRAGConformance runs the RAG suite against every backend family that can
// run fully offline: the core in-memory store, the vectorstore.Store over the
// Flat index (the Local backend) and over the in-process HNSW index.
// httptest-backed fakes (store_test.go) are unexported in their package and
// cannot be imported, so HNSW stands in for the third backend.
func TestRAGConformance(t *testing.T) {
	emb := embeddings.Fake{Dim: 128}
	stores := []struct {
		name     string
		newStore func() vectorstores.VectorStore
	}{
		{"memory", func() vectorstores.VectorStore { return vectorstores.NewMemory(emb) }},
		{"local-flat", func() vectorstores.VectorStore { return vectorstore.New(emb, vectorstore.NewFlat()) }},
		{"local-hnsw", func() vectorstores.VectorStore { return vectorstore.New(emb, vectorstore.NewHNSW()) }},
	}
	for _, s := range stores {
		t.Run(s.name, func(t *testing.T) { conformance.RAG(t, s.newStore) })
	}
}
