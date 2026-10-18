package functional_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/functional"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func TestInterruptReplaysTasks(t *testing.T) {
	var runs atomic.Int32
	wf := functional.New("wf", checkpoint.NewMemory(), func(ctx context.Context, n int) (int, error) {
		sq, err := functional.Task(ctx, "square", func(context.Context) (int, error) { runs.Add(1); return n * n, nil }).Result()
		if err != nil {
			return 0, err
		}
		add := graph.Interrupt(ctx, "add what?").(int)
		return sq + add, nil
	})
	ctx := runnables.WithConfig(context.Background(), graph.Thread("f"))
	r, err := wf.Invoke(ctx, 4)
	if err != nil || r.Interrupt == nil || r.Interrupt.Value != "add what?" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = wf.Resume(ctx, 3)
	if err != nil || r.Interrupt != nil || r.Value != 19 {
		t.Fatalf("%+v %v", r, err)
	}
	if runs.Load() != 1 {
		t.Fatalf("task re-executed %d times", runs.Load())
	}
}
