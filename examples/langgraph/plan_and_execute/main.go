// plan_and_execute: a planner writes steps, an executor runs them one by one,
// a replanner decides to continue or answer (port of plan-and-execute.ipynb).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	planner := demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI("1. Find the winner of the 2024 Australian Open\n2. Find their hometown")
	})
	executor := demo.Smart(func(msgs []messages.Message) messages.Message {
		task := demo.LastHuman(msgs)
		if strings.Contains(task, "winner") {
			return messages.AI("Jannik Sinner")
		}
		return messages.AI("San Candido, Italy")
	})

	g := graph.NewStateGraph(graph.Schema{
		"input": channels.LastValue(), "plan": channels.LastValue(),
		"past_steps": graph.Append(), "response": channels.LastValue(),
	}).
		AddNode("plan", func(ctx context.Context, s graph.State) (any, error) {
			out, err := planner.Generate(ctx, []messages.Message{messages.Human("Make a short numbered plan for: " + s["input"].(string))})
			var steps []string
			for _, l := range strings.Split(out.Text(), "\n") {
				if _, step, ok := strings.Cut(l, ". "); ok {
					steps = append(steps, step)
				}
			}
			return graph.State{"plan": steps}, err
		}).
		AddNode("execute", func(ctx context.Context, s graph.State) (any, error) {
			plan := s["plan"].([]string)
			out, err := executor.Generate(ctx, []messages.Message{messages.Human("Do this step: " + plan[0])})
			fmt.Printf("executed %q -> %s\n", plan[0], out.Text())
			return graph.State{"past_steps": []any{[2]string{plan[0], out.Text()}}, "plan": plan[1:]}, err
		}).
		AddNode("replan", func(_ context.Context, s graph.State) (any, error) {
			if len(s["plan"].([]string)) == 0 {
				past := s["past_steps"].([]any)
				return graph.State{"response": past[len(past)-1].([2]string)[1]}, nil
			}
			return nil, nil
		}).
		AddEdge(graph.Start, "plan").AddEdge("plan", "execute").AddEdge("execute", "replan").
		AddConditionalEdges("replan", func(_ context.Context, s graph.State) (any, error) {
			if s["response"] != nil {
				return graph.End, nil
			}
			return "execute", nil
		})
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"input": "What is the hometown of the 2024 Australian Open winner?"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("answer:", out["response"])
	_ = models.Options{}
}
