// Package runnables is the Go port of the LangChain Expression Language
// (langchain_core.runnables): a typed Runnable interface plus composition
// primitives (Pipe, Parallel, Passthrough, Assign, Branch, Retry, Fallbacks).
//
// Python composes with the | operator; in Go use Pipe/Pipe3/Pipe4 for typed
// chains or Sequence for dynamically typed ones.
package runnables

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
)

// Runnable is a unit of work that maps I to O.
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, in I) (O, error)
}

// Streamer is implemented by runnables that can emit incremental output.
type Streamer[I, O any] interface {
	Stream(ctx context.Context, in I) iter.Seq2[O, error]
}

// Batcher is implemented by runnables with a native batch implementation.
type Batcher[I, O any] interface {
	Batch(ctx context.Context, ins []I) ([]O, error)
}

// Config mirrors RunnableConfig. It travels in the context.
type Config struct {
	RunName        string
	Tags           []string
	Metadata       map[string]any
	Callbacks      []callbacks.Handler
	MaxConcurrency int
	RecursionLimit int
	// Configurable holds per-run values such as "thread_id".
	Configurable map[string]any
}

type cfgKey struct{}

// WithConfig merges c into the configuration carried by ctx.
func WithConfig(ctx context.Context, c Config) context.Context {
	cur := GetConfig(ctx)
	if c.RunName != "" {
		cur.RunName = c.RunName
	}
	cur.Tags = append(append([]string(nil), cur.Tags...), c.Tags...)
	if len(c.Metadata) > 0 {
		m := maps.Clone(cur.Metadata)
		if m == nil {
			m = map[string]any{}
		}
		maps.Copy(m, c.Metadata)
		cur.Metadata = m
	}
	if len(c.Configurable) > 0 {
		m := maps.Clone(cur.Configurable)
		if m == nil {
			m = map[string]any{}
		}
		maps.Copy(m, c.Configurable)
		cur.Configurable = m
	}
	if c.MaxConcurrency > 0 {
		cur.MaxConcurrency = c.MaxConcurrency
	}
	if c.RecursionLimit > 0 {
		cur.RecursionLimit = c.RecursionLimit
	}
	ctx = callbacks.WithHandlers(ctx, c.Callbacks...)
	return context.WithValue(ctx, cfgKey{}, cur)
}

// GetConfig returns the configuration carried by ctx (zero value if none).
func GetConfig(ctx context.Context) Config {
	c, _ := ctx.Value(cfgKey{}).(Config)
	return c
}

// Configurable returns a configurable value from ctx.
func Configurable(ctx context.Context, key string) (any, bool) {
	v, ok := GetConfig(ctx).Configurable[key]
	return v, ok
}

// Stream streams r if it supports it; otherwise it yields the Invoke result.
func Stream[I, O any](ctx context.Context, r Runnable[I, O], in I) iter.Seq2[O, error] {
	if s, ok := r.(Streamer[I, O]); ok {
		return s.Stream(ctx, in)
	}
	return func(yield func(O, error) bool) {
		yield(r.Invoke(ctx, in))
	}
}

// Batch runs r over ins concurrently (bounded by Config.MaxConcurrency),
// preserving order. The first error is returned alongside partial results.
func Batch[I, O any](ctx context.Context, r Runnable[I, O], ins []I) ([]O, error) {
	if b, ok := r.(Batcher[I, O]); ok {
		return b.Batch(ctx, ins)
	}
	out := make([]O, len(ins))
	errs := make([]error, len(ins))
	limit := GetConfig(ctx).MaxConcurrency
	if limit <= 0 {
		limit = len(ins)
	}
	sem := make(chan struct{}, max(limit, 1))
	var wg sync.WaitGroup
	for i, in := range ins {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			out[i], errs[i] = r.Invoke(ctx, in)
		}()
	}
	wg.Wait()
	return out, errors.Join(errs...)
}

// Func adapts a function into a Runnable (RunnableLambda).
type Func[I, O any] struct {
	Name string
	Fn   func(ctx context.Context, in I) (O, error)
}

// Lambda wraps fn as a named Runnable.
func Lambda[I, O any](name string, fn func(ctx context.Context, in I) (O, error)) *Func[I, O] {
	return &Func[I, O]{Name: name, Fn: fn}
}

