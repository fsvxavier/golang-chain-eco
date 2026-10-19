package graph_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	. "github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

var errSave = errors.New("save failed")

// countingSaver counts the Put calls and can delay or fail them.
type countingSaver struct {
	checkpoint.Saver
	delay    time.Duration
	failAt   int64 // the Put with this 1-based number fails; 0 = never
	started  atomic.Int64
	finished atomic.Int64
	storedOK atomic.Int64
}

func (s *countingSaver) Put(ctx context.Context, cp checkpoint.Checkpoint) (string, error) {
	n := s.started.Add(1)
	defer s.finished.Add(1)
	time.Sleep(s.delay)
	if s.failAt > 0 && n == s.failAt {
		return "", errSave
	}
	id, err := s.Saver.Put(ctx, cp)
	if err == nil {
		s.storedOK.Add(1)
	}
	return id, err
}

// linear graph a -> b -> c; b errors while *fail is set.
func durGraph(fail *atomic.Bool, opts ...CompileOption) *Compiled {
	g := NewStateGraph(schema()).
		AddNode("a", add("a")).
		AddNode("b", func(ctx context.Context, s State) (any, error) {
			if fail != nil && fail.Load() {
				return nil, errors.New("node b broke")
			}
			return add("b")(ctx, s)
		}).
		AddNode("c", add("c")).
		AddEdge(Start, "a").AddEdge("a", "b").AddEdge("b", "c").AddEdge("c", End)
	c, err := g.Compile(opts...)
	if err != nil {
		panic(err)
	}
	return c
}

var allModes = []Durability{DurabilitySync, DurabilityAsync, DurabilityExit}

func TestDurabilityPutCounts(t *testing.T) {
	counts := map[Durability]int64{}
	logs := map[Durability]string{}
	for _, d := range allModes {
		s := &countingSaver{Saver: checkpoint.NewMemory(), delay: 2 * time.Millisecond}
		c := durGraph(nil, WithCheckpointer(s))
		ctx := WithRunDurability(runnables.WithConfig(context.Background(), Thread("d")), d)
		out, err := c.Invoke(ctx, State{})
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		counts[d] = s.finished.Load()
		logs[d] = fmt.Sprint(out["log"])
	}
	if counts[DurabilityExit] != 1 {
		t.Fatalf("exit: want 1 put, got %d", counts[DurabilityExit])
	}
	if counts[DurabilitySync] <= 1 {
		t.Fatalf("sync: want one put per step, got %d", counts[DurabilitySync])
	}
	if counts[DurabilityAsync] != counts[DurabilitySync] {
		t.Fatalf("async puts %d != sync puts %d", counts[DurabilityAsync], counts[DurabilitySync])
	}
	if logs[DurabilitySync] != "[a b c]" || logs[DurabilityAsync] != logs[DurabilitySync] || logs[DurabilityExit] != logs[DurabilitySync] {
		t.Fatalf("final states differ: %v", logs)
	}
}

func TestDurabilityCompileDefaultAndRunOverride(t *testing.T) {
	s := &countingSaver{Saver: checkpoint.NewMemory()}
	c := durGraph(nil, WithCheckpointer(s), WithDurability(DurabilityExit))
	ctx := runnables.WithConfig(context.Background(), Thread("c"))
	if _, err := c.Invoke(ctx, State{}); err != nil || s.finished.Load() != 1 {
		t.Fatalf("compile-time exit: puts=%d err=%v", s.finished.Load(), err)
	}
	s2 := &countingSaver{Saver: checkpoint.NewMemory()}
	c2 := durGraph(nil, WithCheckpointer(s2), WithDurability(DurabilityExit))
	ctx2 := WithRunDurability(runnables.WithConfig(context.Background(), Thread("c2")), DurabilitySync)
	if _, err := c2.Invoke(ctx2, State{}); err != nil || s2.finished.Load() <= 1 {
		t.Fatalf("per-run sync must override compile exit: puts=%d err=%v", s2.finished.Load(), err)
	}
	bad := WithRunDurability(runnables.WithConfig(context.Background(), Thread("x")), "bogus")
	if _, err := c.Invoke(bad, State{}); err == nil {
		t.Fatal("unknown durability must fail")
	}
}

func TestAsyncInvokeWaitsForLastPut(t *testing.T) {
	s := &countingSaver{Saver: checkpoint.NewMemory(), delay: 20 * time.Millisecond}
	c := durGraph(nil, WithCheckpointer(s))
	ctx := WithRunDurability(runnables.WithConfig(context.Background(), Thread("w")), DurabilityAsync)
	if _, err := c.Invoke(ctx, State{}); err != nil {
		t.Fatal(err)
	}
	if s.started.Load() == 0 || s.finished.Load() != s.started.Load() {
		t.Fatalf("Invoke returned with saves in flight: started=%d finished=%d", s.started.Load(), s.finished.Load())
	}
	snap, err := c.GetState(ctx)
	if err != nil || len(snap.Next) != 0 || fmt.Sprint(snap.Values["log"]) != "[a b c]" {
		t.Fatalf("latest checkpoint not the final state: %+v %v", snap, err)
	}
}

