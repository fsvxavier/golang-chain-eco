// Package vectorstores defines the VectorStore interface and an in-memory
// implementation with cosine similarity, metadata filters, MMR search and
// JSON persistence (langchain_core.vectorstores.InMemoryVectorStore).
package vectorstores

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Filter decides whether a document is eligible.
type Filter func(documents.Document) bool

// MetadataEquals builds a Filter matching all key/value pairs.
func MetadataEquals(kv map[string]any) Filter {
	return func(d documents.Document) bool {
		for k, v := range kv {
			if fmt.Sprint(d.Metadata[k]) != fmt.Sprint(v) {
				return false
			}
		}
		return true
	}
}

// Scored is a document with its similarity score.
type Scored struct {
	Document documents.Document
	Score    float64
}

// VectorStore stores embedded documents.
type VectorStore interface {
	AddDocuments(ctx context.Context, docs []documents.Document) ([]string, error)
	SimilaritySearchWithScore(ctx context.Context, query string, k int, filter Filter) ([]Scored, error)
	MaxMarginalRelevance(ctx context.Context, query string, k, fetchK int, lambda float64, filter Filter) ([]documents.Document, error)
	Delete(ctx context.Context, ids []string) error
}

// SimilaritySearch returns the k most similar documents.
func SimilaritySearch(ctx context.Context, vs VectorStore, query string, k int, filter Filter) ([]documents.Document, error) {
	scored, err := vs.SimilaritySearchWithScore(ctx, query, k, filter)
	if err != nil {
		return nil, err
	}
	out := make([]documents.Document, len(scored))
	for i, s := range scored {
		out[i] = s.Document
	}
	return out, nil
}

type entry struct {
	Doc    documents.Document `json:"doc"`
	Vector []float32          `json:"vector"`
}

// Memory is an in-process vector store.
type Memory struct {
	emb   embeddings.Embedder
	mu    sync.RWMutex
	items map[string]entry
	order []string
}

// NewMemory creates an empty in-memory store using emb.
func NewMemory(emb embeddings.Embedder) *Memory {
	return &Memory{emb: emb, items: map[string]entry{}}
}

// FromDocuments creates a store and adds docs (from_documents).
func FromDocuments(ctx context.Context, emb embeddings.Embedder, docs []documents.Document) (*Memory, error) {
	m := NewMemory(emb)
	_, err := m.AddDocuments(ctx, docs)
	return m, err
}

// AddDocuments implements VectorStore. Documents keep their ID or get one.
func (m *Memory) AddDocuments(ctx context.Context, docs []documents.Document) ([]string, error) {
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.PageContent
	}
	vecs, err := m.emb.EmbedDocuments(ctx, texts)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, len(docs))
	for i, d := range docs {
		if d.ID == "" {
			d.ID = messages.NewID()
		}
		if _, exists := m.items[d.ID]; !exists {
			m.order = append(m.order, d.ID)
		}
		m.items[d.ID] = entry{d, vecs[i]}
		ids[i] = d.ID
	}
	return ids, nil
}

// AddTexts embeds raw texts with optional per-text metadata.
func (m *Memory) AddTexts(ctx context.Context, texts []string, metas []map[string]any) ([]string, error) {
	docs := make([]documents.Document, len(texts))
	for i, t := range texts {
		docs[i].PageContent = t
		if i < len(metas) {
			docs[i].Metadata = metas[i]
		}
	}
	return m.AddDocuments(ctx, docs)
}

func (m *Memory) rank(ctx context.Context, query string, filter Filter) ([]float32, []Scored, []entry, error) {
	q, err := m.emb.EmbedQuery(ctx, query)
	if err != nil {
		return nil, nil, nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var scored []Scored
	var ents []entry
	for _, id := range m.order {
		e, ok := m.items[id]
		if !ok || (filter != nil && !filter(e.Doc)) {
			continue
		}
		scored = append(scored, Scored{e.Doc, embeddings.Cosine(q, e.Vector)})
		ents = append(ents, e)
	}
	idx := make([]int, len(scored))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scored[idx[a]].Score > scored[idx[b]].Score })
	s2, e2 := make([]Scored, len(idx)), make([]entry, len(idx))
	for i, j := range idx {
		s2[i], e2[i] = scored[j], ents[j]
	}
	return q, s2, e2, nil
}

// SimilaritySearchWithScore implements VectorStore.
func (m *Memory) SimilaritySearchWithScore(ctx context.Context, query string, k int, filter Filter) ([]Scored, error) {
	_, scored, _, err := m.rank(ctx, query, filter)
	if err != nil {
		return nil, err
	}
	return scored[:min(k, len(scored))], nil
}

// MaxMarginalRelevance implements VectorStore: it picks k of the fetchK most
// similar documents trading relevance (lambda=1) against diversity (lambda=0).
func (m *Memory) MaxMarginalRelevance(ctx context.Context, query string, k, fetchK int, lambda float64, filter Filter) ([]documents.Document, error) {
	q, scored, ents, err := m.rank(ctx, query, filter)
	if err != nil {
		return nil, err
	}
	if fetchK <= 0 {
		fetchK = 20
	}
	n := min(fetchK, len(ents))
	var picked []int
	used := make([]bool, n)
	for len(picked) < min(k, n) {
		best, bestScore := -1, -1e18
		for i := 0; i < n; i++ {
			if used[i] {
				continue
			}
			redundancy := 0.0
			for _, p := range picked {
				redundancy = max(redundancy, embeddings.Cosine(ents[i].Vector, ents[p].Vector))
			}
			s := lambda*embeddings.Cosine(q, ents[i].Vector) - (1-lambda)*redundancy
			if s > bestScore {
				best, bestScore = i, s
			}
		}
		used[best] = true
		picked = append(picked, best)
	}
	out := make([]documents.Document, len(picked))
	for i, p := range picked {
		out[i] = scored[p].Document
	}
	return out, nil
}

// Delete implements VectorStore.
func (m *Memory) Delete(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.items, id)
	}
	kept := m.order[:0]
	for _, id := range m.order {
		if _, ok := m.items[id]; ok {
			kept = append(kept, id)
		}
	}
	m.order = kept
	return nil
}

// Get returns documents by ID.
func (m *Memory) Get(ids ...string) []documents.Document {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []documents.Document
	for _, id := range ids {
		if e, ok := m.items[id]; ok {
			out = append(out, e.Doc)
		}
	}
	return out
}

// Len returns the number of stored documents.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.order)
}

// Save writes the store (documents and vectors) to a JSON file.
func (m *Memory) Save(path string) error {
	m.mu.RLock()
	list := make([]entry, 0, len(m.order))
	for _, id := range m.order {
		list = append(list, m.items[id])
	}
	m.mu.RUnlock()
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Load reads a store saved with Save.
func Load(path string, emb embeddings.Embedder) (*Memory, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []entry
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	m := NewMemory(emb)
	for _, e := range list {
		m.items[e.Doc.ID] = e
		m.order = append(m.order, e.Doc.ID)
	}
	return m, nil
}
