package langmem

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
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

// VectorIndex is an optional approximate index for IndexedStore (and ciam):
// vectorstore.NewHNSW() satisfies it. Ids are opaque to the index.
type VectorIndex interface {
	Upsert(ctx context.Context, recs []vectorstore.Record) error
	Delete(ctx context.Context, ids []string) error
	Query(ctx context.Context, vec []float32, k int) ([]vectorstore.Hit, error)
}

// IndexedStore wraps a store.Store and embeds the text of each item on Put,
// enabling SemanticSearch (the `index` config of LangGraph's store). It works
// over any backend (memory, sql, redis, mongo, ...), because vectors live
// inside the item value. Ranking is a brute-force cosine scan of the prefix
// unless Index is set.
type IndexedStore struct {
	store.Store
	Embedder embeddings.Embedder
	// Text extracts the text to embed (default: Value["content"]).
	Text func(value map[string]any) string
	// Index, when set, serves SemanticSearch through an approximate index
	// (e.g. vectorstore.NewHNSW()). Put and Delete keep it in step; call
	// Reindex to add items that were written before it was set.
	Index VectorIndex
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

// indexID encodes (namespace, key) as an index id; parseIndexID reverses it.
func indexID(ns []string, key string) string {
	b, _ := json.Marshal(append(slices.Clone(ns), key))
	return string(b)
}

func parseIndexID(id string) (ns []string, key string, ok bool) {
	var parts []string
	if json.Unmarshal([]byte(id), &parts) != nil || len(parts) == 0 {
		return nil, "", false
	}
	return parts[:len(parts)-1], parts[len(parts)-1], true
}

func (s *IndexedStore) Put(ctx context.Context, ns []string, key string, v map[string]any) error {
	v = maps.Clone(v)
	delete(v, embeddingKey)
	var vec []float32
	if t := s.text(v); t != "" {
		var err error
		if vec, err = s.Embedder.EmbedQuery(ctx, t); err != nil {
			return err
		}
		v[embeddingKey] = vec
	}
	if err := s.Store.Put(ctx, ns, key, v); err != nil {
		return err
	}
	if s.Index == nil {
		return nil
	}
	if vec == nil {
		return s.Index.Delete(ctx, []string{indexID(ns, key)})
	}
	return s.Index.Upsert(ctx, []vectorstore.Record{{ID: indexID(ns, key), Vector: vec}})
}

// Delete removes the item from the store and from Index.
func (s *IndexedStore) Delete(ctx context.Context, ns []string, key string) error {
	if s.Index != nil {
		if err := s.Index.Delete(ctx, []string{indexID(ns, key)}); err != nil {
			return err
		}
	}
	return s.Store.Delete(ctx, ns, key)
}

// Reindex adds every stored item that has an embedding to Index. Items written
// through Put are indexed already; use this after setting Index on a store
// that already holds data.
func (s *IndexedStore) Reindex(ctx context.Context) error {
	if s.Index == nil {
		return nil
	}
	items, err := s.Store.Search(ctx, nil, nil, 0)
	if err != nil {
		return err
	}
	var recs []vectorstore.Record
	for _, it := range items {
		if vec := toVec(it.Value[embeddingKey]); vec != nil {
			recs = append(recs, vectorstore.Record{ID: indexID(it.Namespace, it.Key), Vector: vec})
		}
	}
	return s.Index.Upsert(ctx, recs)
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

// Search lists items under prefix matching filter. Plain values match by
// equality; "$" operator maps are evaluated here (see filter.go).
func (s *IndexedStore) Search(ctx context.Context, prefix []string, filter map[string]any, limit int) ([]store.Item, error) {
	eq, ops, err := splitFilter(filter)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		items, err := s.Store.Search(ctx, prefix, eq, limit)
		for i := range items {
			strip(&items[i])
		}
		return items, err
	}
	// oversample k*4 and post-filter; scan everything if that leaves too few
	if limit > 0 {
		items, err := s.Store.Search(ctx, prefix, eq, limit*4)
		if err != nil {
			return nil, err
		}
		out := filterItems(items, ops)
		if len(out) >= limit || len(items) < limit*4 {
			return stripAll(out[:min(limit, len(out))]), nil
		}
	}
	items, err := s.Store.Search(ctx, prefix, eq, 0)
	if err != nil {
		return nil, err
	}
	out := filterItems(items, ops)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return stripAll(out), nil
}

func stripAll(items []store.Item) []store.Item {
	for i := range items {
		strip(&items[i])
	}
	return items
}

// SemanticSearch ranks items under prefix by similarity to query, restricted
// to items matching filter.
func (s *IndexedStore) SemanticSearch(ctx context.Context, prefix []string, query string, filter map[string]any, limit int) ([]Hit, error) {
	eq, ops, err := splitFilter(filter)
	if err != nil {
		return nil, err
	}
	q, err := s.Embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	if s.Index != nil && limit > 0 {
		hits, ok, err := s.indexedSearch(ctx, prefix, q, eq, ops, limit)
		if err != nil || ok {
			return hits, err
		}
	}
	items, err := s.Store.Search(ctx, prefix, eq, 0)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(items))
	for _, it := range filterItems(items, ops) {
		h := Hit{Item: it}
		if vec := toVec(it.Value[embeddingKey]); vec != nil {
			h.Score = embeddings.Cosine(q, vec)
		}
		strip(&h.Item)
		hits = append(hits, h)
	}
	return rank(hits, limit), nil
}

// indexedSearch asks Index for k*4 neighbours and post-filters them. ok is
// false when fewer than limit matches survived and the index may hold more,
// so the caller falls back to the scan.
func (s *IndexedStore) indexedSearch(ctx context.Context, prefix []string, q []float32, eq map[string]any, ops map[string]map[string]any, limit int) (hits []Hit, ok bool, err error) {
	k := limit * 4
	raw, err := s.Index.Query(ctx, q, k)
	if err != nil {
		return nil, false, err
	}
	for _, r := range raw {
		ns, key, valid := parseIndexID(r.ID)
		if !valid || len(ns) < len(prefix) || !slices.Equal(ns[:len(prefix)], prefix) {
			continue
		}
		it, err := s.Store.Get(ctx, ns, key)
		if err != nil {
			return nil, false, err
		}
		if it == nil || !matchEq(it.Value, eq) || !matchOps(it.Value, ops) {
			continue
		}
		h := Hit{Item: *it, Score: r.Score}
		strip(&h.Item)
		hits = append(hits, h)
	}
	if len(hits) < limit && len(raw) == k {
		return nil, false, nil
	}
	return rank(hits, limit), true, nil
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
