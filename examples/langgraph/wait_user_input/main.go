// wait_user_input: pause a graph to ask the user something, then resume
// (port of human_in_the_loop/wait-user-input.ipynb).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	g := graph.NewStateGraph(graph.Schema{"location": channels.LastValue(), "report": channels.LastValue()}).
		AddNode("ask_location", func(ctx context.Context, _ graph.State) (any, error) {
			loc := graph.Interrupt(ctx, "Where are you?")
			return graph.State{"location": loc}, nil
		}).
		AddNode("report", func(_ context.Context, s graph.State) (any, error) {
			return graph.State{"report": fmt.Sprintf("It is sunny in %v", s["location"])}, nil
		}).
		AddEdge(graph.Start, "ask_location").AddEdge("ask_location", "report").AddEdge("report", graph.End)
	app, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		log.Fatal(err)
	}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("1"))

	out, _ := app.Invoke(ctx, graph.State{})
	for _, i := range graph.Interrupts(out) {
		fmt.Printf("graph paused at %q asking: %v\n", i.Node, i.Value)
	}
	snap, _ := app.GetState(ctx)
	fmt.Println("next nodes:", snap.Next)

	fmt.Println("user answers: Lisbon")
	out, err = app.Invoke(ctx, graph.Command{Resume: "Lisbon"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out["report"])
}
