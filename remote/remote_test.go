package remote_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/remote"
)

func TestRemoteGraph(t *testing.T) {
	var gotKey string
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		switch r.URL.Path {
		case "/threads/t1/runs/wait":
			_ = json.NewEncoder(w).Encode(map[string]any{"answer": "42"})
		case "/threads/t1/state":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{map[string]any{"id": "x", "name": "ask", "interrupts": []any{map[string]any{"value": "ok?"}}}}})
		case "/runs/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: metadata\ndata: {}\n\nevent: values\ndata: {\"n\":1}\n\nevent: values\ndata: {\"n\":2}\n\n")
		case "/threads":
			_ = json.NewEncoder(w).Encode(map[string]any{"thread_id": "t9"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := remote.NewClient(srv.URL)
	c.APIKey = "k"
	g := &remote.Graph{Client: c, AssistantID: "agent"}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("t1"))
	out, err := g.Invoke(ctx, graph.Command{Resume: "yes"})
	if err != nil || out["answer"] != "42" || gotKey != "k" {
		t.Fatalf("%v %v %q", out, err, gotKey)
	}
	if lastBody["command"].(map[string]any)["resume"] != "yes" || lastBody["assistant_id"] != "agent" {
		t.Fatal(lastBody)
	}
	if len(graph.Interrupts(out)) != 1 {
		t.Fatalf("interrupts not surfaced: %v", out)
	}

	var ns []any
	for s, err := range g.Stream(context.Background(), graph.State{"x": 1}) {
		if err != nil {
			t.Fatal(err)
		}
		ns = append(ns, s["n"])
	}
	if fmt.Sprint(ns) != "[1 2]" {
		t.Fatal(ns)
	}
	if id, err := c.CreateThread(context.Background(), nil); err != nil || id != "t9" {
		t.Fatal(id, err)
	}
	if _, err := c.GetRun(context.Background(), "t1", "nope"); err == nil {
		t.Fatal("404 must error")
	}
}
