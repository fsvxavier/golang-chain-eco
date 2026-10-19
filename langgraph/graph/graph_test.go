package graph_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	. "github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func add(s string) NodeFunc {
	return func(_ context.Context, st State) (any, error) { return State{"log": []any{s}}, nil }
}

func schema() Schema { return Schema{"log": Append(), "n": channels.LastValue()} }

func TestLinearAndConditional(t *testing.T) {
	g := NewStateGraph(schema()).
		AddNode("a", add("a")).AddNode("b", add("b")).AddNode("c", add("c")).
		AddEdge(Start, "a").
		AddConditionalEdges("a", func(_ context.Context, s State) (any, error) {
			if s["n"] == 1 {
				return "b", nil
			}
			return "c", nil
		}).
		AddEdge("b", End).AddEdge("c", End)
	c, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Invoke(context.Background(), State{"n": 1})
	if err != nil || fmt.Sprint(out["log"]) != "[a b]" {
		t.Fatalf("%v %v", out, err)
	}
	out, _ = c.Invoke(context.Background(), State{"n": 2})
	if fmt.Sprint(out["log"]) != "[a c]" {
		t.Fatal(out)
	}
}

func TestFanOutJoinAndSend(t *testing.T) {
	g := NewStateGraph(schema()).
		AddNode("a", add("a")).AddNode("b", add("b")).AddNode("c", add("c")).AddNode("d", add("d")).
		AddEdge(Start, "a").AddEdge("a", "b").AddEdge("a", "c").
		AddJoin([]string{"b", "c"}, "d").AddEdge("d", End)
	c, _ := g.Compile()
	out, err := c.Invoke(context.Background(), State{})
	if err != nil || fmt.Sprint(out["log"]) != "[a b c d]" {
		t.Fatalf("%v %v", out, err)
	}

	m := NewStateGraph(schema()).
		AddNode("fan", func(context.Context, State) (any, error) {
			return Command{Send: []Send{{"w", State{"n": 1}}, {"w", State{"n": 2}}}}, nil
		}).
		AddNode("w", func(_ context.Context, s State) (any, error) {
			return State{"log": []any{s["n"]}}, nil
		}).AddEdge(Start, "fan")
	mc, _ := m.Compile()
	out, err = mc.Invoke(context.Background(), State{})
	if err != nil || fmt.Sprint(out["log"]) != "[1 2]" {
		t.Fatalf("%v %v", out, err)
	}
}

func TestInterruptResume(t *testing.T) {
	g := NewStateGraph(schema()).
		AddNode("ask", func(ctx context.Context, s State) (any, error) {
			ans := Interrupt(ctx, "name?")
			return State{"log": []any{ans}}, nil
		}).AddEdge(Start, "ask").AddEdge("ask", End)
	c, _ := g.Compile(WithCheckpointer(checkpoint.NewMemory()))
	ctx := runnables.WithConfig(context.Background(), Thread("t1"))
	out, err := c.Invoke(ctx, State{})
	if err != nil {
		t.Fatal(err)
	}
	is := Interrupts(out)
	if len(is) != 1 || is[0].Value != "name?" {
		t.Fatalf("%v", out)
	}
	out, err = c.Invoke(ctx, Command{Resume: "bob"})
	if err != nil || fmt.Sprint(out["log"]) != "[bob]" || Interrupts(out) != nil {
		t.Fatalf("%v %v", out, err)
	}
	snap, _ := c.GetState(ctx)
	if len(snap.Next) != 0 {
		t.Fatal(snap)
	}
}

