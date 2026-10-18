package store_test

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

func TestMemory(t *testing.T) {
	s, ctx := store.NewMemory(), context.Background()
	_ = s.Put(ctx, []string{"users", "1"}, "prefs", map[string]any{"lang": "go"})
	_ = s.Put(ctx, []string{"users", "2"}, "prefs", map[string]any{"lang": "py"})
	it, _ := s.Get(ctx, []string{"users", "1"}, "prefs")
	if it == nil || it.Value["lang"] != "go" {
		t.Fatal(it)
	}
	got, _ := s.Search(ctx, []string{"users"}, map[string]any{"lang": "py"}, 0)
	if len(got) != 1 || got[0].Namespace[1] != "2" {
		t.Fatal(got)
	}
	ns, _ := s.ListNamespaces(ctx, []string{"users"})
	if len(ns) != 2 {
		t.Fatal(ns)
	}
	_ = s.Delete(ctx, []string{"users", "1"}, "prefs")
	if it, _ := s.Get(ctx, []string{"users", "1"}, "prefs"); it != nil {
		t.Fatal("deleted")
	}
}
