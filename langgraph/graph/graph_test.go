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
