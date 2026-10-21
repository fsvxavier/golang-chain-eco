// Package router is a chat model that fans requests out over several
// providers, failing over to the next target on retryable errors.
package router

import (
	"context"
	"fmt"
	"iter"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// Target is one provider the router can call.
type Target struct {
	Name  string
	Model models.ChatModel
}

// Mode picks the first target of each call; the rest follow in list order.
type Mode string

const (
	ModeFailover   Mode = "failover"
	ModeRoundRobin Mode = "round_robin"
	ModeRandom     Mode = "random"
	ModeWeighted   Mode = "weighted"
)

// Config tunes a Router. The zero value is failover with no timeout.
type Config struct {
	Model   string             // label for ModelID: "router:"+Model
	Mode    Mode               // "" = failover
	Weights map[string]float64 // weighted only; missing name = 1; 0 = never first
	Timeout time.Duration      // per attempt; 0 = none
	Rand    func() float64     // [0,1); nil = math/rand/v2 Float64
}

// Router is a models.ChatModel that tries its targets in turn.
type Router struct {
	targets []Target
	cfg     Config
	next    atomic.Uint64 // round_robin counter, per instance
}

// New validates the targets and config and returns a Router.
func New(targets []Target, cfg Config) (*Router, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("router: no targets")
	}
	names := make(map[string]bool, len(targets))
	for _, t := range targets {
		if t.Name == "" {
			return nil, fmt.Errorf("router: target has an empty name")
		}
		if t.Model == nil {
			return nil, fmt.Errorf("router: target %q has no model", t.Name)
		}
		if names[t.Name] {
			return nil, fmt.Errorf("router: duplicate target %q", t.Name)
		}
		names[t.Name] = true
	}
	switch cfg.Mode {
	case "", ModeFailover, ModeRoundRobin, ModeRandom, ModeWeighted:
	default:
		return nil, fmt.Errorf("router: unknown mode %q", cfg.Mode)
	}
	var sum float64
	for name, w := range cfg.Weights {
		if !names[name] {
			return nil, fmt.Errorf("router: weight for unknown target %q", name)
		}
		if w < 0 {
			return nil, fmt.Errorf("router: negative weight for target %q", name)
		}
	}
	for _, t := range targets {
		if w, ok := cfg.Weights[t.Name]; ok {
			sum += w
		} else {
			sum++
		}
	}
	if cfg.Mode == ModeWeighted && sum == 0 {
		return nil, fmt.Errorf("router: weighted mode needs a positive weight")
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Float64
	}
	return &Router{targets: append([]Target(nil), targets...), cfg: cfg}, nil
}

// ModelID implements models.ChatModel.
func (r *Router) ModelID() string { return "router:" + r.cfg.Model }

// Generate tries the targets in order and returns the first answer. Errors
// that retrying elsewhere cannot fix end the call at once.
func (r *Router) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	var failed []error
	for _, i := range r.order() {
		if err := ctx.Err(); err != nil {
			return messages.Message{}, err
		}
		out, err := r.attempt(ctx, r.targets[i].Model, msgs, opts)
		if err == nil {
			return out, nil
		}
		if !retryable(ctx, err) {
			if cerr := ctx.Err(); cerr != nil {
				return messages.Message{}, cerr
			}
			return messages.Message{}, err
		}
		if len(r.targets) == 1 {
			return messages.Message{}, err
		}
		failed = append(failed, fmt.Errorf("%s: %w", r.targets[i].Name, err))
	}
	return messages.Message{}, &allFailedError{failed}
}

// Stream implements models.ChatModel.
// TODO(task 4): fail over mid-stream; for now it streams from the first target only.
func (r *Router) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	return r.targets[r.order()[0]].Model.Stream(ctx, msgs, opts...)
}

// order returns the target indexes to try, in attempt order. The mode picks
// the first target; the rest follow in list order.
func (r *Router) order() []int {
	first := 0
	switch r.cfg.Mode {
	case ModeRoundRobin:
		first = int((r.next.Add(1) - 1) % uint64(len(r.targets)))
	case ModeRandom:
		first = min(int(r.cfg.Rand()*float64(len(r.targets))), len(r.targets)-1)
	case ModeWeighted:
		first = r.weightedPick()
	}
	idx := make([]int, 0, len(r.targets))
	idx = append(idx, first)
	for i := range r.targets {
		if i != first {
			idx = append(idx, i)
		}
	}
	return idx
}

// weightedPick chooses a target with probability proportional to its weight
// (missing = 1). Weight 0 is never picked here, only as a fallback.
func (r *Router) weightedPick() int {
	weights := make([]float64, len(r.targets))
	var total float64
	last := 0
	for i, t := range r.targets {
		w, ok := r.cfg.Weights[t.Name]
		if !ok {
			w = 1
		}
		weights[i] = w
		total += w
		if w > 0 {
			last = i
		}
	}
	x := r.cfg.Rand() * total
	var cum float64
	for i, w := range weights {
		cum += w
		if w > 0 && x < cum {
			return i
		}
	}
	return last // only reached by float rounding at the top edge
}

// attempt calls one target, bounded by Config.Timeout when set.
func (r *Router) attempt(ctx context.Context, m models.ChatModel, msgs []messages.Message, opts []models.Option) (messages.Message, error) {
	if r.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.cfg.Timeout)
		defer cancel()
	}
	return m.Generate(ctx, msgs, opts...)
}

// allFailedError reports that every target failed; each entry names its target.
type allFailedError struct{ errs []error }

func (e *allFailedError) Error() string {
	parts := make([]string, len(e.errs))
	for i, err := range e.errs {
		parts[i] = err.Error()
	}
	return "router: all targets failed: " + strings.Join(parts, "; ")
}

func (e *allFailedError) Unwrap() []error { return e.errs }
