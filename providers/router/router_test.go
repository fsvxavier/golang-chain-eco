package router

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"sync"
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

	// Stream script: text chunks, then an optional tool-call chunk. The
	// stream fails with streamErr before item streamErrAfter (-1 = never).
	chunks         []string
	toolCall       bool
	streamErrAfter int
	streamErr      error
	stall          bool          // at streamErrAfter, wait for ctx instead of failing
	errWithChunk   bool          // at streamErrAfter, yield the next item together with streamErr
	toolCallFull   bool          // a tool call in ToolCalls rather than ToolCallChunks
	delay          time.Duration // wait before each item, cut short by ctx

	calls atomic.Int32
	mu    sync.Mutex // guards msgs and opts when called concurrently
	msgs  []messages.Message
	opts  models.Options
}

func (s *stub) ModelID() string { return "stub:" + s.name }

func (s *stub) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	n := int(s.calls.Add(1)) - 1
	s.mu.Lock()
	s.msgs = msgs
	s.opts = models.Apply(opts...)
	s.mu.Unlock()
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
		s.calls.Add(1)
		s.mu.Lock()
		s.msgs = msgs
		s.opts = models.Apply(opts...)
		s.mu.Unlock()
		if s.block {
			<-ctx.Done()
			yield(messages.Message{}, ctx.Err())
			return
		}
		items := make([]messages.Message, 0, len(s.chunks)+1)
		for _, c := range s.chunks {
			items = append(items, messages.AI(c))
		}
		if s.toolCall {
			items = append(items, messages.Message{Role: messages.RoleAI, ToolCallChunks: []messages.ToolCallChunk{{ID: "call_1", Name: "search", Args: `{"q":"go"}`}}})
		}
		if s.toolCallFull {
			items = append(items, messages.Message{Role: messages.RoleAI, ToolCalls: []messages.ToolCall{{ID: "call_2", Name: "search", Args: map[string]any{"q": "go"}}}})
		}
		for i := 0; i <= len(items); i++ {
			if i == s.streamErrAfter {
				switch {
				case s.stall:
					<-ctx.Done()
					yield(messages.Message{}, ctx.Err())
				case s.errWithChunk && i < len(items):
					yield(items[i], s.streamErr)
				default:
					yield(messages.Message{}, s.streamErr)
				}
				return
			}
			if i == len(items) {
				return
			}
			if s.delay > 0 {
				select {
				case <-time.After(s.delay):
				case <-ctx.Done():
					yield(messages.Message{}, ctx.Err())
					return
				}
			}
			if !yield(items[i], nil) {
				return
			}
		}
	}
}

// drain collects the content of every chunk until the stream ends or errors.
func drain(seq iter.Seq2[messages.Message, error]) ([]string, error) {
	var texts []string
	for m, err := range seq {
		if err != nil {
			return texts, err
		}
		texts = append(texts, m.Content)
	}
	return texts, nil
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
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{Timeout: 50 * time.Millisecond})
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

// always returns err on every call.
func always(err error) []error {
	errs := make([]error, 1000)
	for i := range errs {
		errs[i] = err
	}
	return errs
}

// healthy answers every call with its own name.
func healthy(name string) *stub { return &stub{name: name, reply: name} }

func threeTargets(a, b, c *stub) []Target {
	return []Target{{Name: "a", Model: a}, {Name: "b", Model: b}, {Name: "c", Model: c}}
}

