package graph_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	. "github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type typedState struct {
	Messages []messages.Message `graph:"messages,reducer=messages"`
	Count    int                `graph:"count"`
	Log      []string           `graph:"log,reducer=append"`
	Plan     plan               `graph:"plan"`
	Ignored  string
}

type plan struct {
	Steps []string `json:"steps"`
}

func TestTypedEquivalentToMap(t *testing.T) {
	// map-based version
	mg := NewStateGraph(Schema{"log": Append(), "n": channels.LastValue()}).
		AddNode("a", add("a")).AddNode("b", add("b")).AddNode("c", add("c")).
		AddEdge(Start, "a").
		AddConditionalEdges("a", func(_ context.Context, s State) (any, error) {
			if s["n"] == 1 {
				return "b", nil
			}
			return "c", nil
		}).
		AddEdge("b", End).AddEdge("c", End)
	mc, _ := mg.Compile()
	mo, err := mc.Invoke(context.Background(), State{"n": 1})
	if err != nil {
		t.Fatal(err)
	}

	type s struct {
		Log []string `graph:"log,reducer=append"`
		N   int      `graph:"n"`
	}
	tg := NewTyped[s]().
		AddNode("a", func(_ context.Context, st s) (Update, error) {
			return Update{"log": []string{"a"}}, nil
		}).
		AddNode("b", func(_ context.Context, st s) (Update, error) {
			return Update{"log": []string{"b"}}, nil
		}).
		AddNode("c", func(_ context.Context, st s) (Update, error) {
			return Update{"log": []string{"c"}}, nil
		}).
		SetEntryPoint("a").
		AddConditionalEdges("a", func(_ context.Context, st s) (any, error) {
			if st.N == 1 {
				return "b", nil
			}
			return "c", nil
		}).
		AddEdge("b", End).AddEdge("c", End)
	tc, err := tg.Compile()
	if err != nil {
		t.Fatal(err)
	}
	to, ints, err := tc.Invoke(context.Background(), s{N: 1})
	if err != nil || len(ints) != 0 {
		t.Fatal(err, ints)
	}
	if fmt.Sprint(to.Log) != fmt.Sprint(mo["log"]) || to.N != mo["n"] {
		t.Fatalf("typed %v, map %v", to, mo)
	}
}