func TestInterruptBeforeAndHistory(t *testing.T) {
	g := NewStateGraph(schema()).AddNode("a", add("a")).AddNode("b", add("b")).
		AddEdge(Start, "a").AddEdge("a", "b").AddEdge("b", End)
	c, _ := g.Compile(WithCheckpointer(checkpoint.NewMemory()), InterruptBefore("b"))
	ctx := runnables.WithConfig(context.Background(), Thread("x"))
	out, _ := c.Invoke(ctx, State{})
	if fmt.Sprint(out["log"]) != "[a]" || len(Interrupts(out)) != 1 {
		t.Fatal(out)
	}
	out, _ = c.Invoke(ctx, nil)
	if fmt.Sprint(out["log"]) != "[a b]" {
		t.Fatal(out)
	}
	h, _ := c.GetStateHistory(ctx, 0)
	if len(h) < 3 {
		t.Fatal(len(h))
	}
	if _, err := c.UpdateState(ctx, State{"n": 9}, ""); err != nil {
		t.Fatal(err)
	}
	snap, _ := c.GetState(ctx)
	if snap.Values["n"] != 9 {
		t.Fatal(snap)
	}
}

func TestRecursionLimitAndStream(t *testing.T) {
	g := NewStateGraph(schema()).AddNode("loop", add("x")).AddEdge(Start, "loop").AddEdge("loop", "loop")
	c, _ := g.Compile()
	ctx := runnables.WithConfig(context.Background(), runnables.Config{RecursionLimit: 3})
	_, err := c.Invoke(ctx, State{})
	if _, ok := err.(*GraphRecursionError); !ok {
		t.Fatal(err)
	}

	g2 := NewStateGraph(schema()).
		AddNode("a", func(ctx context.Context, _ State) (any, error) {
			StreamWriter(ctx)("hi")
			return State{"log": []any{"a"}}, nil
		}).AddEdge(Start, "a").AddEdge("a", End)
	c2, _ := g2.Compile()
	var modes []string
	for ev, err := range c2.StreamEvents(context.Background(), State{}, ModeValues, ModeUpdates, ModeCustom) {
		if err != nil {
			t.Fatal(err)
		}
		modes = append(modes, string(ev.Mode))
	}
	if got := strings.Join(modes, ","); got != "values,custom,updates,values" {
		t.Fatal(got)
	}
}

func TestSubgraphInterruptPropagates(t *testing.T) {
	inner, _ := NewStateGraph(schema()).
		AddNode("ask", func(ctx context.Context, _ State) (any, error) {
			return State{"log": []any{Interrupt(ctx, "q")}}, nil
		}).AddEdge(Start, "ask").AddEdge("ask", End).Compile()
	outer, _ := NewStateGraph(schema()).
		AddNode("sub", func(ctx context.Context, s State) (any, error) {
			o, err := inner.Invoke(ctx, State{})
			return State{"log": o["log"]}, err
		}).AddEdge(Start, "sub").AddEdge("sub", End).Compile(WithCheckpointer(checkpoint.NewMemory()))
	ctx := runnables.WithConfig(context.Background(), Thread("s"))
	out, _ := outer.Invoke(ctx, State{})
	if len(Interrupts(out)) != 1 {
		t.Fatal(out)
	}
	out, err := outer.Invoke(ctx, Command{Resume: "ans"})
	if err != nil || fmt.Sprint(out["log"]) != "[ans]" {
		t.Fatalf("%v %v", out, err)
	}
}

// subgraphWithSaver builds a parent whose node "sub" runs inner (with its own
// checkpointer, possibly shared with the parent) and returns both graphs.
func subgraphWithSaver(inner *Compiled, outerSaver checkpoint.Saver) *Compiled {
	outer, _ := NewStateGraph(schema()).
		AddNode("sub", func(ctx context.Context, s State) (any, error) {
			o, err := inner.Invoke(ctx, State{})
			return State{"log": o["log"]}, err
		}).AddEdge(Start, "sub").AddEdge("sub", End).Compile(WithCheckpointer(outerSaver))
	return outer
}

