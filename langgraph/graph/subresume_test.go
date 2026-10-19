package graph_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	. "github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func asker(name string) NodeFunc {
	return func(ctx context.Context, _ State) (any, error) {
		return State{"log": []any{name + "=" + fmt.Sprint(Interrupt(ctx, "q"+name))}}, nil
	}
}

// Two nodes of a subgraph interrupt in the same super-step. The pauses
// surface in task order and each answer reaches the node that asked,
// whatever the goroutine scheduling, with or without a child checkpointer.
func TestSubgraphParallelInterruptsResumeInTaskOrder(t *testing.T) {
	for _, withSaver := range []bool{true, false} {
		t.Run(fmt.Sprint("saver=", withSaver), func(t *testing.T) {
			for i := range 50 {
				saver := checkpoint.NewMemory()
				var opts []CompileOption
				if withSaver {
					opts = append(opts, WithCheckpointer(saver))
				}
				inner, err := NewStateGraph(schema()).AddNode("x", asker("x")).AddNode("y", asker("y")).
					AddEdge(Start, "x").AddEdge(Start, "y").AddEdge("x", End).AddEdge("y", End).Compile(opts...)
				if err != nil {
					t.Fatal(err)
				}
				outer := subgraphWithSaver(inner, saver)
				ctx := runnables.WithConfig(context.Background(), Thread(fmt.Sprint("p", i)))
				out, _ := outer.Invoke(ctx, State{})
				if is := Interrupts(out); len(is) != 1 || is[0].Value != "qx" {
					t.Fatalf("run %d first pause: %v", i, out)
				}
				out, _ = outer.Invoke(ctx, Command{Resume: "a1"})
				if is := Interrupts(out); len(is) != 1 || is[0].Value != "qy" {
					t.Fatalf("run %d second pause: %v", i, out)
				}
				out, err = outer.Invoke(ctx, Command{Resume: "a2"})
				if err != nil || fmt.Sprint(out["log"]) != "[x=a1 y=a2]" {
					t.Fatalf("run %d: %v %v", i, out, err)
				}
			}
		})
	}
}

// One node of a checkpointed subgraph interrupts twice: on the second resume
// it re-runs from its start and must get both answers in order.
func TestSubgraphNodeInterruptsTwice(t *testing.T) {
	saver := checkpoint.NewMemory()
	inner, _ := NewStateGraph(schema()).
		AddNode("ask", func(ctx context.Context, _ State) (any, error) {
			a, b := Interrupt(ctx, "q1"), Interrupt(ctx, "q2")
			return State{"log": []any{a, b}}, nil
		}).AddEdge(Start, "ask").AddEdge("ask", End).Compile(WithCheckpointer(saver))
	outer := subgraphWithSaver(inner, saver)
	ctx := runnables.WithConfig(context.Background(), Thread("twice"))
	out, _ := outer.Invoke(ctx, State{})
	if is := Interrupts(out); len(is) != 1 || is[0].Value != "q1" {
		t.Fatalf("first pause: %v", out)
	}
	out, _ = outer.Invoke(ctx, Command{Resume: "a1"})
	if is := Interrupts(out); len(is) != 1 || is[0].Value != "q2" {
		t.Fatalf("second pause: %v", out)
	}
	out, err := outer.Invoke(ctx, Command{Resume: "a2"})
	if err != nil || fmt.Sprint(out["log"]) != "[a1 a2]" {
		t.Fatalf("%v %v", out, err)
	}
}
