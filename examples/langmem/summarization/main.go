// summarization: short-term memory. A running summary keeps a long chat inside
// a token budget while the full history stays in state
// (port of LangMem's summarization guide).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

func main() {
	ctx := runnables.WithConfig(context.Background(), graph.Thread("t1"))
	summarizer := demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI(fmt.Sprintf("[summary of %d earlier messages: user is Bob, likes cats]", len(msgs)-1))
	})
	assistant := demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI(fmt.Sprintf("(reply; the model saw %d messages)", len(msgs)))
	})

	node := langmem.SummarizationNode{SummarizeOptions: langmem.SummarizeOptions{
		Model: summarizer, MaxTokens: 60, MaxSummaryTokens: 15,
	}}
	schema := langmem.SummarizationSchema()
	schema["messages"] = graph.Messages()
	g := graph.NewStateGraph(schema).
		AddNode("summarize", node.Node()).
		AddNode("call_model", func(ctx context.Context, s graph.State) (any, error) {
			// the model reads the summarized view, not the full history
			view, _ := messages.Coerce(s["summarized_messages"])
			out, err := assistant.Generate(ctx, view)
			return graph.State{"messages": []messages.Message{out}}, err
		}).
		AddEdge(graph.Start, "summarize").AddEdge("summarize", "call_model")
	app, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		log.Fatal(err)
	}
	for _, q := range []string{
		"Hi, my name is Bob.", "Write a short poem about cats, please, make it a long one.",
		"Now do the same but for dogs, again make it fairly long.", "What's my name?",
	} {
		out, err := app.Invoke(ctx, graph.State{"messages": q})
		if err != nil {
			log.Fatal(err)
		}
		full, _ := messages.Coerce(out["messages"])
		view, _ := messages.Coerce(out["summarized_messages"])
		fmt.Printf("Q: %-58s history=%d model-view=%d\n", q, len(full), len(view))
		if len(view) > 0 && view[0].Name == "summary" {
			fmt.Println("   ", view[0].Text())
		}
	}
}