func TestSubgraphOwnCheckpointerPropagatesInterrupt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		inner func(s checkpoint.Saver) checkpoint.Saver
	}{
		{"same saver", func(s checkpoint.Saver) checkpoint.Saver { return s }},
		{"other saver", func(checkpoint.Saver) checkpoint.Saver { return checkpoint.NewMemory() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outerSaver := checkpoint.NewMemory()
			inner, _ := NewStateGraph(schema()).
				AddNode("ask", func(ctx context.Context, _ State) (any, error) {
					return State{"log": []any{Interrupt(ctx, "q")}}, nil
				}).AddEdge(Start, "ask").AddEdge("ask", End).Compile(WithCheckpointer(tc.inner(outerSaver)))
			outer := subgraphWithSaver(inner, outerSaver)
			ctx := runnables.WithConfig(context.Background(), Thread("s"))

			out, err := outer.Invoke(ctx, State{})
			if err != nil || len(Interrupts(out)) != 1 || Interrupts(out)[0].Value != "q" {
				t.Fatalf("parent must pause on the child's interrupt: %v %v", out, err)
			}
			// The child's paused checkpoint lives under the namespace "sub:0".
			cp, ok, _ := inner.Saver().Get(ctx, checkpoint.Config{ThreadID: "s", Namespace: "sub:0"})
			if !ok || len(cp.Next) == 0 {
				t.Fatalf("child checkpoint not found in its namespace: %+v %v", cp, ok)
			}
			out, err = outer.Invoke(ctx, Command{Resume: "ans"})
			if err != nil || fmt.Sprint(out["log"]) != "[ans]" || len(Interrupts(out)) != 0 {
				t.Fatalf("%v %v", out, err)
			}
		})
	}
}

