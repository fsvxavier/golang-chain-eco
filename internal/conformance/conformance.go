// Package conformance holds backend-agnostic test suites for checkpoint
// savers, long-term stores and vector backends, so every integration is held
// to the same behaviour as the in-memory references.
package conformance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Saver exercises put/get/list/delete ordering.
func Saver(t *testing.T, s checkpoint.Saver) {
	ctx, th := context.Background(), fmt.Sprintf("t-%d", time.Now().UnixNano())
	for i := range 3 {
		id, err := s.Put(ctx, checkpoint.Checkpoint{ID: fmt.Sprintf("c%d", i), ThreadID: th, Step: i, Source: "loop",
			Channels: map[string]any{"n": i}, Next: []checkpoint.Task{{Node: "x"}}})
		if err != nil || id != fmt.Sprintf("c%d", i) {
			t.Fatal(id, err)
		}
	}
	got, ok, err := s.Get(ctx, checkpoint.Config{ThreadID: th})
	if err != nil || !ok || got.ID != "c2" || got.Step != 2 || len(got.Next) != 1 {
		t.Fatalf("latest: %+v %v %v", got, ok, err)
	}
	got, ok, _ = s.Get(ctx, checkpoint.Config{ThreadID: th, CheckpointID: "c0"})
	if !ok || got.ID != "c0" {
		t.Fatalf("by id: %+v", got)
	}
	list, err := s.List(ctx, th, 0)
	if err != nil || len(list) != 3 || list[0].ID != "c2" || list[2].ID != "c0" {
		t.Fatalf("list: %v %v", list, err)
	}
	if list, _ = s.List(ctx, th, 2); len(list) != 2 {
		t.Fatalf("limit: %d", len(list))
	}
	if _, ok, _ = s.Get(ctx, checkpoint.Config{ThreadID: th, CheckpointID: "nope"}); ok {
		t.Fatal("missing id found")
	}
	if err := s.DeleteThread(ctx, th); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ = s.Get(ctx, checkpoint.Config{ThreadID: th}); ok {
		t.Fatal("thread not deleted")
	}
	Namespaces(t, s)
}

// Namespaces checks that checkpoints of a subgraph (namespace "sub:0") stay
// apart from the root graph's, and that DeleteThread removes both.
func Namespaces(t *testing.T, s checkpoint.Saver) {
	ctx, th := context.Background(), fmt.Sprintf("ns-%d", time.Now().UnixNano())
	const sub = "sub:0|inner:1"
	put := func(id, ns string, step int) {
		if _, err := s.Put(ctx, checkpoint.Checkpoint{ID: id, ThreadID: th, Namespace: ns, Step: step, Source: "loop",
			Channels: map[string]any{"n": step}, Next: []checkpoint.Task{{Node: "x"}}}); err != nil {
			t.Fatal(err)
		}
	}
	put("r0", "", 0)
	put("s0", sub, 0)
	put("s1", sub, 1)
	put("r1", "", 1)

	if got, ok, _ := s.Get(ctx, checkpoint.Config{ThreadID: th}); !ok || got.ID != "r1" || got.Namespace != "" {
		t.Fatalf("root latest: %+v %v", got, ok)
	}
	if got, ok, _ := s.Get(ctx, checkpoint.Config{ThreadID: th, Namespace: sub}); !ok || got.ID != "s1" || got.Namespace != sub || got.Step != 1 {
		t.Fatalf("namespace latest: %+v %v", got, ok)
	}
	if got, ok, _ := s.Get(ctx, checkpoint.Config{ThreadID: th, Namespace: sub, CheckpointID: "r0"}); ok {
		t.Fatalf("root id found in namespace: %+v", got)
	}
	if got, ok, _ := s.Get(ctx, checkpoint.Config{ThreadID: th, CheckpointID: "s0"}); ok {
		t.Fatalf("namespace id found in root: %+v", got)
	}
	if list, _ := s.List(ctx, th, 0); len(list) != 2 || list[0].ID != "r1" || list[1].ID != "r0" {
		t.Fatalf("List must return root checkpoints only: %v", list)
	}
	if err := s.DeleteThread(ctx, th); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, checkpoint.Config{ThreadID: th, Namespace: sub}); ok {
		t.Fatal("DeleteThread left a namespace behind")
	}
}

// Store exercises put/get/search/list/delete.
func Store(t *testing.T, s store.Store) {
	ctx, root := context.Background(), fmt.Sprintf("r-%d", time.Now().UnixNano())
	put := func(user, lang string) {
		if err := s.Put(ctx, []string{root, user}, "prefs", map[string]any{"lang": lang}); err != nil {
			t.Fatal(err)
		}
	}
	put("1", "go")
	put("2", "py")
	it, err := s.Get(ctx, []string{root, "1"}, "prefs")
	if err != nil || it == nil || it.Value["lang"] != "go" || it.CreatedAt.IsZero() {
		t.Fatalf("get: %+v %v", it, err)
	}
	created := it.CreatedAt
	put("1", "rust")
	if it, _ = s.Get(ctx, []string{root, "1"}, "prefs"); it.Value["lang"] != "rust" || !it.CreatedAt.Equal(created) {
		t.Fatalf("update keeps CreatedAt: %+v", it)
	}
	got, err := s.Search(ctx, []string{root}, map[string]any{"lang": "py"}, 0)
	if err != nil || len(got) != 1 || got[0].Namespace[1] != "2" {
		t.Fatalf("search: %+v %v", got, err)
	}
	if got, _ = s.Search(ctx, []string{root}, nil, 1); len(got) != 1 {
		t.Fatalf("limit: %d", len(got))
	}
	if ns, _ := s.ListNamespaces(ctx, []string{root}); len(ns) != 2 {
		t.Fatalf("namespaces: %v", ns)
	}
	if err := s.Delete(ctx, []string{root, "1"}, "prefs"); err != nil {
		t.Fatal(err)
	}
	if it, _ = s.Get(ctx, []string{root, "1"}, "prefs"); it != nil {
		t.Fatal("not deleted")
	}
	if it, err = s.Get(ctx, []string{root, "zzz"}, "prefs"); it != nil || err != nil {
		t.Fatalf("missing: %v %v", it, err)
	}
}

// Vector exercises upsert/query/delete on 3-d unit-ish vectors.
func Vector(t *testing.T, b vectorstore.Backend) {
	ctx := context.Background()
	recs := []vectorstore.Record{
		{ID: "x", Vector: []float32{1, 0, 0}, Text: "x-axis", Metadata: map[string]any{"k": "v"}},
		{ID: "y", Vector: []float32{0, 1, 0}, Text: "y-axis"},
		{ID: "z", Vector: []float32{0, 0, 1}, Text: "z-axis"},
	}
	if err := b.Upsert(ctx, recs); err != nil {
		t.Fatal(err)
	}
	hits, err := b.Query(ctx, []float32{0.9, 0.1, 0}, 2)
	if err != nil || len(hits) != 2 || hits[0].ID != "x" || hits[0].Text != "x-axis" || hits[0].Metadata["k"] != "v" {
		t.Fatalf("query: %+v %v", hits, err)
	}
	if hits[0].Score < hits[1].Score {
		t.Fatalf("scores not descending: %v", hits)
	}
	if err := b.Delete(ctx, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if hits, _ = b.Query(ctx, []float32{1, 0, 0}, 1); len(hits) != 1 || hits[0].ID == "x" {
		t.Fatalf("after delete: %+v", hits)
	}
}
