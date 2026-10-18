// Package langsmith ships callback events to LangSmith as runs, so any
// chain, model, tool, or graph can be traced:
//
//	tr := langsmith.New(langsmith.Options{Project: "demo"})
//	defer tr.Close()
//	ctx = callbacks.WithHandlers(ctx, tr)
//
// Tracing is best-effort and never fails the traced program: delivery
// errors are reported to Options.OnError.
package langsmith

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
)

// Options configures the tracer.
type Options struct {
	// APIKey defaults to $LANGSMITH_API_KEY / $LANGCHAIN_API_KEY.
	APIKey string
	// Endpoint defaults to $LANGSMITH_ENDPOINT or https://api.smith.langchain.com.
	Endpoint string
	// Project defaults to $LANGSMITH_PROJECT or "default".
	Project string
	HTTP    *http.Client
	OnError func(error)
	// Buffer is the event queue size (default 1024); events are dropped
	// (reported to OnError) when it is full.
	Buffer int
}

// Tracer is a callbacks.Handler that posts runs to LangSmith.
type Tracer struct {
	o     Options
	q     chan job
	wg    sync.WaitGroup
	mu    sync.Mutex
	runs  map[string]*runInfo
	close sync.Once
}

type runInfo struct {
	trace, dotted string
	start         time.Time
}

type job struct {
	method, path string
	body         map[string]any
}

// New starts the background sender.
func New(o Options) *Tracer {
	env := func(keys ...string) string {
		for _, k := range keys {
			if v := os.Getenv(k); v != "" {
				return v
			}
		}
		return ""
	}
	if o.APIKey == "" {
		o.APIKey = env("LANGSMITH_API_KEY", "LANGCHAIN_API_KEY")
	}
	if o.Endpoint == "" {
		o.Endpoint = env("LANGSMITH_ENDPOINT", "LANGCHAIN_ENDPOINT")
	}
	if o.Endpoint == "" {
		o.Endpoint = "https://api.smith.langchain.com"
	}
	o.Endpoint = strings.TrimRight(o.Endpoint, "/")
	if o.Project == "" {
		o.Project = env("LANGSMITH_PROJECT", "LANGCHAIN_PROJECT")
	}
	if o.Project == "" {
		o.Project = "default"
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if o.OnError == nil {
		o.OnError = func(error) {}
	}
	if o.Buffer <= 0 {
		o.Buffer = 1024
	}
	t := &Tracer{o: o, q: make(chan job, o.Buffer), runs: map[string]*runInfo{}}
	t.wg.Add(1)
	go t.loop()
	return t
}

// Close flushes queued events and stops the sender.
func (t *Tracer) Close() {
	t.close.Do(func() { close(t.q) })
	t.wg.Wait()
}

func (t *Tracer) loop() {
	defer t.wg.Done()
	for j := range t.q {
		if err := t.send(j); err != nil {
			t.o.OnError(err)
		}
	}
}

func (t *Tracer) send(j job) error {
	b, err := json.Marshal(j.body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(j.method, t.o.Endpoint+j.path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", t.o.APIKey)
	resp, err := t.o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("langsmith: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (t *Tracer) enqueue(j job) {
	defer func() { _ = recover() }() // queue closed
	select {
	case t.q <- j:
	default:
		t.o.OnError(fmt.Errorf("langsmith: queue full, dropped %s %s", j.method, j.path))
	}
}

// uuid formats a 32-hex run ID as a UUID.
func uuid(id string) string {
	if len(id) != 32 {
		return id
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func runType(k callbacks.Kind) string {
	switch k {
	case callbacks.ModelStart:
		return "llm"
	case callbacks.ToolStart:
		return "tool"
	case callbacks.RetrieverStart:
		return "retriever"
	}
	return "chain"
}

func isStart(k callbacks.Kind) bool {
	return k == callbacks.ChainStart || k == callbacks.ModelStart || k == callbacks.ToolStart || k == callbacks.RetrieverStart
}

func isEnd(k callbacks.Kind) bool {
	switch k {
	case callbacks.ChainEnd, callbacks.ModelEnd, callbacks.ToolEnd, callbacks.RetrieverEnd,
		callbacks.ChainError, callbacks.ModelError, callbacks.ToolError, callbacks.RetrieverError:
		return true
	}
	return false
}

// Handle implements callbacks.Handler.
func (t *Tracer) Handle(_ context.Context, e callbacks.Event) {
	switch {
	case isStart(e.Kind):
		id := uuid(e.RunID)
		now := time.Now().UTC()
		seg := now.Format("20060102T150405.000000Z") + id
		t.mu.Lock()
		info := &runInfo{trace: id, dotted: seg, start: now}
		if p := t.runs[e.ParentRunID]; p != nil {
			info.trace, info.dotted = p.trace, p.dotted+"."+seg
		}
		t.runs[e.RunID] = info
		t.mu.Unlock()
		body := map[string]any{
			"id": id, "name": e.Name, "run_type": runType(e.Kind), "start_time": now.Format(time.RFC3339Nano),
			"inputs": wrap("input", e.Input), "session_name": t.o.Project,
			"trace_id": info.trace, "dotted_order": info.dotted, "extra": map[string]any{"metadata": e.Metadata},
		}
		if e.ParentRunID != "" {
			body["parent_run_id"] = uuid(e.ParentRunID)
		}
		t.enqueue(job{http.MethodPost, "/runs", body})
	case isEnd(e.Kind):
		t.mu.Lock()
		delete(t.runs, e.RunID)
		t.mu.Unlock()
		body := map[string]any{"end_time": time.Now().UTC().Format(time.RFC3339Nano)}
		if e.Err != nil {
			body["error"] = e.Err.Error()
		} else {
			body["outputs"] = wrap("output", e.Output)
		}
		t.enqueue(job{http.MethodPatch, "/runs/" + uuid(e.RunID), body})
	}
}

// wrap makes values safe for JSON: maps pass through, everything else is
// stored under key; unmarshalable values fall back to their text form.
func wrap(key string, v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		if _, err := json.Marshal(m); err == nil {
			return m
		}
	}
	if _, err := json.Marshal(v); err != nil {
		v = fmt.Sprint(v)
	}
	return map[string]any{key: v}
}