func TestSubgraphOwnCheckpointerTwoInterrupts(t *testing.T) {
	outerSaver := checkpoint.NewMemory()
	inner, _ := NewStateGraph(schema()).
		AddNode("ask1", func(ctx context.Context, _ State) (any, error) {
			return State{"log": []any{Interrupt(ctx, "q1")}}, nil
		}).
		AddNode("ask2", func(ctx context.Context, _ State) (any, error) {
			return State{"log": []any{Interrupt(ctx, "q2")}}, nil
		}).
		AddEdge(Start, "ask1").AddEdge("ask1", "ask2").AddEdge("ask2", End).
		Compile(WithCheckpointer(outerSaver))
	outer := subgraphWithSaver(inner, outerSaver)
	ctx := runnables.WithConfig(context.Background(), Thread("two"))

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

func TestSubgraphNamespacesKeepCheckpointsApart(t *testing.T) {
	saver := checkpoint.NewMemory()
	inner, _ := NewStateGraph(schema()).
		AddNode("ask", func(ctx context.Context, _ State) (any, error) {
			return State{"log": []any{Interrupt(ctx, "q")}}, nil
		}).AddEdge(Start, "ask").AddEdge("ask", End).Compile(WithCheckpointer(saver))
	outer := subgraphWithSaver(inner, saver)
	ctx := runnables.WithConfig(context.Background(), Thread("ns"))

	if _, err := outer.Invoke(ctx, State{}); err != nil {
		t.Fatal(err)
	}
	rootCps, _ := saver.List(ctx, "ns", 0)
	for _, cp := range rootCps {
		if cp.Namespace != "" {
			t.Fatalf("List returned a subgraph checkpoint: %+v", cp)
		}
	}
	if len(rootCps) == 0 {
		t.Fatal("no root checkpoints")
	}
	child, ok, _ := saver.Get(ctx, checkpoint.Config{ThreadID: "ns", Namespace: "sub:0"})
	if !ok || child.Namespace != "sub:0" {
		t.Fatalf("child namespace: %+v %v", child, ok)
	}
	// Child checkpoints must not replace the root's: the root's latest is the
	// parent's paused checkpoint, not the child's.
	latest, _, _ := saver.Get(ctx, checkpoint.Config{ThreadID: "ns"})
	if latest.Namespace != "" || latest.ID == child.ID {
		t.Fatalf("root latest clobbered by child: %+v", latest)
	}
	if err := saver.DeleteThread(ctx, "ns"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := saver.Get(ctx, checkpoint.Config{ThreadID: "ns", Namespace: "sub:0"}); ok {
		t.Fatal("DeleteThread must remove every namespace")
	}
}

func TestMessagesReducer(t *testing.T) {
	g := NewStateGraph(Schema{"messages": Messages()}).
		AddNode("a", func(context.Context, State) (any, error) { return State{"messages": "hi"}, nil }).
		AddEdge(Start, "a").AddEdge("a", End)
	c, _ := g.Compile()
	out, err := c.Invoke(context.Background(), State{"messages": "yo"})
	if err != nil {
		t.Fatal(err)
	}
	if ms := out["messages"].([]messages.Message); len(ms) != 2 || ms[1].Content != "hi" {
		t.Fatal(ms)
	}
}

func TestCacheAndDefer(t *testing.T) {
	calls := 0
	g := NewStateGraph(schema()).
		AddNode("exp", func(context.Context, State) (any, error) {
			calls++
			return State{"log": []any{"exp"}}, nil
		}, WithCachePolicy(CachePolicy{})).
		AddEdge(Start, "exp").AddEdge("exp", End)
	c, _ := g.Compile(WithCache(NewMemoryCache()))
	for i := 0; i < 3; i++ {
		out, err := c.Invoke(context.Background(), State{"n": 1})
		if err != nil || fmt.Sprint(out["log"]) != "[exp]" {
			t.Fatal(out, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cache miss: %d calls", calls)
	}
	c.Invoke(context.Background(), State{"n": 2})
	if calls != 2 {
		t.Fatal("different input must miss")
	}

	// a (short branch) -> d(deferred) ; b -> b2 (long branch): d must run last, once
	d := NewStateGraph(schema()).
		AddNode("a", add("a")).AddNode("b", add("b")).AddNode("b2", add("b2")).
		AddNode("d", add("d"), Deferred()).
		AddEdge(Start, "a").AddEdge(Start, "b").AddEdge("a", "d").AddEdge("b", "b2").AddEdge("b2", "d").AddEdge("d", End)
	dc, _ := d.Compile()
	out, err := dc.Invoke(context.Background(), State{})
	if err != nil || fmt.Sprint(out["log"]) != "[a b b2 d]" {
		t.Fatalf("%v %v", out, err)
	}
}

// nestedGraphs builds outer whose node "sub" invokes inner (two nodes).
func nestedGraphs(t *testing.T) *Compiled {
	t.Helper()
	inner, err := NewStateGraph(schema()).
		AddNode("leaf", add("in")).AddEdge(Start, "leaf").AddEdge("leaf", End).Compile()
	if err != nil {
		t.Fatal(err)
	}
	outer, err := NewStateGraph(schema()).
		AddNode("sub", func(ctx context.Context, s State) (any, error) {
			o, err := inner.Invoke(ctx, State{})
			return State{"log": o["log"]}, err
		}).AddEdge(Start, "sub").AddEdge("sub", End).Compile()
	if err != nil {
		t.Fatal(err)
	}
	return outer
}

func TestStreamSubgraphsTagsNamespace(t *testing.T) {
	outer := nestedGraphs(t)
	var child, root int
	for ev, err := range outer.StreamEvents(context.Background(), State{}, ModeUpdates, Subgraphs()) {
		if err != nil {
			t.Fatal(err)
		}
		if len(ev.Namespace) == 0 {
			root++
			continue
		}
		child++
		if fmt.Sprint(ev.Namespace) != "[sub:0]" || ev.Node != "leaf" {
			t.Fatalf("child event %+v", ev)
		}
	}
	if root != 1 || child != 1 {
		t.Fatalf("root=%d child=%d", root, child)
	}
}

func TestStreamWithoutSubgraphsSkipsChildEvents(t *testing.T) {
	for ev, err := range nestedGraphs(t).StreamEvents(context.Background(), State{}, ModeUpdates) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Namespace != nil {
			t.Fatalf("child event without Subgraphs(): %+v", ev)
		}
	}
}
