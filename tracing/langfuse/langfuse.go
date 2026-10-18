// Package langfuse ships callback events to Langfuse through its public
// ingestion API. Chains, tools and models become spans and generations
// nested under one trace per root run.
package langfuse

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
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Options configures the tracer.
type Options struct {
	PublicKey string // default $LANGFUSE_PUBLIC_KEY
	SecretKey string // default $LANGFUSE_SECRET_KEY
	Host      string // default $LANGFUSE_HOST or https://cloud.langfuse.com
	HTTP      *http.Client
	OnError   func(error)
	Buffer    int
}

// Tracer is a callbacks.Handler.
type Tracer struct {
	o    Options
	q    chan map[string]any
	wg   sync.WaitGroup
	mu   sync.Mutex
	runs map[string]*info
	once sync.Once
}

type info struct{ trace, kind string }

// New starts the background sender.
func New(o Options) *Tracer {
	if o.PublicKey == "" {
		o.PublicKey = os.Getenv("LANGFUSE_PUBLIC_KEY")
	}
	if o.SecretKey == "" {
		o.SecretKey = os.Getenv("LANGFUSE_SECRET_KEY")
	}
	if o.Host == "" {
		o.Host = os.Getenv("LANGFUSE_HOST")
	}
	if o.Host == "" {
		o.Host = "https://cloud.langfuse.com"
	}
	o.Host = strings.TrimRight(o.Host, "/")
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if o.OnError == nil {
		o.OnError = func(error) {}
	}
	if o.Buffer <= 0 {
		o.Buffer = 1024
	}
	t := &Tracer{o: o, q: make(chan map[string]any, o.Buffer), runs: map[string]*info{}}
	t.wg.Add(1)
	go t.loop()
	return t
}

// Close flushes queued events.
func (t *Tracer) Close() {
	t.once.Do(func() { close(t.q) })
	t.wg.Wait()
}

func (t *Tracer) loop() {
	defer t.wg.Done()
	var batch []map[string]any
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := t.send(batch); err != nil {
			t.o.OnError(err)
		}
		batch = nil
	}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case ev, ok := <-t.q:
			if !ok {
				flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= 50 {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

func (t *Tracer) send(batch []map[string]any) error {
	b, err := json.Marshal(map[string]any{"batch": batch})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, t.o.Host+"/api/public/ingestion", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(t.o.PublicKey, t.o.SecretKey)
	resp, err := t.o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("langfuse: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (t *Tracer) enqueue(typ string, body map[string]any) {
	defer func() { _ = recover() }() // queue closed
	ev := map[string]any{"id": messages.NewID(), "type": typ, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "body": body}
	select {
	case t.q <- ev:
	default:
		t.o.OnError(fmt.Errorf("langfuse: queue full, dropped %s", typ))
	}
}

func uuid(id string) string {
	if len(id) != 32 {
		return id
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func safe(v any) any {
	if _, err := json.Marshal(v); err != nil {
		return fmt.Sprint(v)
	}
	return v
}

// Handle implements callbacks.Handler.
func (t *Tracer) Handle(_ context.Context, e callbacks.Event) {
	id := uuid(e.RunID)
	switch e.Kind {
	case callbacks.ChainStart, callbacks.ToolStart, callbacks.RetrieverStart, callbacks.ModelStart:
		kind := "span"
		if e.Kind == callbacks.ModelStart {
			kind = "generation"
		}
		t.mu.Lock()
		trace := id
		if p := t.runs[e.ParentRunID]; p != nil {
			trace = p.trace
		}
		t.runs[e.RunID] = &info{trace, kind}
		t.mu.Unlock()
		if trace == id { // root run: open the trace
			t.enqueue("trace-create", map[string]any{"id": trace, "name": e.Name, "input": safe(e.Input), "timestamp": time.Now().UTC().Format(time.RFC3339Nano)})
		}
		body := map[string]any{"id": id, "traceId": trace, "name": e.Name, "startTime": time.Now().UTC().Format(time.RFC3339Nano), "input": safe(e.Input)}
		if e.ParentRunID != "" {
			body["parentObservationId"] = uuid(e.ParentRunID)
		}
		if kind == "generation" {
			body["model"] = e.Name
		}
		t.enqueue(kind+"-create", body)
	case callbacks.ChainEnd, callbacks.ToolEnd, callbacks.RetrieverEnd, callbacks.ModelEnd,
		callbacks.ChainError, callbacks.ToolError, callbacks.RetrieverError, callbacks.ModelError:
		t.mu.Lock()
		in := t.runs[e.RunID]
		delete(t.runs, e.RunID)
		t.mu.Unlock()
		if in == nil {
			return
		}
		body := map[string]any{"id": id, "traceId": in.trace, "endTime": time.Now().UTC().Format(time.RFC3339Nano)}
		if e.Err != nil {
			body["level"], body["statusMessage"] = "ERROR", e.Err.Error()
		} else {
			body["output"] = safe(e.Output)
		}
		t.enqueue(in.kind+"-update", body)
		if in.trace == id { // close the trace with the final output
			t.enqueue("trace-create", map[string]any{"id": in.trace, "output": body["output"]})
		}
	}
}
