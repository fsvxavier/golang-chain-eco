// Package agui serves a compiled graph over the AG-UI protocol, the open
// event protocol that connects agents to frontends (CopilotKit, assistant-ui,
// any AG-UI client). A POST with a RunAgentInput answers with a
// text/event-stream of AG-UI events:
//
//	RUN_STARTED, STEP_STARTED/FINISHED (per node), TEXT_MESSAGE_START/
//	CONTENT/END (streamed LLM tokens), STATE_SNAPSHOT, RUN_FINISHED | RUN_ERROR
//
//	http.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any {
//		return graph.State{"messages": toMessages(in.Messages)}
//	}))
package agui

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// RunInput is the AG-UI RunAgentInput (fields the adapter needs).
type RunInput struct {
	ThreadID string `json:"threadId"`
	RunID    string `json:"runId"`
	State    any    `json:"state"`
	Messages []struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	ForwardedProps map[string]any `json:"forwardedProps"`
}

// Event is one AG-UI event; Fields are merged next to "type".
type Event map[string]any

// Handler returns the POST handler. build converts the request into the
// graph's input; the thread id becomes the checkpoint thread.
func Handler(g *graph.Compiled, build func(RunInput) any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var in RunInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&in); err != nil {
			http.Error(w, "bad RunAgentInput: "+err.Error(), http.StatusBadRequest)
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for ev := range Events(r.Context(), g, in, build(in)) {
			b, _ := json.Marshal(ev)
			w.Write([]byte("data: "))
			w.Write(b)
			w.Write([]byte("\n\n"))
			fl.Flush()
		}
	})
}

func newID() string { return messages.NewID() }

// Events runs the graph and returns the AG-UI event stream (closed at the end).
func Events(ctx context.Context, g *graph.Compiled, in RunInput, input any) <-chan Event {
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		if in.ThreadID != "" {
			ctx = runnables.WithConfig(ctx, runnables.Config{Configurable: map[string]any{"thread_id": in.ThreadID}})
		}
		send := func(e Event) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if !send(Event{"type": "RUN_STARTED", "threadId": in.ThreadID, "runId": in.RunID}) {
			return
		}
		var msgID, msgNode string
		endMsg := func() bool {
			if msgID == "" {
				return true
			}
			id := msgID
			msgID, msgNode = "", ""
			return send(Event{"type": "TEXT_MESSAGE_END", "messageId": id})
		}
		for ev, err := range g.StreamEvents(ctx, input, graph.ModeMessages, graph.ModeUpdates, graph.ModeValues) {
			if err != nil {
				endMsg()
				send(Event{"type": "RUN_ERROR", "message": err.Error()})
				return
			}
			switch ev.Mode {
			case graph.ModeMessages:
				m, ok := ev.Data.(messages.Message)
				if !ok || m.Text() == "" {
					continue
				}
				if msgID != "" && msgNode != ev.Node {
					if !endMsg() {
						return
					}
				}
				if msgID == "" {
					msgID, msgNode = newID(), ev.Node
					if !send(Event{"type": "TEXT_MESSAGE_START", "messageId": msgID, "role": "assistant"}) {
						return
					}
				}
				if !send(Event{"type": "TEXT_MESSAGE_CONTENT", "messageId": msgID, "delta": m.Text()}) {
					return
				}
			case graph.ModeUpdates:
				if !endMsg() || !send(Event{"type": "STEP_STARTED", "stepName": ev.Node}) || !send(Event{"type": "STEP_FINISHED", "stepName": ev.Node}) {
					return
				}
			case graph.ModeValues:
				if !send(Event{"type": "STATE_SNAPSHOT", "snapshot": ev.Data}) {
					return
				}
			}
		}
		endMsg()
		send(Event{"type": "RUN_FINISHED", "threadId": in.ThreadID, "runId": in.RunID})
	}()
	return out
}
