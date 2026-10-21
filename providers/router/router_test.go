package router

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

func TestRetryable(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"408", &httpx.StatusError{Code: 408}, true},
		{"425", &httpx.StatusError{Code: 425}, true},
		{"429", &httpx.StatusError{Code: 429}, true},
		{"500", &httpx.StatusError{Code: 500}, true},
		{"502", &httpx.StatusError{Code: 502}, true},
		{"503", &httpx.StatusError{Code: 503}, true},
		{"529", &httpx.StatusError{Code: 529}, true},
		{"400", &httpx.StatusError{Code: 400}, false},
		{"401", &httpx.StatusError{Code: 401}, false},
		{"403", &httpx.StatusError{Code: 403}, false},
		{"404", &httpx.StatusError{Code: 404}, false},
		{"422", &httpx.StatusError{Code: 422}, false},
		{"wrapped 503", fmt.Errorf("x: %w", &httpx.StatusError{Code: 503}), true},
		{"wrapped 400", fmt.Errorf("x: %w", &httpx.StatusError{Code: 400}), false},
		{"network error", errors.New("connection reset"), true},
		{"attempt deadline", context.DeadlineExceeded, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryable(live, tc.err); got != tc.want {
				t.Errorf("retryable(live, %v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}

	// Once the caller's context is done, nothing is retryable.
	for _, tc := range cases {
		t.Run("cancelled/"+tc.name, func(t *testing.T) {
			if got := retryable(cancelled, tc.err); got {
				t.Errorf("retryable(cancelled, %v) = true, want false", tc.err)
			}
		})
	}
}

// stub is a scripted ChatModel: call i returns errs[i] when present, else
// an AI message with reply. It records what it was called with.
type stub struct {
	name  string
	errs  []error
	reply string
	block bool // wait for ctx to end instead of answering

	calls atomic.Int32
	msgs  []messages.Message
	opts  models.Options
}

func (s *stub) ModelID() string { return "stub:" + s.name }

func (s *stub) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	n := int(s.calls.Add(1)) - 1
	s.msgs = msgs
	s.opts = models.Apply(opts...)
	if s.block {
		<-ctx.Done()
		return messages.Message{}, ctx.Err()
	}
	if n < len(s.errs) && s.errs[n] != nil {
		return messages.Message{}, s.errs[n]
	}
	return messages.AI(s.reply), nil
}

func (s *stub) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	return func(yield func(messages.Message, error) bool) {
		out, err := s.Generate(ctx, msgs, opts...)
		yield(out, err)
	}
}

func status(code int) error { return &httpx.StatusError{Code: code, Body: "boom"} }

func TestFailoverSecondAnswers(t *testing.T) {
	a := &stub{name: "a", errs: []error{status(500)}}
	b := &stub{name: "b", reply: "ok"}
	r, err := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Generate(context.Background(), []messages.Message{messages.Human("hi")})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Text() != "ok" {
		t.Errorf("got %q, want %q", out.Text(), "ok")
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Errorf("calls a=%d b=%d, want 1 and 1", a.calls.Load(), b.calls.Load())
	}
}

func TestAuthErrorDoesNotFallThrough(t *testing.T) {
	a := &stub{name: "a", errs: []error{status(401)}}
	b := &stub{name: "b", reply: "ok"}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	_, err := r.Generate(context.Background(), nil)
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("err = %v, want StatusError 401", err)
	}
	if b.calls.Load() != 0 {
		t.Errorf("b called %d times, want 0", b.calls.Load())
	}
}

func TestRateLimitFallsThrough(t *testing.T) {
	a := &stub{name: "a", errs: []error{status(429)}}
	b := &stub{name: "b", reply: "ok"}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	out, err := r.Generate(context.Background(), nil)
	if err != nil || out.Text() != "ok" {
		t.Fatalf("got (%q, %v), want (ok, nil)", out.Text(), err)
	}
}