func TestTypedReducers(t *testing.T) {
	tg := NewTyped[typedState]().
		AddNode("a", func(_ context.Context, s typedState) (Update, error) {
			return Update{
				"messages": []messages.Message{messages.Human("hi")},
				"count":    s.Count + 1,
				"log":      []string{"a", "x"},
				"plan":     plan{Steps: []string{"one"}},
			}, nil
		}).
		AddNode("b", func(_ context.Context, s typedState) (Update, error) {
			if len(s.Messages) != 1 || s.Count != 1 || s.Plan.Steps[0] != "one" {
				return nil, fmt.Errorf("b sees %+v", s)
			}
			return Update{"messages": []messages.Message{messages.AI("hello")}, "log": []string{"b"}}, nil
		}).
		SetEntryPoint("a").AddEdge("a", "b").SetFinishPoint("b")
	c, err := tg.Compile()
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := c.Invoke(context.Background(), typedState{Count: 0, Log: []string{"start"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 2 || out.Messages[1].Content != "hello" {
		t.Fatalf("messages %+v", out.Messages)
	}
	// the input Log is an update too, so it is appended before the nodes' writes
	if fmt.Sprint(out.Log) != "[start a x b]" {
		t.Fatalf("log %v", out.Log)
	}
	if out.Count != 1 || out.Plan.Steps[0] != "one" || out.Ignored != "" {
		t.Fatalf("out %+v", out)
	}
}

func TestTypedBadTags(t *testing.T) {
	cases := map[string]func() error{
		"unknown reducer": func() error {
			type S struct {
				X int `graph:"x,reducer=bogus"`
			}
			_, err := NewTyped[S]().AddNode("a", func(context.Context, S) (Update, error) { return nil, nil }).
				SetEntryPoint("a").SetFinishPoint("a").Compile()
			return err
		},
		"duplicate key": func() error {
			type S struct {
				X int `graph:"x"`
				Y int `graph:"x"`
			}
			_, err := NewTyped[S]().Compile()
			return err
		},
		"messages on int": func() error {
			type S struct {
				X int `graph:"x,reducer=messages"`
			}
			_, err := NewTyped[S]().Compile()
			return err
		},
		"sum on string": func() error {
			type S struct {
				X string `graph:"x,reducer=sum"`
			}
			_, err := NewTyped[S]().Compile()
			return err
		},
		"unexported tagged": func() error {
			type S struct {
				x int `graph:"x"`
			}
			_, err := NewTyped[S]().Compile()
			return err
		},
		"not a struct": func() error {
			_, err := NewTyped[int]().Compile()
			return err
		},
		"reserved key": func() error {
			type S struct {
				X int `graph:"__interrupt__"`
			}
			_, err := NewTyped[S]().Compile()
			return err
		},
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Errorf("%s: want Compile error", name)
		}
	}
}

func TestTypedDuplicateKeyMessage(t *testing.T) {
	type S struct {
		X int `graph:"x"`
		Y int `graph:"x"`
	}
	tg := NewTyped[S]().AddNode("a", func(context.Context, S) (Update, error) { return nil, nil }).
		SetEntryPoint("a").SetFinishPoint("a")
	_, err := tg.Compile()
	if err == nil || !strings.Contains(err.Error(), "duplicate state key") {
		t.Fatalf("got %v", err)
	}
}

func TestTypedUnknownUpdateKey(t *testing.T) {
	type S struct {
		Count int `graph:"count"`
	}
	c, err := NewTyped[S]().
		AddNode("a", func(context.Context, S) (Update, error) {
			return Update{"count": 1, "nope": 2}, nil
		}).
		SetEntryPoint("a").SetFinishPoint("a").Compile()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Invoke(context.Background(), S{}); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("want unknown key error, got %v", err)
	}
}

var errInvalid = errors.New("invalid state")

type validated struct {
	N int `graph:"n"`
}

func (v validated) Validate() error {
	if v.N < 0 {
		return errInvalid
	}
	return nil
}

func TestTypedValidate(t *testing.T) {
	calls := 0
	c, err := NewTyped[validated]().
		AddNode("a", func(_ context.Context, s validated) (Update, error) {
			calls++
			return Update{"n": s.N - 10}, nil
		}).
		AddNode("b", func(_ context.Context, s validated) (Update, error) {
			calls++
			return nil, nil
		}).
		SetEntryPoint("a").AddEdge("a", "b").SetFinishPoint("b").Compile()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Invoke(context.Background(), validated{N: 5})
	if !errors.Is(err, errInvalid) {
		t.Fatalf("want Validate error on node b input, got %v", err)
	}
	if calls != 1 {
		// a runs; b's input fails validation before b's function runs
		t.Fatalf("calls %d", calls)
	}
}

type approval struct {
	Answer string `graph:"answer"`
	Asked  int    `graph:"asked"`
}

func TestTypedInterruptResume(t *testing.T) {
	checkpoint.Strict(true)
	defer checkpoint.Strict(false)

	c, err := NewTyped[typedState]().
		AddNode("ask", func(ctx context.Context, s typedState) (Update, error) {
			ans := Interrupt(ctx, "name?")
			return Update{"log": []string{fmt.Sprint(ans)}, "plan": plan{Steps: []string{"after"}}}, nil
		}).
		AddNode("done", func(_ context.Context, s typedState) (Update, error) {
			return Update{"count": s.Count + 1}, nil
		}).
		SetEntryPoint("ask").AddEdge("ask", "done").SetFinishPoint("done").
		Compile(WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := runnables.WithConfig(context.Background(), Thread("typed"))
	out, ints, err := c.Invoke(ctx, typedState{Count: 41})
	if err != nil {
		t.Fatal(err)
	}
	if len(ints) != 1 || ints[0].Node != "ask" || ints[0].Value != "name?" {
		t.Fatalf("interrupts %+v", ints)
	}
	if out.Count != 41 {
		t.Fatalf("paused state %+v", out)
	}

	out, ints, err = c.Invoke(ctx, Command{Resume: "bob"})
	if err != nil || len(ints) != 0 {
		t.Fatal(err, ints)
	}
	if fmt.Sprint(out.Log) != "[bob]" || out.Count != 42 || out.Plan.Steps[0] != "after" {
		t.Fatalf("resumed %+v", out)
	}
	if c.Unwrap().Saver() == nil {
		t.Fatal("Unwrap lost the saver")
	}
	snap, err := c.Unwrap().GetState(ctx)
	if err != nil || snap.Values["count"] != 42 {
		t.Fatalf("GetState %v %v", snap.Values, err)
	}
}

func TestTypedStream(t *testing.T) {
	c, _ := NewTyped[approval]().
		AddNode("a", func(context.Context, approval) (Update, error) { return Update{"asked": 1}, nil }).
		AddNode("b", func(_ context.Context, s approval) (Update, error) {
			return Update{"answer": fmt.Sprint("asked=", s.Asked)}, nil
		}).
		SetEntryPoint("a").AddEdge("a", "b").SetFinishPoint("b").Compile()
	var last approval
	n := 0
	for st, err := range c.Stream(context.Background(), approval{}) {
		if err != nil {
			t.Fatal(err)
		}
		last = st
		n++
	}
	// the input state is emitted first, then one value per super-step
	if n != 3 || last.Answer != "asked=1" {
		t.Fatalf("n=%d last=%+v", n, last)
	}
}
