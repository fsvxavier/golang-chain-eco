package agui_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/integrations/agui"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func app(t *testing.T, fail bool) *graph.Compiled {
	g := graph.NewStateGraph(graph.Schema{"out": channels.LastValue()}).
		AddNode("talk", func(ctx context.Context, s graph.State) (any, error) {
			if fail {
				return nil, context.DeadlineExceeded
			}
			for _, tok := range []string{"Hel", "lo"} {
				callbacks.Emit(ctx, callbacks.Event{Kind: callbacks.ModelNewToken, Chunk: messages.AI(tok)})
			}
			return graph.State{"out": "Hello"}, nil
		}).
		AddEdge(graph.Start, "talk").AddEdge("talk", graph.End)
	c, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func types(evs []agui.Event) string {
	var s []string
	for _, e := range evs {
		s = append(s, e["type"].(string))
	}
	return strings.Join(s, ",")
}

func TestEvents(t *testing.T) {
	var evs []agui.Event
	for e := range agui.Events(context.Background(), app(t, false), agui.RunInput{ThreadID: "t", RunID: "r"}, graph.State{}) {
		evs = append(evs, e)
	}
	got := types(evs)
	for _, want := range []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT,TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "STEP_FINISHED", "STATE_SNAPSHOT"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if !strings.HasSuffix(got, "RUN_FINISHED") || evs[0]["threadId"] != "t" {
		t.Fatal(got)
	}
	var text string
	for _, e := range evs {
		if e["type"] == "TEXT_MESSAGE_CONTENT" {
			text += e["delta"].(string)
		}
	}
	if text != "Hello" {
		t.Fatal(text)
	}
}

func TestRunError(t *testing.T) {
	var last agui.Event
	for e := range agui.Events(context.Background(), app(t, true), agui.RunInput{}, graph.State{}) {
		last = e
	}
	if last["type"] != "RUN_ERROR" {
		t.Fatal(last)
	}
}

func TestHandlerSSE(t *testing.T) {
	srv := httptest.NewServer(agui.Handler(app(t, false), func(agui.RunInput) any { return graph.State{} }))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"id":"m","role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatal(ct)
	}
	var n int
	var last map[string]any
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if after, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			n++
			_ = json.Unmarshal([]byte(after), &last)
		}
	}
	if n < 5 || last["type"] != "RUN_FINISHED" || last["runId"] != "r1" {
		t.Fatal(n, last)
	}
	if r, _ := http.Get(srv.URL); r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(r.StatusCode)
	}
}
