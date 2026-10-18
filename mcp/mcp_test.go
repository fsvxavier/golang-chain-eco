package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/mcp"
)

func handle(req map[string]any) (any, bool) {
	switch req["method"] {
	case "initialize":
		return map[string]any{"serverInfo": map[string]any{"name": "fake"}}, true
	case "tools/list":
		return map[string]any{"tools": []any{map[string]any{
			"name": "echo", "description": "echo",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
		}}}, true
	case "tools/call":
		p := req["params"].(map[string]any)
		text := p["arguments"].(map[string]any)["text"].(string)
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "echo:" + text}}}, true
	}
	return nil, false
}

func TestPipeAndHTTP(t *testing.T) {
	// stdio-style over pipes
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	go func() {
		sc := bufio.NewScanner(sr)
		for sc.Scan() {
			var req map[string]any
			_ = json.Unmarshal(sc.Bytes(), &req)
			if res, ok := handle(req); ok {
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": res})
				sw.Write(append(b, '\n'))
			}
		}
	}()
	ctx := context.Background()
	c, err := mcp.Connect(ctx, mcp.NewPipeTransport(cw, cr))
	if err != nil {
		t.Fatal(err)
	}
	ts, err := c.Tools(ctx, "srv")
	if err != nil || len(ts) != 1 || ts[0].Name() != "srv_echo" {
		t.Fatalf("%v %v", ts, err)
	}
	out, err := ts[0].Call(ctx, map[string]any{"text": "hi"})
	if err != nil || out != "echo:hi" {
		t.Fatalf("%v %v", out, err)
	}
	if _, err := ts[0].Call(ctx, map[string]any{}); err == nil {
		t.Fatal("missing required arg must fail validation")
	}

	// streamable HTTP, SSE response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		res, ok := handle(req)
		if !ok {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": res})
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message\ndata: " + string(b) + "\n\n"))
	}))
	defer srv.Close()
	hc, err := mcp.Connect(ctx, mcp.HTTP(srv.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	ts, _ = hc.Tools(ctx, "")
	if out, err := ts[0].Call(ctx, map[string]any{"text": "x"}); err != nil || out != "echo:x" {
		t.Fatalf("%v %v", out, err)
	}
}
