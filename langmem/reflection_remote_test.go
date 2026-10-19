package langmem_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langmem"
	"github.com/fsvxavier/golang-chain-eco/remote"
	"github.com/fsvxavier/golang-chain-eco/remote/server"
)

func TestRemoteReflectionDebounce(t *testing.T) {
	var runs atomic.Int32
	g, err := graph.NewStateGraph(graph.Schema{"messages": channels.LastValue(), "max_steps": channels.LastValue(), "done": channels.LastValue()}).
		AddNode("reflect", func(context.Context, graph.State) (any, error) {
			runs.Add(1)
			return graph.State{"done": true}, nil
		}).AddEdge(graph.Start, "reflect").AddEdge("reflect", graph.End).Compile()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.New(map[string]*graph.Compiled{"reflector": g}))
	t.Cleanup(ts.Close)

	ex := langmem.NewRemoteReflectionExecutor(remote.NewClient(ts.URL), "reflector")
	defer ex.Shutdown(true, false)
	msgs := []messages.Message{messages.Human("hi")}
	ctx := context.Background()

	// A scheduled run fires once the delay has passed.
	start := time.Now()
	fut, err := ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 40 * time.Millisecond, ThreadID: "t-fire"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fut.Wait(); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 35*time.Millisecond || runs.Load() != 1 {
		t.Fatalf("fired after %v with %d runs", el, runs.Load())
	}

	// A second submit on the same thread cancels the first; only one run executes.
	first, err := ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 80 * time.Millisecond, ThreadID: "t-debounce"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 40 * time.Millisecond, ThreadID: "t-debounce"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Wait(); !errors.Is(err, langmem.ErrCancelled) {
		t.Fatalf("first run: want ErrCancelled, got %v", err)
	}
	if _, err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := runs.Load(); got != 2 {
		t.Fatalf("runs = %d, want 2 (one per debounced thread)", got)
	}
}