// Invoke implements Runnable and reports chain callbacks.
func (f *Func[I, O]) Invoke(ctx context.Context, in I) (O, error) {
	ctx, end := callbacks.Start(ctx, callbacks.ChainStart, f.Name, in)
	out, err := f.Fn(ctx, in)
	end(out, err)
	return out, err
}

type pipe[A, B, C any] struct {
	first  Runnable[A, B]
	second Runnable[B, C]
}

// Pipe composes two runnables (a | b).
func Pipe[A, B, C any](a Runnable[A, B], b Runnable[B, C]) Runnable[A, C] {
	return pipe[A, B, C]{a, b}
}

// Pipe3 composes three runnables.
func Pipe3[A, B, C, D any](a Runnable[A, B], b Runnable[B, C], c Runnable[C, D]) Runnable[A, D] {
	return Pipe(Pipe(a, b), c)
}

// Pipe4 composes four runnables.
func Pipe4[A, B, C, D, E any](a Runnable[A, B], b Runnable[B, C], c Runnable[C, D], d Runnable[D, E]) Runnable[A, E] {
	return Pipe(Pipe3(a, b, c), d)
}

func (p pipe[A, B, C]) Invoke(ctx context.Context, in A) (C, error) {
	ctx, end := callbacks.Start(ctx, callbacks.ChainStart, "RunnableSequence", in)
	mid, err := p.first.Invoke(ctx, in)
	if err != nil {
		var zero C
		end(nil, err)
		return zero, err
	}
	out, err := p.second.Invoke(ctx, mid)
	end(out, err)
	return out, err
}

// Stream invokes the first step and streams the last one, matching how
// LCEL streams the final streaming-capable component.
func (p pipe[A, B, C]) Stream(ctx context.Context, in A) iter.Seq2[C, error] {
	return func(yield func(C, error) bool) {
		mid, err := p.first.Invoke(ctx, in)
		if err != nil {
			var zero C
			yield(zero, err)
			return
		}
		for v, err := range Stream(ctx, p.second, mid) {
			if !yield(v, err) || err != nil {
				return
			}
		}
	}
}

// Any is the dynamically typed runnable used by Sequence and Parallel.
type Any = Runnable[any, any]

// Erase converts a typed runnable into Any (the input must be assignable to I).
func Erase[I, O any](r Runnable[I, O]) Any {
	return Lambda("erased", func(ctx context.Context, in any) (any, error) {
		v, ok := in.(I)
		if !ok && in != nil {
			var zero I
			return nil, fmt.Errorf("runnables: expected input %T, got %T", zero, in)
		}
		return r.Invoke(ctx, v)
	})
}

// Sequence chains dynamically typed runnables.
func Sequence(steps ...Any) Any {
	return Lambda("RunnableSequence", func(ctx context.Context, in any) (any, error) {
		var err error
		for _, s := range steps {
			if in, err = s.Invoke(ctx, in); err != nil {
				return nil, err
			}
		}
		return in, nil
	})
}

// Parallel runs every branch on the same input concurrently and returns a map
// of branch name to output (RunnableParallel / dict literal in LCEL).
func Parallel[I any](branches map[string]Runnable[I, any]) Runnable[I, map[string]any] {
	return Lambda("RunnableParallel", func(ctx context.Context, in I) (map[string]any, error) {
		var mu sync.Mutex
		var wg sync.WaitGroup
		out := make(map[string]any, len(branches))
		var errs []error
		for k, b := range branches {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, err := b.Invoke(ctx, in)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", k, err))
					return
				}
				out[k] = v
			}()
		}
		wg.Wait()
		return out, errors.Join(errs...)
	})
}

// Passthrough returns its input unchanged (RunnablePassthrough).
func Passthrough[T any]() Runnable[T, T] {
	return Lambda("RunnablePassthrough", func(_ context.Context, in T) (T, error) { return in, nil })
}

// Assign runs branches on the input map and merges their outputs into a copy
// of it (RunnablePassthrough.assign).
func Assign(branches map[string]Runnable[map[string]any, any]) Runnable[map[string]any, map[string]any] {
	par := Parallel(branches)
	return Lambda("RunnableAssign", func(ctx context.Context, in map[string]any) (map[string]any, error) {
		extra, err := par.Invoke(ctx, in)
		if err != nil {
			return nil, err
		}
		out := maps.Clone(in)
		if out == nil {
			out = map[string]any{}
		}
		maps.Copy(out, extra)
		return out, nil
	})
}

