// info_gather: a chatbot that collects requirements through conversation,
// then switches to generating the final prompt once complete
// (port of chatbots/information-gather-prompting.ipynb).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	collector := demo.Smart(func(msgs []messages.Message) messages.Message {
		have := messages.Buffer(msgs)
		switch {
		case !strings.Contains(have, "objective"):
			return messages.AI("What is the objective of the prompt?")
		case !strings.Contains(have, "variables"):
			return messages.AI("Which variables should the prompt accept?")
		}
		return messages.AI("READY: objective=summarise tickets; variables=ticket_text")
	})
	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages(), "spec": channels.LastValue(), "prompt": channels.LastValue()}).
		AddNode("gather", func(ctx context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			out, err := collector.Generate(ctx, msgs)
			upd := graph.State{"messages": []messages.Message{out}}
			if spec, ok := strings.CutPrefix(out.Text(), "READY: "); ok {
				upd["spec"] = spec
			}
			return upd, err
		}).
		AddNode("write_prompt", func(_ context.Context, s graph.State) (any, error) {
			return graph.State{"prompt": "You are an assistant. Task per spec [" + s["spec"].(string) + "]. Input: {ticket_text}"}, nil
		}).
		AddEdge(graph.Start, "gather").
		AddConditionalEdges("gather", func(_ context.Context, s graph.State) (any, error) {
			if s["spec"] != nil {
				return "write_prompt", nil
			}
			return graph.End, nil // wait for the next user message
		}).
		AddEdge("write_prompt", graph.End)
	app, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		log.Fatal(err)
	}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("chat-1"))
	for _, user := range []string{"I need a prompt", "the objective is to summarise tickets", "the variables: ticket_text"} {
		out, err := app.Invoke(ctx, graph.State{"messages": user})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("user: %s\nbot:  %s\n", user, demo.Last(out))
		if p, ok := out["prompt"]; ok {
			fmt.Println("generated prompt:", p)
		}
	}
}
