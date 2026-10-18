// Package functional is the LangGraph functional API: write a workflow as
// plain Go with Task calls and Interrupt, and get checkpointing, resumption
// and human-in-the-loop for free (@entrypoint / @task).
//
// On resume the entrypoint body runs again from the top; Task results already
// produced are replayed from the checkpoint instead of re-executing, so
// side effects belong inside tasks. Call Interrupt only from the entrypoint
// body, not from inside a task.
package functional

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Result is the outcome of one entrypoint run.
type Result[O any] struct {
	Value O
	// Interrupt is set when the workflow paused; resume with Entrypoint.Resume.
	Interrupt *graph.InterruptInfo
}

// Entrypoint is a workflow function with optional persistence.
type Entrypoint[I, O any] struct {
	name  string
	fn    func(ctx context.Context, in I) (O, error)
	saver checkpoint.Saver
}

// New wraps fn. saver may be nil (no resume support, Interrupt then fails).
func New[I, O any](name string, saver checkpoint.Saver, fn func(ctx context.Context, in I) (O, error)) *Entrypoint[I, O] {
	return &Entrypoint[I, O]{name: name, fn: fn, saver: saver}
}

type runState struct {
	mu    sync.Mutex
	idx   int
	cache map[string]any
}

type rsKey struct{}

// Invoke starts a fresh run on the configured thread.
func (e *Entrypoint[I, O]) Invoke(ctx context.Context, in I) (Result[O], error) {
	return e.run(ctx, in, map[string]any{}, nil)
}

// Resume continues a paused run, answering its pending Interrupt with v.
func (e *Entrypoint[I, O]) Resume(ctx context.Context, v any) (Result[O], error) {
	var zero Result[O]
	thread, err := e.thread(ctx)
	if err != nil {
		return zero, err
	}
	cp, ok, err := e.saver.Get(ctx, checkpoint.Config{ThreadID: thread})
	if err != nil {
		return zero, err
	}
	if !ok || len(cp.Next) == 0 || !cp.Next[0].Interrupted {
		return zero, errors.New("functional: nothing to resume")
	}
	in, _ := cp.Channels["input"].(I)
	cache, _ := cp.Channels["tasks"].(map[string]any)
	resumes := append(append([]any(nil), cp.Next[0].Resumes...), v)
	return e.run(ctx, in, cloneMap(cache), resumes)
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (e *Entrypoint[I, O]) thread(ctx context.Context) (string, error) {
	if e.saver == nil {
		return "", errors.New("functional: persistence needs a saver")
	}
	t, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	if t == "" {
		return "", errors.New("functional: missing thread_id (use graph.Thread)")
	}
	return t, nil
}

func (e *Entrypoint[I, O]) run(ctx context.Context, in I, cache map[string]any, resumes []any) (Result[O], error) {
	var res Result[O]
	rs := &runState{cache: cache}
	ctx = context.WithValue(ctx, rsKey{}, rs)
	var out O
	val, interrupted, err := graph.RunInterruptible(ctx, resumes, func(ctx context.Context) (err error) {
		out, err = e.fn(ctx, in)
		return
	})
	if err != nil {
		return res, err
	}
	if e.saver == nil {
		if interrupted {
			return res, errors.New("functional: Interrupt needs a saver")
		}
		res.Value = out
		return res, nil
	}
	thread, err := e.thread(ctx)
	if err != nil {
		return res, err
	}
	cp := checkpoint.Checkpoint{ThreadID: thread, Source: "loop", Channels: map[string]any{"input": in, "tasks": rs.cache}}
	if interrupted {
		cp.Next = []checkpoint.Task{{Node: e.name, Interrupted: true, InterruptValue: val, Resumes: resumes}}
		res.Interrupt = &graph.InterruptInfo{ID: e.name + ":0", Node: e.name, Value: val}
	} else {
		cp.Channels["output"] = out
		res.Value = out
	}
	_, err = e.saver.Put(ctx, cp)
	return res, err
}

// Future is a task running in the background.
type Future[T any] struct {
	done chan struct{}
	v    T
	err  error
}

// Result waits for the task.
func (f *Future[T]) Result() (T, error) { <-f.done; return f.v, f.err }

// Task starts fn concurrently and returns its Future. Inside an entrypoint
// with a saver, results are cached by call order so resumed runs skip
// finished tasks; keep the call order of tasks deterministic.
func Task[T any](ctx context.Context, name string, fn func(ctx context.Context) (T, error)) *Future[T] {
	f := &Future[T]{done: make(chan struct{})}
	rs, _ := ctx.Value(rsKey{}).(*runState)
	var key string
	if rs != nil {
		rs.mu.Lock()
		key = fmt.Sprintf("%s#%d", name, rs.idx)
		rs.idx++
		if v, ok := rs.cache[key]; ok {
			rs.mu.Unlock()
			f.v, _ = v.(T)
			close(f.done)
			return f
		}
		rs.mu.Unlock()
	}
	go func() {
		defer close(f.done)
		f.v, f.err = fn(ctx)
		if f.err == nil && rs != nil {
			rs.mu.Lock()
			rs.cache[key] = f.v
			rs.mu.Unlock()
		}
	}()
	return f
}