// Pick selects keys from a map input (RunnablePick). A single key returns the
// bare value; several keys return a sub-map.
func Pick(keys ...string) Runnable[map[string]any, any] {
	return Lambda("RunnablePick", func(_ context.Context, in map[string]any) (any, error) {
		if len(keys) == 1 {
			return in[keys[0]], nil
		}
		out := map[string]any{}
		for _, k := range keys {
			if v, ok := in[k]; ok {
				out[k] = v
			}
		}
		return out, nil
	})
}

// Case is one arm of a Branch.
type Case[I, O any] struct {
	When func(ctx context.Context, in I) (bool, error)
	Then Runnable[I, O]
}

// Branch picks the first case whose condition holds, else def (RunnableBranch).
func Branch[I, O any](def Runnable[I, O], cases ...Case[I, O]) Runnable[I, O] {
	return Lambda("RunnableBranch", func(ctx context.Context, in I) (O, error) {
		for _, c := range cases {
			ok, err := c.When(ctx, in)
			if err != nil {
				var zero O
				return zero, err
			}
			if ok {
				return c.Then.Invoke(ctx, in)
			}
		}
		return def.Invoke(ctx, in)
	})
}

// RetryOptions configures WithRetry.
type RetryOptions struct {
	MaxAttempts     int           // total attempts, default 3
	InitialInterval time.Duration // default 500ms
	BackoffFactor   float64       // default 2
	MaxInterval     time.Duration // default 30s
	Jitter          bool
	RetryIf         func(error) bool // default: retry every error
}

// Backoff runs fn with exponential backoff according to o.
func Backoff(ctx context.Context, o RetryOptions, fn func() error) error {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 3
	}
	if o.InitialInterval <= 0 {
		o.InitialInterval = 500 * time.Millisecond
	}
	if o.BackoffFactor <= 0 {
		o.BackoffFactor = 2
	}
	if o.MaxInterval <= 0 {
		o.MaxInterval = 30 * time.Second
	}
	wait := o.InitialInterval
	var err error
	for attempt := 1; ; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if attempt >= o.MaxAttempts || (o.RetryIf != nil && !o.RetryIf(err)) {
			return err
		}
		d := wait
		if o.Jitter {
			d += time.Duration(rand.Int64N(int64(d)/2 + 1))
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(d):
		}
		wait = min(time.Duration(float64(wait)*o.BackoffFactor), o.MaxInterval)
	}
}

// WithRetry retries r on failure with exponential backoff.
func WithRetry[I, O any](r Runnable[I, O], o RetryOptions) Runnable[I, O] {
	return Lambda("RunnableRetry", func(ctx context.Context, in I) (O, error) {
		var out O
		err := Backoff(ctx, o, func() (err error) {
			out, err = r.Invoke(ctx, in)
			return err
		})
		return out, err
	})
}

// WithFallbacks tries r, then each fallback in order, until one succeeds.
func WithFallbacks[I, O any](r Runnable[I, O], fallbacks ...Runnable[I, O]) Runnable[I, O] {
	return Lambda("RunnableWithFallbacks", func(ctx context.Context, in I) (O, error) {
		out, err := r.Invoke(ctx, in)
		if err == nil {
			return out, nil
		}
		errs := []error{err}
		for _, f := range fallbacks {
			if out, err = f.Invoke(ctx, in); err == nil {
				return out, nil
			}
			errs = append(errs, err)
		}
		return out, errors.Join(errs...)
	})
}

// WithConfigured binds a config to r for every call (Runnable.with_config).
func WithConfigured[I, O any](r Runnable[I, O], c Config) Runnable[I, O] {
	return Lambda(c.RunName, func(ctx context.Context, in I) (O, error) {
		return r.Invoke(WithConfig(ctx, c), in)
	})
}

// Map applies r to every element of a slice concurrently (Runnable.map()).
func Map[I, O any](r Runnable[I, O]) Runnable[[]I, []O] {
	return Lambda("RunnableEach", func(ctx context.Context, in []I) ([]O, error) {
		return Batch(ctx, r, in)
	})
}
