package langmem

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

const embeddingKey = "_embedding"

// Hit is a search result with its relevance score.
type Hit struct {
	store.Item
	Score float64
}

// SemanticSearcher is implemented by stores that can rank items by a text query.
type SemanticSearcher interface {
	SemanticSearch(ctx context.Context, prefix []string, query string, filter map[string]any, limit int) ([]Hit, error)
}

// IndexedStore wraps a store.Store and embeds the text of each item on Put,
// enabling SemanticSearch (the `index` config of LangGraph's store). It works
// over any backend (memory, sql, redis, mongo, ...), because vectors live
// inside the item value; ranking is a brute-force cosine scan of the prefix.
type IndexedStore struct {
	store.Store
	Embedder embeddings.Embedder
	// Text extracts the text to embed (default: Value["content"]).
	Text func(value map[string]any) string
}

// NewIndexedStore wraps st.
func NewIndexedStore(st store.Store, e embeddings.Embedder) *IndexedStore {
	return &IndexedStore{Store: st, Embedder: e}
}

func (s *IndexedStore) text(v map[string]any) string {
	if s.Text != nil {
		return s.Text(v)
	}
	return TextOf(v["content"])
}

// TextOf renders a memory content (string or JSON-able value) as text.
func TextOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, TextOf(x[k]))
		}
		return strings.Join(parts, " ")
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, TextOf(e))
		}
		return strings.Join(parts, " ")
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *IndexedStore) Put(ctx context.Context, ns []string, key string, v map[string]any) error {
	v = maps.Clone(v)
	delete(v, embeddingKey)
	if t := s.text(v); t != "" {
		vec, err := s.Embedder.EmbedQuery(ctx, t)
		if err != nil {
			return err
		}
		v[embeddingKey] = vec
	}
	return s.Store.Put(ctx, ns, key, v)
}

func strip(it *store.Item) {
	if it == nil || it.Value == nil {
		return
	}
	v := maps.Clone(it.Value)
	delete(v, embeddingKey)
	it.Value = v
}

func (s *IndexedStore) Get(ctx context.Context, ns []string, key string) (*store.Item, error) {
	it, err := s.Store.Get(ctx, ns, key)
	strip(it)
	return it, err
}

func (s *IndexedStore) Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]store.Item, error) {
	items, err := s.Store.Search(ctx, prefix, filter, limit)
	for i := range items {
		strip(&items[i])
	}
	return items, err
}

// SemanticSearch ranks items under prefix by similarity to query.
func (s *IndexedStore) SemanticSearch(ctx context.Context, prefix []string, query string, filter map[string]any, limit int) ([]Hit, error) {
	items, err := s.Store.Search(ctx, prefix, filter, 0)
	if err != nil {
		return nil, err
	}
	q, err := s.Embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(items))
	for _, it := range items {
		h := Hit{Item: it}
		if vec := toVec(it.Value[embeddingKey]); vec != nil {
			h.Score = embeddings.Cosine(q, vec)
		}
		strip(&h.Item)
		hits = append(hits, h)
	}
	return rank(hits, limit), nil
}

// toVec accepts []float32 (in-memory) and []any (decoded from JSON backends).
func toVec(v any) []float32 {
	switch x := v.(type) {
	case []float32:
		return x
	case []float64:
		out := make([]float32, len(x))
		for i, f := range x {
			out[i] = float32(f)
		}
		return out
	case []any:
		out := make([]float32, len(x))
		for i, e := range x {
			f, _ := e.(float64)
			out[i] = float32(f)
		}
		return out
	}
	return nil
}

func rank(h []Hit, limit int) []Hit {
	slices.SortStableFunc(h, func(a, b Hit) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	if limit > 0 && len(h) > limit {
		h = h[:limit]
	}
	return h
}

// Query searches st for query under prefix: semantic when st is a SemanticSearcher,
// otherwise a keyword-overlap ranking over the whole prefix. An empty query
// lists items. limit <= 0 means 10.
func Query(ctx context.Context, st store.Store, prefix []string, query string, filter map[string]any, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 10
	}
	if query != "" {
		if s, ok := st.(SemanticSearcher); ok {
			return s.SemanticSearch(ctx, prefix, query, filter, limit)
		}
	}
	items, err := st.Search(ctx, prefix, filter, 0)
	if err != nil {
		return nil, err
	}
	qw := words(query)
	hits := make([]Hit, 0, len(items))
	for _, it := range items {
		h := Hit{Item: it}
		if len(qw) > 0 {
			dw := words(TextOf(it.Value["content"]))
			n := 0
			for w := range qw {
				if dw[w] {
					n++
				}
			}
			h.Score = float64(n) / float64(len(qw))
			if n == 0 {
				continue
			}
		}
		hits = append(hits, h)
	}
	return rank(hits, limit), nil
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		m[w] = true
	}
	return m
}
