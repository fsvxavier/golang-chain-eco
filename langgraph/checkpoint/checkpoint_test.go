package checkpoint_test

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

func TestMemory(t *testing.T) {
	m, ctx := checkpoint.NewMemory(), context.Background()
	id1, _ := m.Put(ctx, checkpoint.Checkpoint{ThreadID: "t", Step: 1})
	id2, _ := m.Put(ctx, checkpoint.Checkpoint{ThreadID: "t", Step: 2, ParentID: id1})
	if cp, ok, _ := m.Get(ctx, checkpoint.Config{ThreadID: "t"}); !ok || cp.ID != id2 {
		t.Fatal("latest")
	}
	if cp, ok, _ := m.Get(ctx, checkpoint.Config{ThreadID: "t", CheckpointID: id1}); !ok || cp.Step != 1 {
		t.Fatal("by id")
	}
	if l, _ := m.List(ctx, "t", 0); len(l) != 2 || l[0].ID != id2 {
		t.Fatal("list order")
	}
	_ = m.DeleteThread(ctx, "t")
	if _, ok, _ := m.Get(ctx, checkpoint.Config{ThreadID: "t"}); ok {
		t.Fatal("deleted")
	}
	if _, err := m.Put(ctx, checkpoint.Checkpoint{}); err == nil {
		t.Fatal("empty thread must fail")
	}
}
