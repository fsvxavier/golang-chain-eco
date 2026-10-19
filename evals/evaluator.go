package evals

import (
	"context"
	"time"
)

// Run is what the target produced for one example.
type Run struct {
	Outputs map[string]any
	// Err is the target's error (nil on success). Evaluators still run and
	// see it, so they can score failures.
	Err     error
	Latency time.Duration
}

// Result is one score. Score is 0..1 (1 = pass or perfect).
type Result struct {
	Key     string  `json:"key"`
	Score   float64 `json:"score"`
	Comment string  `json:"comment,omitempty"`
}

// Evaluator scores a run against its example.
type Evaluator interface {
	Evaluate(ctx context.Context, run Run, ex Example) (Result, error)
}

// EvaluatorFunc adapts a function to Evaluator.
type EvaluatorFunc func(ctx context.Context, run Run, ex Example) (Result, error)

// Evaluate implements Evaluator.
func (f EvaluatorFunc) Evaluate(ctx context.Context, run Run, ex Example) (Result, error) {
	return f(ctx, run, ex)
}