func TestAllFailError(t *testing.T) {
	a := &stub{name: "a", errs: []error{status(500)}}
	b := &stub{name: "b", errs: []error{status(503)}}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	_, err := r.Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("want error")
	}
	msg := err.Error()
	for _, want := range []string{"router: all targets failed:", "a: http 500", "b: http 503"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	var se *httpx.StatusError
	if !errors.As(err, &se) {
		t.Errorf("errors.As to StatusError failed for %q", msg)
	}
}

func TestSingleTargetErrorUnwrapped(t *testing.T) {
	a := &stub{name: "a", errs: []error{status(500)}}
	r, _ := New([]Target{{Name: "a", Model: a}}, Config{})
	_, err := r.Generate(context.Background(), nil)
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 500 {
		t.Fatalf("err = %v, want StatusError 500", err)
	}
	if strings.Contains(err.Error(), "all targets") {
		t.Errorf("single target error wrapped: %q", err)
	}
}

func TestCancelledContextStops(t *testing.T) {
	a := &stub{name: "a", reply: "ok"}
	b := &stub{name: "b", reply: "ok"}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Generate(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if a.calls.Load() != 0 || b.calls.Load() != 0 {
		t.Errorf("calls a=%d b=%d, want 0 and 0", a.calls.Load(), b.calls.Load())
	}
}

func TestTimeoutFallsThrough(t *testing.T) {
	a := &stub{name: "a", block: true}
	b := &stub{name: "b", reply: "ok"}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{Timeout: 20 * time.Millisecond})
	out, err := r.Generate(context.Background(), nil)
	if err != nil || out.Text() != "ok" {
		t.Fatalf("got (%q, %v), want (ok, nil)", out.Text(), err)
	}
}

func TestContextPreservedOnFailover(t *testing.T) {
	type searchArgs struct {
		Q string `json:"q"`
	}
	search := tools.New("search", "search the web", func(ctx context.Context, args searchArgs) (any, error) {
		return nil, nil
	})
	history := []messages.Message{
		messages.System("be brief"),
		messages.Human("first"),
		messages.AI("second"),
	}
	opts := []models.Option{
		models.WithTools(search),
		models.WithTemperature(0.2),
		models.WithMaxTokens(64),
	}

	a := &stub{name: "a", errs: []error{status(503)}}
	b := &stub{name: "b", reply: "ok"}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	if _, err := r.Generate(context.Background(), history, opts...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.msgs, b.msgs) || len(b.msgs) != 3 {
		t.Errorf("messages differ: a=%v b=%v", a.msgs, b.msgs)
	}
	if !reflect.DeepEqual(a.opts, b.opts) || len(b.opts.Tools) != 1 {
		t.Errorf("options differ: a=%+v b=%+v", a.opts, b.opts)
	}
}

func TestModelID(t *testing.T) {
	r, _ := New([]Target{{Name: "a", Model: &stub{name: "a"}}}, Config{Model: "gpt-x"})
	if got := r.ModelID(); got != "router:gpt-x" {
		t.Errorf("ModelID() = %q, want router:gpt-x", got)
	}
}

func TestNewValidation(t *testing.T) {
	a := Target{Name: "a", Model: &stub{name: "a"}}
	b := Target{Name: "b", Model: &stub{name: "b"}}
	cases := []struct {
		name    string
		targets []Target
		cfg     Config
	}{
		{"no targets", nil, Config{}},
		{"duplicate name", []Target{a, a}, Config{}},
		{"unknown mode", []Target{a, b}, Config{Mode: "fastest"}},
		{"weight for unknown name", []Target{a, b}, Config{Mode: ModeWeighted, Weights: map[string]float64{"c": 1}}},
		{"negative weight", []Target{a, b}, Config{Mode: ModeWeighted, Weights: map[string]float64{"a": -1}}},
		{"weighted all zero", []Target{a, b}, Config{Mode: ModeWeighted, Weights: map[string]float64{"a": 0, "b": 0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.targets, tc.cfg); err == nil {
				t.Error("New succeeded, want error")
			}
		})
	}
}