func TestRoundRobinEven(t *testing.T) {
	a, b, c := healthy("a"), healthy("b"), healthy("c")
	r, err := New(threeTargets(a, b, c), Config{Mode: ModeRoundRobin})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err := r.Generate(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []*stub{a, b, c} {
		if got := s.calls.Load(); got != 10 {
			t.Errorf("%s called %d times, want 10", s.name, got)
		}
	}
}

func TestRoundRobinFailsOverFromChosen(t *testing.T) {
	a := healthy("a")
	b := &stub{name: "b", errs: always(status(500))}
	c := healthy("c")
	r, _ := New(threeTargets(a, b, c), Config{Mode: ModeRoundRobin})
	// Calls 0..8 pick a, b, c in turn; the b picks (1, 4, 7) fail over to a,
	// the next in list order after b.
	for i := 0; i < 9; i++ {
		out, err := r.Generate(context.Background(), nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if out.Text() == "b" {
			t.Fatalf("call %d answered from failing target b", i)
		}
	}
	if got := b.calls.Load(); got != 3 {
		t.Errorf("b called %d times, want 3", got)
	}
	if got := a.calls.Load(); got != 6 {
		t.Errorf("a called %d times, want 6 (3 own picks + 3 fallbacks)", got)
	}
}

func TestRoundRobinConcurrent(t *testing.T) {
	a, b, c := healthy("a"), healthy("b"), healthy("c")
	r, _ := New(threeTargets(a, b, c), Config{Mode: ModeRoundRobin})
	var wg sync.WaitGroup
	for i := 0; i < 90; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Generate(context.Background(), nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, s := range []*stub{a, b, c} {
		if s.calls.Load() < 1 {
			t.Errorf("%s never called", s.name)
		}
	}
}

func TestWeightedFirstPick(t *testing.T) {
	// a:3, b:1 (total 4): [0, 0.75) picks a, [0.75, 1) picks b.
	cases := []struct {
		rand  float64
		first string
	}{
		{0.0, "a"}, {0.74, "a"}, {0.76, "b"}, {0.99, "b"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("rand=%v", tc.rand), func(t *testing.T) {
			a, b := healthy("a"), healthy("b")
			r, err := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{
				Mode:    ModeWeighted,
				Weights: map[string]float64{"a": 3, "b": 1},
				Rand:    func() float64 { return tc.rand },
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Generate(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if out.Text() != tc.first {
				t.Errorf("first = %q, want %q", out.Text(), tc.first)
			}
		})
	}
}

func TestWeightedZeroNeverFirst(t *testing.T) {
	cfg := func(rand float64) Config {
		return Config{
			Mode:    ModeWeighted,
			Weights: map[string]float64{"a": 0},
			Rand:    func() float64 { return rand },
		}
	}
	for _, v := range []float64{0.0, 0.3, 0.999} {
		a, b := healthy("a"), healthy("b")
		r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, cfg(v))
		if _, err := r.Generate(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		if a.calls.Load() != 0 {
			t.Errorf("rand=%v: weight-0 target a was first", v)
		}
	}

	// With b failing, the fallback reaches a.
	a := healthy("a")
	b := &stub{name: "b", errs: always(status(500))}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, cfg(0.0))
	out, err := r.Generate(context.Background(), nil)
	if err != nil || out.Text() != "a" {
		t.Fatalf("got (%q, %v), want (a, nil)", out.Text(), err)
	}
}

func TestRandomUsesRand(t *testing.T) {
	cases := []struct {
		rand  float64
		first string
	}{
		{0.0, "a"}, {0.5, "b"}, {0.99, "c"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("rand=%v", tc.rand), func(t *testing.T) {
			a, b, c := healthy("a"), healthy("b"), healthy("c")
			r, err := New(threeTargets(a, b, c), Config{
				Mode: ModeRandom,
				Rand: func() float64 { return tc.rand },
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Generate(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if out.Text() != tc.first {
				t.Errorf("first = %q, want %q", out.Text(), tc.first)
			}
		})
	}
}

func TestNewRejectsBadTarget(t *testing.T) {
	cases := []struct {
		name    string
		targets []Target
		want    string
	}{
		{"nil model", []Target{{Name: "a", Model: &stub{name: "a"}}, {Name: "b"}}, "target \"b\" has no model"},
		{"empty name", []Target{{Name: "", Model: &stub{name: "x"}}}, "target has an empty name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.targets, Config{})
			if err == nil {
				t.Fatal("New succeeded, want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestStreamFailsOverBeforeFirstChunk(t *testing.T) {
	a := &stub{name: "a", streamErrAfter: 0, streamErr: status(503)}
	b := &stub{name: "b", chunks: []string{"x", "y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	texts, err := drain(r.Stream(context.Background(), []messages.Message{messages.Human("hi")}))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	if !reflect.DeepEqual(texts, []string{"x", "y"}) {
		t.Errorf("chunks = %q, want [x y]", texts)
	}
}

func TestStreamContinuesAfterText(t *testing.T) {
	history := []messages.Message{messages.Human("hi")}
	opts := []models.Option{models.WithTemperature(0.2), models.WithMaxTokens(64)}
	a := &stub{name: "a", chunks: []string{"x"}, streamErrAfter: 1, streamErr: status(503)}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	texts, err := drain(r.Stream(context.Background(), history, opts...))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	if !reflect.DeepEqual(texts, []string{"x", "y"}) {
		t.Errorf("chunks = %q, want [x y]", texts)
	}
	want := append(append([]messages.Message{}, history...), messages.AI("x"), messages.Human(continuePrompt))
	if !reflect.DeepEqual(b.msgs, want) {
		t.Errorf("b msgs = %v, want %v", b.msgs, want)
	}
	if !reflect.DeepEqual(a.opts, b.opts) {
		t.Errorf("options differ: a=%+v b=%+v", a.opts, b.opts)
	}
}

func TestStreamPartialToolCallNotContinued(t *testing.T) {
	a := &stub{name: "a", toolCall: true, streamErrAfter: 1, streamErr: status(503)}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	_, err := drain(r.Stream(context.Background(), nil))
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 503 {
		t.Fatalf("err = %v, want StatusError 503", err)
	}
	if b.calls.Load() != 0 {
		t.Errorf("b called %d times, want 0", b.calls.Load())
	}
}

func TestStreamTwoSwitches(t *testing.T) {
	a := &stub{name: "a", chunks: []string{"x"}, streamErrAfter: 1, streamErr: status(503)}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: 1, streamErr: status(503)}
	c := &stub{name: "c", chunks: []string{"z"}, streamErrAfter: -1}
	r, _ := New(threeTargets(a, b, c), Config{})
	history := []messages.Message{messages.Human("hi")}
	opts := []models.Option{models.WithTemperature(0.2), models.WithMaxTokens(64)}
	texts, err := drain(r.Stream(context.Background(), history, opts...))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	if !reflect.DeepEqual(texts, []string{"x", "y", "z"}) {
		t.Errorf("chunks = %q, want [x y z]", texts)
	}
	wantB := append(append([]messages.Message{}, history...), messages.AI("x"), messages.Human(continuePrompt))
	wantC := append(append([]messages.Message{}, history...), messages.AI("xy"), messages.Human(continuePrompt))
	if !reflect.DeepEqual(b.msgs, wantB) {
		t.Errorf("b msgs = %v, want %v", b.msgs, wantB)
	}
	if !reflect.DeepEqual(c.msgs, wantC) {
		t.Errorf("c msgs = %v, want %v", c.msgs, wantC)
	}
	if !reflect.DeepEqual(a.opts, b.opts) || !reflect.DeepEqual(b.opts, c.opts) {
		t.Errorf("options differ: a=%+v b=%+v c=%+v", a.opts, b.opts, c.opts)
	}
}

func TestStreamAuthErrorAfterTextFinal(t *testing.T) {
	a := &stub{name: "a", chunks: []string{"x"}, streamErrAfter: 1, streamErr: status(401)}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	texts, err := drain(r.Stream(context.Background(), nil))
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("err = %v, want StatusError 401", err)
	}
	if !reflect.DeepEqual(texts, []string{"x"}) {
		t.Errorf("chunks = %q, want [x]", texts)
	}
	if b.calls.Load() != 0 {
		t.Errorf("b called %d times, want 0", b.calls.Load())
	}
}

func TestStreamAuthErrorFinal(t *testing.T) {
	a := &stub{name: "a", streamErrAfter: 0, streamErr: status(401)}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	_, err := drain(r.Stream(context.Background(), nil))
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("err = %v, want StatusError 401", err)
	}
	if b.calls.Load() != 0 {
		t.Errorf("b called %d times, want 0", b.calls.Load())
	}
}

func TestStreamEarlyBreak(t *testing.T) {
	a := &stub{name: "a", chunks: []string{"x", "y"}, streamErrAfter: 2, streamErr: status(503)}
	b := &stub{name: "b", chunks: []string{"z"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	n := 0
	for range r.Stream(context.Background(), nil) {
		n++
		break
	}
	if n != 1 {
		t.Errorf("consumer saw %d chunks, want 1", n)
	}
	if b.calls.Load() != 0 {
		t.Errorf("b called %d times, want 0", b.calls.Load())
	}
}

// Stream timeout is idle time: it bounds time to first chunk and each gap
// between chunks, not the whole stream nor the consumer's own pauses.

func TestStreamSteadyLongerThanTimeoutCompletes(t *testing.T) {
	a := &stub{name: "a", chunks: []string{"a", "b", "c", "d", "e", "f", "g", "h"}, streamErrAfter: -1, delay: 5 * time.Millisecond}
	b := healthy("b")
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{Timeout: 50 * time.Millisecond})
	texts, err := drain(r.Stream(context.Background(), nil))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	if len(texts) != 8 || b.calls.Load() != 0 {
		t.Errorf("chunks = %d, b calls = %d; want 8 chunks and no failover", len(texts), b.calls.Load())
	}
}

func TestStreamStallAfterFirstChunkFailsOver(t *testing.T) {
	history := []messages.Message{messages.Human("hi")}
	a := &stub{name: "a", chunks: []string{"x"}, streamErrAfter: 1, stall: true}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{Timeout: 50 * time.Millisecond})
	texts, err := drain(r.Stream(context.Background(), history))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	if !reflect.DeepEqual(texts, []string{"x", "y"}) {
		t.Errorf("chunks = %q, want [x y]", texts)
	}
	want := append(append([]messages.Message{}, history...), messages.AI("x"), messages.Human(continuePrompt))
	if !reflect.DeepEqual(b.msgs, want) {
		t.Errorf("b msgs = %v, want %v", b.msgs, want)
	}
}

func TestStreamConsumerPauseNotCounted(t *testing.T) {
	a := &stub{name: "a", chunks: []string{"x", "y"}, streamErrAfter: -1}
	b := healthy("b")
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{Timeout: 50 * time.Millisecond})
	var texts []string
	for m, err := range r.Stream(context.Background(), nil) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		texts = append(texts, m.Content)
		if len(texts) == 1 {
			time.Sleep(100 * time.Millisecond) // longer than Timeout, while the consumer holds the chunk
		}
	}
	if !reflect.DeepEqual(texts, []string{"x", "y"}) || b.calls.Load() != 0 {
		t.Errorf("chunks = %q, b calls = %d; want [x y] and no failover", texts, b.calls.Load())
	}
}

func TestStreamChunkWithErrorForwardedFirst(t *testing.T) {
	history := []messages.Message{messages.Human("hi")}
	a := &stub{name: "a", chunks: []string{"x", "y"}, streamErrAfter: 1, streamErr: status(503), errWithChunk: true}
	b := &stub{name: "b", chunks: []string{"z"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	texts, err := drain(r.Stream(context.Background(), history))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	if !reflect.DeepEqual(texts, []string{"x", "y", "z"}) {
		t.Errorf("chunks = %q, want [x y z]", texts)
	}
	want := append(append([]messages.Message{}, history...), messages.AI("xy"), messages.Human(continuePrompt))
	if !reflect.DeepEqual(b.msgs, want) {
		t.Errorf("b msgs = %v, want %v", b.msgs, want)
	}
}

func TestStreamNoChunksNoErrorCompletes(t *testing.T) {
	a := &stub{name: "a", streamErrAfter: -1}
	b := healthy("b")
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	texts, err := drain(r.Stream(context.Background(), nil))
	if err != nil || len(texts) != 0 || b.calls.Load() != 0 {
		t.Errorf("got (%q, %v), b calls %d; want no chunks, no error, no failover", texts, err, b.calls.Load())
	}
}

func TestStreamAllTargetsFail(t *testing.T) {
	a := &stub{name: "a", streamErrAfter: 0, streamErr: status(500)}
	b := &stub{name: "b", streamErrAfter: 0, streamErr: status(503)}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	_, err := drain(r.Stream(context.Background(), nil))
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

func TestStreamSingleTargetErrorUnwrapped(t *testing.T) {
	a := &stub{name: "a", streamErrAfter: 0, streamErr: status(500)}
	r, _ := New([]Target{{Name: "a", Model: a}}, Config{})
	_, err := drain(r.Stream(context.Background(), nil))
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 500 {
		t.Fatalf("err = %v, want StatusError 500", err)
	}
	if strings.Contains(err.Error(), "all targets") {
		t.Errorf("single target error wrapped: %q", err)
	}
}

func TestStreamCancelledBeforeStart(t *testing.T) {
	a := &stub{name: "a", chunks: []string{"x"}, streamErrAfter: -1}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := drain(r.Stream(ctx, nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if a.calls.Load() != 0 || b.calls.Load() != 0 {
		t.Errorf("calls a=%d b=%d, want 0 and 0", a.calls.Load(), b.calls.Load())
	}
}

func TestStreamToolCallFullBlocksContinuation(t *testing.T) {
	a := &stub{name: "a", toolCallFull: true, streamErrAfter: 1, streamErr: status(503)}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{})
	_, err := drain(r.Stream(context.Background(), nil))
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 503 {
		t.Fatalf("err = %v, want StatusError 503", err)
	}
	if b.calls.Load() != 0 {
		t.Errorf("b called %d times, want 0", b.calls.Load())
	}
}

func TestNonRetryableOnSecondTargetReturnedRaw(t *testing.T) {
	a := &stub{name: "a", errs: []error{status(500)}}
	b := &stub{name: "b", errs: []error{status(401)}}
	c := healthy("c")
	r, _ := New(threeTargets(a, b, c), Config{})
	_, err := r.Generate(context.Background(), nil)
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("err = %v, want StatusError 401", err)
	}
	if strings.Contains(err.Error(), "all targets") {
		t.Errorf("error wrapped: %q", err)
	}
	if c.calls.Load() != 0 {
		t.Errorf("c called %d times, want 0", c.calls.Load())
	}
}

func TestWeightedCumulativeEdge(t *testing.T) {
	// a:3, b:1, total 4: 0.75*4 == 3 is exactly the edge, which belongs to b.
	for _, tc := range []struct {
		rand  float64
		first string
	}{{0.75, "b"}, {0.7499, "a"}} {
		a, b := healthy("a"), healthy("b")
		r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{
			Mode: ModeWeighted, Weights: map[string]float64{"a": 3, "b": 1},
			Rand: func() float64 { return tc.rand },
		})
		out, err := r.Generate(context.Background(), nil)
		if err != nil || out.Text() != tc.first {
			t.Errorf("rand=%v: got (%q, %v), want %q", tc.rand, out.Text(), err, tc.first)
		}
	}
}

func TestWeightedLastFallbackSkipsZeroWeight(t *testing.T) {
	// 1.0 is outside Rand's contract but reaches the rounding fallback.
	for _, v := range []float64{0.9999999, 1.0} {
		a, b := healthy("a"), healthy("b")
		r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{
			Mode: ModeWeighted, Weights: map[string]float64{"b": 0},
			Rand: func() float64 { return v },
		})
		out, err := r.Generate(context.Background(), nil)
		if err != nil || out.Text() != "a" || b.calls.Load() != 0 {
			t.Errorf("rand=%v: got (%q, %v), b calls %d; want a first", v, out.Text(), err, b.calls.Load())
		}
	}
}

func TestRandomOneClampsToLast(t *testing.T) {
	a, b, c := healthy("a"), healthy("b"), healthy("c")
	r, _ := New(threeTargets(a, b, c), Config{Mode: ModeRandom, Rand: func() float64 { return 1.0 }})
	out, err := r.Generate(context.Background(), nil)
	if err != nil || out.Text() != "c" {
		t.Fatalf("got (%q, %v), want c", out.Text(), err)
	}
}

func TestSingleTargetEveryMode(t *testing.T) {
	for _, mode := range []Mode{ModeFailover, ModeRoundRobin, ModeRandom, ModeWeighted} {
		t.Run(string(mode), func(t *testing.T) {
			a := healthy("a")
			r, err := New([]Target{{Name: "a", Model: a}}, Config{Mode: mode, Rand: func() float64 { return 0.99 }})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Generate(context.Background(), nil)
			if err != nil || out.Text() != "a" || a.calls.Load() != 1 {
				t.Fatalf("got (%q, %v), calls %d; want a, 1 call", out.Text(), err, a.calls.Load())
			}
		})
	}
}

func TestStreamStallBeforeFirstChunkFailsOver(t *testing.T) {
	history := []messages.Message{messages.Human("hi")}
	a := &stub{name: "a", chunks: []string{"x"}, streamErrAfter: 0, stall: true}
	b := &stub{name: "b", chunks: []string{"y"}, streamErrAfter: -1}
	r, _ := New([]Target{{Name: "a", Model: a}, {Name: "b", Model: b}}, Config{Timeout: 50 * time.Millisecond})
	texts, err := drain(r.Stream(context.Background(), history))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	if !reflect.DeepEqual(texts, []string{"y"}) {
		t.Errorf("chunks = %q, want [y]", texts)
	}
	if !reflect.DeepEqual(b.msgs, history) {
		t.Errorf("b msgs = %v, want original %v", b.msgs, history)
	}
}
