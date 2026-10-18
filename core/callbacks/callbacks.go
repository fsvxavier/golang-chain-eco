// Package callbacks implements the event hooks used for logging, tracing and
// token streaming (langchain_core.callbacks / tracers). Handlers travel in the
// context, so every component below a call site reports to them.
package callbacks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"
)

// Kind is the type of a callback event.
type Kind string

const (
	ChainStart     Kind = "chain_start"
	ChainEnd       Kind = "chain_end"
	ChainError     Kind = "chain_error"
	ModelStart     Kind = "chat_model_start"
	ModelNewToken  Kind = "llm_new_token"
	ModelEnd       Kind = "llm_end"
	ModelError     Kind = "llm_error"
	ToolStart      Kind = "tool_start"
	ToolEnd        Kind = "tool_end"
	ToolError      Kind = "tool_error"
	RetrieverStart Kind = "retriever_start"
	RetrieverEnd   Kind = "retriever_end"
	RetrieverError Kind = "retriever_error"
	Custom         Kind = "custom_event"
)

// Event describes something that happened during a run.
type Event struct {
	Kind        Kind
	RunID       string
	ParentRunID string
	Name        string
	Tags        []string
	Metadata    map[string]any
	Input       any
	Output      any
	Chunk       any // streaming chunk for ModelNewToken (a messages.Message)
	Err         error
	Time        time.Time
}

// Handler receives events. Implementations must be safe for concurrent use.
type Handler interface {
	Handle(ctx context.Context, e Event)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, e Event)

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, e Event) { f(ctx, e) }

type ctxKey int

const (
	handlersKey ctxKey = iota
	runKey
)

// WithHandlers returns a context that reports to hs in addition to any
// handlers already present.
func WithHandlers(ctx context.Context, hs ...Handler) context.Context {
	if len(hs) == 0 {
		return ctx
	}
	cur := Handlers(ctx)
	all := make([]Handler, 0, len(cur)+len(hs))
	all = append(append(all, cur...), hs...)
	return context.WithValue(ctx, handlersKey, all)
}

// Handlers returns the handlers attached to ctx.
func Handlers(ctx context.Context) []Handler {
	hs, _ := ctx.Value(handlersKey).([]Handler)
	return hs
}

// NewRunID returns a random run identifier.
func NewRunID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CurrentRun returns the run ID of the innermost Start call, if any.
func CurrentRun(ctx context.Context) string {
	id, _ := ctx.Value(runKey).(string)
	return id
}

// Start emits a start event and returns a child context plus a function to
// emit the matching end/error event. It is cheap when no handler is set.
func Start(ctx context.Context, kind Kind, name string, input any) (context.Context, func(output any, err error)) {
	hs := Handlers(ctx)
	if len(hs) == 0 {
		return ctx, func(any, error) {}
	}
	runID := NewRunID()
	parent := CurrentRun(ctx)
	emit(ctx, hs, Event{Kind: kind, RunID: runID, ParentRunID: parent, Name: name, Input: input})
	child := context.WithValue(ctx, runKey, runID)
	endKind, errKind := endKinds(kind)
	return child, func(output any, err error) {
		e := Event{Kind: endKind, RunID: runID, ParentRunID: parent, Name: name, Output: output}
		if err != nil {
			e.Kind, e.Err = errKind, err
		}
		emit(ctx, hs, e)
	}
}

func endKinds(k Kind) (Kind, Kind) {
	switch k {
	case ModelStart:
		return ModelEnd, ModelError
	case ToolStart:
		return ToolEnd, ToolError
	case RetrieverStart:
		return RetrieverEnd, RetrieverError
	}
	return ChainEnd, ChainError
}

// Emit sends an arbitrary event (e.g. ModelNewToken or Custom).
func Emit(ctx context.Context, e Event) {
	hs := Handlers(ctx)
	if len(hs) == 0 {
		return
	}
	if e.RunID == "" {
		e.RunID = CurrentRun(ctx)
	}
	emit(ctx, hs, e)
}

func emit(ctx context.Context, hs []Handler, e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	for _, h := range hs {
		h.Handle(ctx, e)
	}
}

// Logger returns a handler that logs every event with slog.
func Logger(l *slog.Logger) Handler {
	if l == nil {
		l = slog.Default()
	}
	return HandlerFunc(func(ctx context.Context, e Event) {
		if e.Kind == ModelNewToken {
			return
		}
		attrs := []any{"run", e.RunID, "name", e.Name}
		if e.Err != nil {
			attrs = append(attrs, "err", e.Err)
		}
		l.InfoContext(ctx, string(e.Kind), attrs...)
	})
}

// StdOut prints tokens and run boundaries to w (StdOutCallbackHandler).
func StdOut(w io.Writer) Handler {
	var mu sync.Mutex
	return HandlerFunc(func(_ context.Context, e Event) {
		mu.Lock()
		defer mu.Unlock()
		switch e.Kind {
		case ModelNewToken:
			if c, ok := e.Chunk.(interface{ Text() string }); ok {
				fmt.Fprint(w, c.Text())
			}
		case ChainStart, ToolStart:
			fmt.Fprintf(w, "\n> Entering %s %q\n", e.Kind, e.Name)
		case ChainEnd, ToolEnd:
			fmt.Fprintf(w, "> Finished %q\n", e.Name)
		case ChainError, ToolError, ModelError:
			fmt.Fprintf(w, "> Error in %q: %v\n", e.Name, e.Err)
		}
	})
}

// Run is a node of a recorded trace.
type Run struct {
	ID       string    `json:"id"`
	Parent   string    `json:"parent_run_id,omitempty"`
	Name     string    `json:"name"`
	Type     string    `json:"run_type"`
	Input    any       `json:"inputs,omitempty"`
	Output   any       `json:"outputs,omitempty"`
	Error    string    `json:"error,omitempty"`
	Start    time.Time `json:"start_time"`
	End      time.Time `json:"end_time"`
	Children []*Run    `json:"child_runs,omitempty"`
}

// Tracer records a run tree in memory (a minimal LangSmith-like tracer).
type Tracer struct {
	mu    sync.Mutex
	runs  map[string]*Run
	roots []*Run
}

// NewTracer creates an empty tracer.
func NewTracer() *Tracer { return &Tracer{runs: map[string]*Run{}} }

// Handle implements Handler.
func (t *Tracer) Handle(_ context.Context, e Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch e.Kind {
	case ChainStart, ModelStart, ToolStart, RetrieverStart:
		r := &Run{ID: e.RunID, Parent: e.ParentRunID, Name: e.Name, Type: string(e.Kind[:len(e.Kind)-len("_start")]), Input: e.Input, Start: e.Time}
		t.runs[r.ID] = r
		if p, ok := t.runs[e.ParentRunID]; ok {
			p.Children = append(p.Children, r)
		} else {
			t.roots = append(t.roots, r)
		}
	case ChainEnd, ModelEnd, ToolEnd, RetrieverEnd, ChainError, ModelError, ToolError, RetrieverError:
		if r, ok := t.runs[e.RunID]; ok {
			r.Output, r.End = e.Output, e.Time
			if e.Err != nil {
				r.Error = e.Err.Error()
			}
		}
	}
}

// Roots returns the top-level runs recorded so far.
func (t *Tracer) Roots() []*Run {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*Run(nil), t.roots...)
}

// JSON renders the recorded trace.
func (t *Tracer) JSON() string {
	b, _ := json.MarshalIndent(t.Roots(), "", "  ")
	return string(b)
}
