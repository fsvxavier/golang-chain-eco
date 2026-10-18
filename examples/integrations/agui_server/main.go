// agui_server: serves a compiled graph over the AG-UI protocol (the event
// stream CopilotKit and assistant-ui consume) and calls it once in-process,
// printing the events. Point a real AG-UI client at the same handler.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/integrations/agui"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	app, err := graph.NewStateGraph(graph.Schema{"answer": channels.LastValue()}).
		AddNode("respond", func(ctx context.Context, s graph.State) (any, error) {
			// a real node would call a streaming chat model; tokens reach the
			// frontend as TEXT_MESSAGE_CONTENT events
			for _, tok := range []string{"AG-UI ", "streams ", "tokens."} {
				callbacks.Emit(ctx, callbacks.Event{Kind: callbacks.ModelNewToken, Chunk: messages.AI(tok)})
			}
			return graph.State{"answer": "AG-UI streams tokens."}, nil
		}).
		AddEdge(graph.Start, "respond").AddEdge("respond", graph.End).Compile()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/agent", agui.Handler(app, func(agui.RunInput) any { return graph.State{} }))
	srv := httptest.NewServer(mux) // use http.ListenAndServe in a real service
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/agent", "application/json",
		strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"id":"m1","role":"user","content":"hi"}]}`))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	fmt.Println("Content-Type:", resp.Header.Get("Content-Type"))
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") {
			fmt.Println(strings.TrimPrefix(line, "data: "))
		}
	}
}
