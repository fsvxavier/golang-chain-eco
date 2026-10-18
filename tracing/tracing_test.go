package tracing_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/tracing/langfuse"
	"github.com/fsvxavier/golang-chain-eco/tracing/otlp"
)

func run(h callbacks.Handler) {
	ctx := callbacks.WithHandlers(context.Background(), h)
	ctx, endOuter := callbacks.Start(ctx, callbacks.ChainStart, "agent", map[string]any{"q": 1})
	_, endModel := callbacks.Start(ctx, callbacks.ModelStart, "gpt", "prompt")
	endModel("answer", nil)
	_, endTool := callbacks.Start(ctx, callbacks.ToolStart, "search", "x")
	endTool(nil, errors.New("boom"))
	endOuter("done", nil)
}

func TestLangfuse(t *testing.T) {
	var mu sync.Mutex
	var events []map[string]any
	var user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ = r.BasicAuth()
		var b struct{ Batch []map[string]any }
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		events = append(events, b.Batch...)
		mu.Unlock()
		w.WriteHeader(207)
	}))
	defer srv.Close()
	tr := langfuse.New(langfuse.Options{PublicKey: "pk", SecretKey: "sk", Host: srv.URL})
	run(tr)
	tr.Close()
	mu.Lock()
	defer mu.Unlock()
	types := map[string]int{}
	for _, e := range events {
		types[e["type"].(string)]++
	}
	if user != "pk" || pass != "sk" || types["trace-create"] != 2 || types["span-create"] != 2 || types["generation-create"] != 1 || types["generation-update"] != 1 || types["span-update"] != 2 {
		t.Fatalf("%v %s:%s", types, user, pass)
	}
	var errSeen bool
	for _, e := range events {
		if b := e["body"].(map[string]any); b["level"] == "ERROR" && b["statusMessage"] == "boom" {
			errSeen = true
		}
	}
	if !errSeen {
		t.Fatal("tool error must be reported")
	}
}

func TestOTLP(t *testing.T) {
	var mu sync.Mutex
	var spans []map[string]any
	var hdr, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr, path = r.Header.Get("x-key"), r.URL.Path
		var b struct {
			ResourceSpans []struct {
				ScopeSpans []struct{ Spans []map[string]any } `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		for _, rs := range b.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				spans = append(spans, ss.Spans...)
			}
		}
		mu.Unlock()
	}))
	defer srv.Close()
	ex := otlp.New(otlp.Options{Endpoint: srv.URL, Headers: map[string]string{"x-key": "k"}, ServiceName: "svc"})
	run(ex)
	ex.Close()
	mu.Lock()
	defer mu.Unlock()
	if hdr != "k" || path != "/v1/traces" || len(spans) != 3 {
		t.Fatalf("%s %s %d", hdr, path, len(spans))
	}
	by := map[string]map[string]any{}
	for _, s := range spans {
		by[s["name"].(string)] = s
	}
	root, model, tool := by["agent"], by["gpt"], by["search"]
	if root["parentSpanId"] != nil || model["parentSpanId"] != root["spanId"] || model["traceId"] != root["traceId"] || len(root["traceId"].(string)) != 32 {
		t.Fatalf("hierarchy: %v", spans)
	}
	if tool["status"].(map[string]any)["code"] != float64(2) || !strings.Contains(tool["status"].(map[string]any)["message"].(string), "boom") {
		t.Fatalf("%v", tool["status"])
	}
}
