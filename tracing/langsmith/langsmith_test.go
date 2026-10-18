package langsmith_test

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
	"github.com/fsvxavier/golang-chain-eco/tracing/langsmith"
)

func TestTracer(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.Header.Get("x-api-key") != "key" {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		got, methods = append(got, b), append(methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	tr := langsmith.New(langsmith.Options{APIKey: "key", Endpoint: srv.URL, Project: "p"})
	ctx := callbacks.WithHandlers(context.Background(), tr)
	ctx, endOuter := callbacks.Start(ctx, callbacks.ChainStart, "outer", map[string]any{"q": 1})
	_, endInner := callbacks.Start(ctx, callbacks.ToolStart, "inner", "x")
	endInner("ok", nil)
	endOuter(nil, errors.New("boom"))
	tr.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("want 4 requests, got %d: %v", len(got), methods)
	}
	outer, inner := got[0], got[1]
	if outer["name"] != "outer" || outer["session_name"] != "p" || outer["run_type"] != "chain" {
		t.Fatal(outer)
	}
	if inner["parent_run_id"] != outer["id"] || inner["trace_id"] != outer["id"] || inner["run_type"] != "tool" {
		t.Fatal(inner)
	}
	if !strings.HasPrefix(inner["dotted_order"].(string), outer["dotted_order"].(string)+".") {
		t.Fatalf("dotted order: %v / %v", outer["dotted_order"], inner["dotted_order"])
	}
	if !strings.HasPrefix(methods[2], "PATCH /runs/") || got[3]["error"] != "boom" {
		t.Fatal(methods, got[3])
	}
}
