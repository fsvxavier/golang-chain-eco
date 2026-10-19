// typed_state: estado tipado (struct com tags) sobre StateGraph, sem modelo.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// OrderState is the graph state. Tags name the channel and the reducer.
type OrderState struct {
	Messages []messages.Message `graph:"messages,reducer=messages"`
	Total    int                `graph:"total,reducer=sum"`
	Log      []string           `graph:"log,reducer=append"`
	Approved bool               `graph:"approved"`
	Items    int                `graph:"items"`
}

// Validate runs before every node; a negative quantity fails the run.
func (s OrderState) Validate() error {
	if s.Items < 0 {
		return errors.New("items must not be negative")
	}
	return nil
}

func main() {
	g := graph.NewTyped[OrderState]().
		AddNode("price", func(_ context.Context, s OrderState) (graph.Update, error) {
			return graph.Update{
				"total": s.Items * 10,
				"log":   []string{fmt.Sprintf("priced %d items", s.Items)},
			}, nil
		}).
		AddNode("approve", func(ctx context.Context, s OrderState) (graph.Update, error) {
			// pause for a human decision; the answer comes back as Resume
			answer := graph.Interrupt(ctx, fmt.Sprintf("approve total %d?", s.Total))
			approved, _ := answer.(bool)
			return graph.Update{"approved": approved, "log": []string{fmt.Sprint("answer: ", answer)}}, nil
		}).
		AddNode("confirm", func(_ context.Context, s OrderState) (graph.Update, error) {
			status := "rejected"
			if s.Approved {
				status = "approved"
			}
			return graph.Update{"messages": []messages.Message{messages.AI("order " + status)}}, nil
		}).
		SetEntryPoint("price").
		AddEdge("price", "approve").
		AddEdge("approve", "confirm").
		SetFinishPoint("confirm")

	c, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		log.Fatal(err)
	}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("order-1"))

	out, ints, err := c.Invoke(ctx, OrderState{Items: 3, Messages: []messages.Message{messages.Human("3 items please")}})
	if err != nil {
		log.Fatal(err)
	}
	for _, i := range ints {
		fmt.Printf("paused at %s: %v\n", i.Node, i.Value)
	}
	fmt.Printf("total so far: %d\n", out.Total)

	// the reviewer approves; resume the same thread
	out, _, err = c.Invoke(ctx, graph.Command{Resume: true})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("log: %v\n", out.Log)
	fmt.Printf("final: %s (total %d)\n", out.Messages[len(out.Messages)-1].Content, out.Total)

	// a bad input is rejected by Validate before any node runs
	bad := runnables.WithConfig(context.Background(), graph.Thread("order-2"))
	if _, _, err := c.Invoke(bad, OrderState{Items: -1}); err != nil {
		fmt.Println("rejected:", err)
	}
}
