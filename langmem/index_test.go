package langmem_test

import (
	"context"
	"fmt"
	sl "slices"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

var vocab = []string{"sushi", "ramen", "kubernetes", "hiking", "coffee", "python", "guitar", "tokyo", "budget", "cluster"}

func hnswIndexed(base store.Store) *langmem.IndexedStore {
	ix := langmem.NewIndexedStore(base, embeddings.Fake{})
	ix.Index = vectorstore.NewHNSW()
	return ix
}

func keys(items []store.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Key
	}
	sl.Sort(out)
	return out
}

func hitKeys(hits []langmem.Hit) []string {
	items := make([]store.Item, len(hits))
	for i, h := range hits {
		items[i] = h.Item
	}
	return keys(items)
}

func TestIndexedStoreIndexMatchesScan(t *testing.T) {
	ctx, ns := context.Background(), []string{"m"}
	scan := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})
	idx := hnswIndexed(store.NewMemory())
	for i := range 300 {
		content := fmt.Sprintf("note %d about %s and %s", i, vocab[i%len(vocab)], vocab[(i*7+3)%len(vocab)])
		v := map[string]any{"content": content}
		if err := scan.Put(ctx, ns, fmt.Sprint(i), v); err != nil {
			t.Fatal(err)
		}
		if err := idx.Put(ctx, ns, fmt.Sprint(i), v); err != nil {
			t.Fatal(err)
		}
	}
	found, total := 0, 0
	for _, q := range vocab {
		want, _ := scan.SemanticSearch(ctx, ns, q+" notes", nil, 5)
		got, err := idx.SemanticSearch(ctx, ns, q+" notes", nil, 5)
		if err != nil || len(want) == 0 {
			t.Fatal(err)
		}
		// Many documents tie exactly (bag-of-words vectors), so a hit counts when
		// its score reaches the scan's k-th best score, not when its id matches.
		kth := want[len(want)-1].Score
		for _, h := range got {
			if h.Score >= kth-1e-6 { // float32 index vs float64 scan
				found++
			}
		}
		total += len(want)
	}
	if r := float64(found) / float64(total); r < 0.9 {
		t.Fatalf("indexed recall@5 %.2f", r)
	}
}

func TestIndexedStoreDeleteAndReindex(t *testing.T) {
	ctx, ns, base := context.Background(), []string{"m"}, store.NewMemory()
	ix := langmem.NewIndexedStore(base, embeddings.Fake{})
	_ = ix.Put(ctx, ns, "a", map[string]any{"content": "sushi lover"})
	_ = ix.Put(ctx, ns, "b", map[string]any{"content": "sushi and ramen"})
	// a store with data written before the index existed
	ix2 := hnswIndexed(base)
	if err := ix2.Reindex(ctx); err != nil {
		t.Fatal(err)
	}
	hits, _ := ix2.SemanticSearch(ctx, ns, "sushi", nil, 5)
	if got := hitKeys(hits); !sl.Equal(got, []string{"a", "b"}) {
		t.Fatalf("after reindex: %v", got)
	}
	if err := ix2.Delete(ctx, ns, "a"); err != nil {
		t.Fatal(err)
	}
	hits, _ = ix2.SemanticSearch(ctx, ns, "sushi", nil, 5)
	if got := hitKeys(hits); !sl.Equal(got, []string{"b"}) {
		t.Fatalf("after delete: %v", got)
	}
	if it, _ := ix2.Get(ctx, ns, "a"); it != nil {
		t.Fatal("delete did not reach the store")
	}
}

// Operators must give the same answer on the scan and on the index path.
func TestFilterOperators(t *testing.T) {
	ctx, ns := context.Background(), []string{"m"}
	cases := []struct {
		name   string
		filter map[string]any
		want   []string
	}{
		{"plain equality", map[string]any{"kind": "food"}, []string{"a", "b"}},
		{"$gt", map[string]any{"priority": map[string]any{"$gt": 1}}, []string{"a", "c"}},
		{"$gte", map[string]any{"priority": map[string]any{"$gte": 3}}, []string{"a", "c"}},
		{"$lt", map[string]any{"priority": map[string]any{"$lt": 3}}, []string{"b"}},
		{"$lte", map[string]any{"priority": map[string]any{"$lte": 3}}, []string{"a", "b"}},
		{"$eq", map[string]any{"priority": map[string]any{"$eq": 5}}, []string{"c"}},
		{"$ne", map[string]any{"kind": map[string]any{"$ne": "food"}}, []string{"c"}},
		{"$in", map[string]any{"kind": map[string]any{"$in": []any{"ops", "x"}}}, []string{"c"}},
		{"combined", map[string]any{"kind": "food", "priority": map[string]any{"$gt": 2}}, []string{"a"}},
		{"string order", map[string]any{"kind": map[string]any{"$lt": "g"}}, []string{"a", "b"}},
		{"non-comparable matches nothing", map[string]any{"priority": map[string]any{"$gt": "x"}}, nil},
	}
	items := map[string]map[string]any{
		"a": {"content": "sushi japanese food", "kind": "food", "priority": 3},
		"b": {"content": "sushi ramen", "kind": "food", "priority": 1},
		"c": {"content": "sushi cluster", "kind": "ops", "priority": 5.0}, // float, as JSON backends decode
	}
	for _, mode := range []string{"scan", "index"} {
		var ix *langmem.IndexedStore
		if mode == "index" {
			ix = hnswIndexed(store.NewMemory())
		} else {
			ix = langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})
		}
		for k, v := range items {
			if err := ix.Put(ctx, ns, k, v); err != nil {
				t.Fatal(err)
			}
		}
		for _, c := range cases {
			t.Run(mode+"/"+c.name, func(t *testing.T) {
				hits, err := ix.SemanticSearch(ctx, ns, "sushi", c.filter, 10)
				if err != nil {
					t.Fatal(err)
				}
				if got := hitKeys(hits); !sl.Equal(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
					t.Fatalf("semantic: got %v want %v", got, c.want)
				}
				listed, err := ix.Search(ctx, ns, c.filter, 10)
				if err != nil {
					t.Fatal(err)
				}
				if got := keys(listed); !sl.Equal(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
					t.Fatalf("search: got %v want %v", got, c.want)
				}
			})
		}
	}
	if _, err := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{}).Search(ctx, ns, map[string]any{"kind": map[string]any{"$regex": "x"}}, 5); err == nil || !strings.Contains(err.Error(), "$regex") {
		t.Fatalf("unknown operator must fail, got %v", err)
	}
}

// When the oversampled index hits are all filtered out, the search must fall
// back to the scan and still find the single matching item.
func TestFilterOversampleFallsBackToScan(t *testing.T) {
	ctx, ns := context.Background(), []string{"m"}
	ix := hnswIndexed(store.NewMemory())
	for i := range 40 {
		_ = ix.Put(ctx, ns, fmt.Sprint("food", i), map[string]any{"content": "sushi", "kind": "food"})
	}
	_ = ix.Put(ctx, ns, "ops", map[string]any{"content": "kubernetes cluster", "kind": "ops"})
	filter := map[string]any{"kind": map[string]any{"$eq": "ops"}}
	hits, err := ix.SemanticSearch(ctx, ns, "sushi", filter, 1)
	if err != nil || len(hits) != 1 || hits[0].Key != "ops" {
		t.Fatalf("semantic fallback: %+v %v", hits, err)
	}
	items, err := ix.Search(ctx, ns, filter, 1)
	if err != nil || len(items) != 1 || items[0].Key != "ops" {
		t.Fatalf("search fallback: %+v %v", items, err)
	}
}
