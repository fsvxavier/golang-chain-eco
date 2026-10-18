// self_discover: the model selects reasoning modules, adapts them to the
// task, builds a reasoning structure, then solves with it
// (port of self-discover/self-discover.ipynb): a 4-step prompt pipeline.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

var modules = "1. Break the problem into sub-problems\n2. Think step by step\n3. Critical thinking\n4. Use simulation"

func main() {
	model := demo.Smart(func(msgs []messages.Message) messages.Message {
		p := demo.LastHuman(msgs)
		switch {
		case strings.HasPrefix(p, "SELECT"):
			return messages.AI("1. Break the problem into sub-problems\n2. Think step by step")
		case strings.HasPrefix(p, "ADAPT"):
			return messages.AI("1. Split the rope puzzle into the two ropes and the timing events\n2. Reason through each burn event step by step")
		case strings.HasPrefix(p, "STRUCTURE"):
			return messages.AI(`{"burn_rope_A_both_ends":"","burn_rope_B_one_end_when_A_done":"","measure_total":""}`)
		}
		return messages.AI("Light rope A at both ends and rope B at one end. When A burns out (30 min) light B's other end: B finishes 15 minutes later = 45 minutes.")
	})
	step := func(key, tmpl string, from ...string) graph.NodeFunc {
		return func(ctx context.Context, s graph.State) (any, error) {
			prompt := tmpl
			for _, f := range from {
				prompt += fmt.Sprintf("\n%s:\n%v", f, s[f])
			}
			out, err := model.Generate(ctx, []messages.Message{messages.Human(prompt)})
			fmt.Printf("[%s] %s\n", key, strings.ReplaceAll(out.Text(), "\n", " | "))
			return graph.State{key: out.Text()}, err
		}
	}
	g := graph.NewStateGraph(graph.Schema{"task": channels.LastValue(), "selected": channels.LastValue(),
		"adapted": channels.LastValue(), "structure": channels.LastValue(), "answer": channels.LastValue()}).
		AddNode("select", func(ctx context.Context, s graph.State) (any, error) {
			return step("selected", "SELECT useful modules for the task from:\n"+modules+"\nTask: "+s["task"].(string))(ctx, s)
		}).
		AddNode("adapt", step("adapted", "ADAPT the selected modules to the task.", "selected", "task")).
		AddNode("structure", step("structure", "STRUCTURE a JSON reasoning plan from the adapted modules.", "adapted")).
		AddNode("reason", step("answer", "SOLVE the task by filling the reasoning structure.", "structure", "task")).
		AddEdge(graph.Start, "select").AddEdge("select", "adapt").AddEdge("adapt", "structure").
		AddEdge("structure", "reason").AddEdge("reason", graph.End)
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"task": "Two ropes each burn in 60 minutes (unevenly). Measure 45 minutes."})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nanswer:", out["answer"])
}
