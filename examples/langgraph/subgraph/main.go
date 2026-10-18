// subgraph: a compiled graph used as a node of a parent graph
// (port of subgraph.ipynb).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	// child: clean text, then count words
	child, err := graph.NewStateGraph(graph.Schema{"text": channels.LastValue(), "words": channels.LastValue()}).
		AddNode("clean", func(_ context.Context, s graph.State) (any, error) {
			return graph.State{"text": strings.ToLower(strings.TrimSpace(s["text"].(string)))}, nil
		}).
		AddNode("count", func(_ context.Context, s graph.State) (any, error) {
			return graph.State{"words": len(strings.Fields(s["text"].(string)))}, nil
		}).
		AddEdge(graph.Start, "clean").AddEdge("clean", "count").AddEdge("count", graph.End).Compile()
	if err != nil {
		log.Fatal(err)
	}

	// parent: embeds the child as a node and maps state in/out
	parent, err := graph.NewStateGraph(graph.Schema{"text": channels.LastValue(), "words": channels.LastValue(), "summary": channels.LastValue()}).
		AddNode("analyse", func(ctx context.Context, s graph.State) (any, error) {
			out, err := child.Invoke(ctx, graph.State{"text": s["text"]})
			return graph.State{"words": out["words"]}, err
		}).
		AddNode("summarise", func(_ context.Context, s graph.State) (any, error) {
			return graph.State{"summary": fmt.Sprintf("%v words", s["words"])}, nil
		}).
		AddEdge(graph.Start, "analyse").AddEdge("analyse", "summarise").AddEdge("summarise", graph.End).Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := parent.Invoke(context.Background(), graph.State{"text": "  Hello Graph WORLD from Go  "})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out["summary"])
}
