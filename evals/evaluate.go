package evals

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Target runs the system under test on one example's inputs.
type Target func(ctx context.Context, inputs map[string]any) (map[string]any, error)

// ExampleResult is the outcome for one example.
type ExampleResult struct {
	ExampleID string         `json:"example_id"`
	Outputs   map[string]any `json:"outputs,omitempty"`
	Error     string         `json:"error,omitempty"`
	Latency   time.Duration  `json:"latency_ns"`
	Results   []Result       `json:"results"`
	// EvalErrors are evaluator failures (not scores).
	EvalErrors []string `json:"eval_errors,omitempty"`
}

// Report is the outcome of Evaluate. Summary holds the mean score per key,
// over the examples that produced that key.
type Report struct {
	Results []ExampleResult    `json:"results"`
	Summary map[string]float64 `json:"summary"`
}

type config struct {
	concurrency int
	timeout     time.Duration
}

// Option configures Evaluate.
type Option func(*config)

// WithConcurrency runs up to n examples at once (default 1). Report order
// always follows the dataset.
func WithConcurrency(n int) Option {
	return func(c *config) { c.concurrency = max(n, 1) }
}

// WithTimeout bounds each example (target plus evaluators). A target that
// ignores its context is abandoned when the timeout fires.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// Evaluate runs target on every example, scores each run with evaluators and
// returns the report. Target and evaluator errors are recorded per example
// and do not stop the run. The error return is only for ctx cancellation or
// invalid arguments.
func Evaluate(ctx context.Context, target Target, dataset []Example, evaluators []Evaluator, opts ...Option) (Report, error) {
	if target == nil {
		return Report{}, errors.New("evals: nil target")
	}
	cfg := config{concurrency: 1}
	for _, o := range opts {
		o(&cfg)
	}
	results := make([]ExampleResult, len(dataset))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range cfg.concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = evaluateOne(ctx, cfg, target, dataset[i], i, evaluators)
			}
		}()
	}
	for i := range dataset {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return Report{Results: results, Summary: summarize(results)}, nil
}

func evaluateOne(ctx context.Context, cfg config, target Target, ex Example, i int, evaluators []Evaluator) ExampleResult {
	if ex.ID == "" {
		ex.ID = fmt.Sprintf("example-%d", i+1)
	}
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}
	res := ExampleResult{ExampleID: ex.ID}
	start := time.Now()
	run := Run{}
	run.Outputs, run.Err = runTarget(ctx, target, ex.Inputs)
	run.Latency = time.Since(start)
	res.Outputs, res.Latency = run.Outputs, run.Latency
	if run.Err != nil {
		res.Error = run.Err.Error()
	}
	for _, e := range evaluators {
		r, err := e.Evaluate(ctx, run, ex)
		if err != nil {
			res.EvalErrors = append(res.EvalErrors, err.Error())
			continue
		}
		res.Results = append(res.Results, r)
	}
	return res
}

// runTarget calls target in a goroutine so a timeout returns even when the
// target ignores ctx.
func runTarget(ctx context.Context, target Target, inputs map[string]any) (map[string]any, error) {
	type out struct {
		m   map[string]any
		err error
	}
	ch := make(chan out, 1)
	go func() {
		m, err := target(ctx, inputs)
		ch <- out{m, err}
	}()
	select {
	case o := <-ch:
		return o.m, o.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func summarize(results []ExampleResult) map[string]float64 {
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, r := range results {
		for _, s := range r.Results {
			sums[s.Key] += s.Score
			counts[s.Key]++
		}
	}
	for k := range sums {
		sums[k] /= float64(counts[k])
	}
	return sums
}
