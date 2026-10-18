// Package otlp exports callback events as OpenTelemetry spans over OTLP/HTTP
// (JSON), which Jaeger, Grafana Tempo, Honeycomb, Datadog, New Relic and most
// observability backends ingest. No OpenTelemetry SDK is required.
package otlp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
)

// Options configures the exporter.
type Options struct {
	// Endpoint is the OTLP/HTTP base URL (default $OTEL_EXPORTER_OTLP_ENDPOINT
	// or http://localhost:4318); spans go to <Endpoint>/v1/traces.
	Endpoint string
	// Headers are added to every request (e.g. API keys).
	Headers     map[string]string
	ServiceName string // default golang-chain-eco
	HTTP        *http.Client
	OnError     func(error)
	Buffer      int
}

// Exporter is a callbacks.Handler.
type Exporter struct {
	o    Options
	q    chan map[string]any
	wg   sync.WaitGroup
	mu   sync.Mutex
	runs map[string]*span
	once sync.Once
}

type span struct {
	trace, id, parent string
	name, kind        string
	start             time.Time
	input             any
}

// New starts the background exporter.
func New(o Options) *Exporter {
	if o.Endpoint == "" {
		o.Endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if o.Endpoint == "" {
		o.Endpoint = "http://localhost:4318"
	}
	o.Endpoint = strings.TrimRight(o.Endpoint, "/")
	if o.ServiceName == "" {
		o.ServiceName = "golang-chain-eco"
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
	e := &Exporter{o: o, q: make(chan map[string]any, o.Buffer), runs: map[string]*span{}}
	e.wg.Add(1)
	go e.loop()
	return e
}

// Close flushes queued spans.
func (e *Exporter) Close() {
	e.once.Do(func() { close(e.q) })
	e.wg.Wait()
}

func (e *Exporter) loop() {
	defer e.wg.Done()
	var batch []map[string]any
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := e.send(batch); err != nil {
			e.o.OnError(err)
		}
		batch = nil
	}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case s, ok := <-e.q:
			if !ok {
				flush()
				return
			}
			batch = append(batch, s)
			if len(batch) >= 64 {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

func str(k, v string) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
}

func (e *Exporter) send(spans []map[string]any) error {
	body := map[string]any{"resourceSpans": []any{map[string]any{
		"resource":   map[string]any{"attributes": []any{str("service.name", e.o.ServiceName)}},
		"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "golang-chain-eco"}, "spans": spans}},
	}}}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, e.o.Endpoint+"/v1/traces", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.o.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("otlp: http %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func text(v any) string {
	s, err := json.Marshal(v)
	if err != nil {
		s = []byte(fmt.Sprint(v))
	}
	if len(s) > 4096 {
		s = append(s[:4096:4096], "…"...)
	}
	return string(s)
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

// Handle implements callbacks.Handler. A span is exported when its run ends.
func (e *Exporter) Handle(_ context.Context, ev callbacks.Event) {
	switch ev.Kind {
	case callbacks.ChainStart, callbacks.ToolStart, callbacks.RetrieverStart, callbacks.ModelStart:
		s := &span{id: randHex(8), name: ev.Name, kind: runType(ev.Kind), start: time.Now(), input: ev.Input}
		e.mu.Lock()
		if p := e.runs[ev.ParentRunID]; p != nil {
			s.trace, s.parent = p.trace, p.id
		} else {
			s.trace = randHex(16)
		}
		e.runs[ev.RunID] = s
		e.mu.Unlock()
	case callbacks.ChainEnd, callbacks.ToolEnd, callbacks.RetrieverEnd, callbacks.ModelEnd,
		callbacks.ChainError, callbacks.ToolError, callbacks.RetrieverError, callbacks.ModelError:
		e.mu.Lock()
		s := e.runs[ev.RunID]
		delete(e.runs, ev.RunID)
		e.mu.Unlock()
		if s == nil {
			return
		}
		attrs := []any{str("langchain.run_type", s.kind), str("langchain.input", text(s.input))}
		if s.kind == "llm" {
			attrs = append(attrs, str("gen_ai.request.model", s.name))
		}
		out := map[string]any{
			"traceId": s.trace, "spanId": s.id, "name": s.name, "kind": 1,
			"startTimeUnixNano": strconv.FormatInt(s.start.UnixNano(), 10),
			"endTimeUnixNano":   strconv.FormatInt(time.Now().UnixNano(), 10),
		}
		if s.parent != "" {
			out["parentSpanId"] = s.parent
		}
		if ev.Err != nil {
			out["status"] = map[string]any{"code": 2, "message": ev.Err.Error()}
		} else {
			attrs = append(attrs, str("langchain.output", text(ev.Output)))
			out["status"] = map[string]any{"code": 1}
		}
		out["attributes"] = attrs
		func() {
			defer func() { _ = recover() }() // queue closed
			select {
			case e.q <- out:
			default:
				e.o.OnError(fmt.Errorf("otlp: queue full, dropped span %s", s.name))
			}
		}()
	}
}