func TestAsyncSaveErrorAbortsRun(t *testing.T) {
	s := &countingSaver{Saver: checkpoint.NewMemory(), failAt: 2}
	c := durGraph(nil, WithCheckpointer(s))
	ctx := WithRunDurability(runnables.WithConfig(context.Background(), Thread("e")), DurabilityAsync)
	out, err := c.Invoke(ctx, State{})
	if !errors.Is(err, errSave) || out != nil {
		t.Fatalf("want save error, got out=%v err=%v", out, err)
	}
	if s.started.Load() != s.finished.Load() {
		t.Fatalf("save still running after Invoke returned")
	}
}

func TestExitModeNodeErrorKeepsCheckpoint(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	s := checkpoint.NewMemory()
	c := durGraph(&fail, WithCheckpointer(s))
	ctx := WithRunDurability(runnables.WithConfig(context.Background(), Thread("n")), DurabilityExit)
	if _, err := c.Invoke(ctx, State{}); err == nil {
		t.Fatal("node error must surface")
	}
	snap, err := c.GetState(ctx)
	if err != nil || fmt.Sprint(snap.Values["log"]) != "[a]" || len(snap.Next) != 1 || snap.Next[0] != "b" {
		t.Fatalf("want checkpoint paused before b with log [a]: %+v %v", snap, err)
	}
	fail.Store(false)
	out, err := c.Invoke(ctx, nil) // resume from the saved checkpoint
	if err != nil || fmt.Sprint(out["log"]) != "[a b c]" {
		t.Fatalf("resume after failure: %v %v", out, err)
	}
}

func TestDurabilityInterruptResume(t *testing.T) {
	for _, d := range allModes {
		t.Run(string(d), func(t *testing.T) {
			g := NewStateGraph(schema()).
				AddNode("a", add("a")).
				AddNode("ask", func(ctx context.Context, _ State) (any, error) {
					return State{"log": []any{Interrupt(ctx, "q")}}, nil
				}).
				AddNode("c", add("c")).
				AddEdge(Start, "a").AddEdge("a", "ask").AddEdge("ask", "c").AddEdge("c", End)
			c, err := g.Compile(WithCheckpointer(checkpoint.NewMemory()))
			if err != nil {
				t.Fatal(err)
			}
			ctx := WithRunDurability(runnables.WithConfig(context.Background(), Thread("i")), d)
			out, err := c.Invoke(ctx, State{})
			if err != nil || len(Interrupts(out)) != 1 || Interrupts(out)[0].Value != "q" {
				t.Fatalf("want pause: %v %v", out, err)
			}
			out, err = c.Invoke(ctx, Command{Resume: "x"})
			if err != nil || fmt.Sprint(out["log"]) != "[a x c]" || len(Interrupts(out)) != 0 {
				t.Fatalf("resume: %v %v", out, err)
			}
		})
	}
}

func TestDurabilitySubgraphPauseSavesInEveryMode(t *testing.T) {
	for _, d := range allModes {
		t.Run(string(d), func(t *testing.T) {
			outerSaver := checkpoint.NewMemory()
			inner, _ := NewStateGraph(schema()).
				AddNode("ask", func(ctx context.Context, _ State) (any, error) {
					return State{"log": []any{Interrupt(ctx, "q")}}, nil
				}).AddEdge(Start, "ask").AddEdge("ask", End).Compile(WithCheckpointer(checkpoint.NewMemory()))
			outer := subgraphWithSaver(inner, outerSaver)
			ctx := WithRunDurability(runnables.WithConfig(context.Background(), Thread("sg")), d)
			out, err := outer.Invoke(ctx, State{})
			if err != nil || len(Interrupts(out)) != 1 {
				t.Fatalf("parent must pause: %v %v", out, err)
			}
			cp, ok, _ := inner.Saver().Get(ctx, checkpoint.Config{ThreadID: "sg", Namespace: "sub:0"})
			if !ok || len(cp.Next) == 0 {
				t.Fatalf("child pause not saved: %+v %v", cp, ok)
			}
			out, err = outer.Invoke(ctx, Command{Resume: "ans"})
			if err != nil || fmt.Sprint(out["log"]) != "[ans]" {
				t.Fatalf("resume: %v %v", out, err)
			}
		})
	}
}

func TestAsyncCancelDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	s := &countingSaver{Saver: checkpoint.NewMemory(), delay: 30 * time.Millisecond}
	c := durGraph(nil, WithCheckpointer(s))
	base, cancel := context.WithCancel(context.Background())
	ctx := WithRunDurability(runnables.WithConfig(base, Thread("l")), DurabilityAsync)
	time.AfterFunc(10*time.Millisecond, cancel)
	_, _ = c.Invoke(ctx, State{})
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Fatalf("goroutines: before=%d after=%d", before, n)
	}
}
