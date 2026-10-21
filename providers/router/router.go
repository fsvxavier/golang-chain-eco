// Package router is a chat model that fans requests out over several
// providers, failing over to the next target on retryable errors.
package router

import (
	"context"
	"fmt"
	"iter"
	"strings"
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
}

// New validates the targets and config and returns a Router.
func New(targets []Target, cfg Config) (*Router, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("router: no targets")
	}
	names := make(map[string]bool, len(targets))
	for _, t := range targets {
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

// order returns the target indexes to try, in attempt order. Only list
// order for now; the other modes come with task 3.
func (r *Router) order() []int {
	idx := make([]int, len(r.targets))
	for i := range idx {
		idx[i] = i
	}
	return idx
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
