// rewoo: Reasoning WithOut Observation — plan all tool calls up front with
// placeholders (#E1, #E2), execute them, then solve (port of rewoo/rewoo.ipynb).
package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type step struct{ Plan, Var, Tool, Input string }

var stepRe = regexp.MustCompile(`Plan:\s*(.+)\n\s*(#E\d+)\s*=\s*(\w+)\[(.+)\]`)

func main() {
	planner := demo.Smart(func([]messages.Message) messages.Message {
		return messages.AI("Plan: Find the 2024 Australian Open men's champion\n#E1 = Search[2024 Australian Open winner]\n" +
			"Plan: Find the birthplace of that champion\n#E2 = Search[birthplace of #E1]")
	})
	search := func(q string) string {
		if strings.Contains(q, "birthplace") {
			return "Sesto, Italy"
		}
		return "Jannik Sinner"
	}
	solver := demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI("Jannik Sinner was born in Sesto, Italy.")
	})

	g := graph.NewStateGraph(graph.Schema{
		"task": channels.LastValue(), "steps": channels.LastValue(),
		"results": channels.BinOp(func(c, u any) (any, error) {
			m := map[string]string{}
			for k, v := range c.(map[string]string) {
				m[k] = v
			}
			for k, v := range u.(map[string]string) {
				m[k] = v
			}
			return m, nil
		}, func() any { return map[string]string{} }),
		"result": channels.LastValue(),
	}).
		AddNode("plan", func(ctx context.Context, s graph.State) (any, error) {
			out, err := planner.Generate(ctx, []messages.Message{messages.Human("Make a plan for: " + s["task"].(string))})
			var steps []step
			for _, m := range stepRe.FindAllStringSubmatch(out.Text(), -1) {
				steps = append(steps, step{m[1], m[2], m[3], m[4]})
			}
			fmt.Printf("planned %d steps up front\n", len(steps))
			return graph.State{"steps": steps}, err
		}).
		AddNode("tool", func(_ context.Context, s graph.State) (any, error) {
			steps, results := s["steps"].([]step), s["results"].(map[string]string)
			st := steps[len(results)]
			input := st.Input
			for v, r := range results { // substitute earlier evidence
				input = strings.ReplaceAll(input, v, r)
			}
			res := search(input)
			fmt.Printf("  %s = %s[%s] -> %s\n", st.Var, st.Tool, input, res)
			return graph.State{"results": map[string]string{st.Var: res}}, nil
		}).
		AddNode("solve", func(ctx context.Context, s graph.State) (any, error) {
			out, err := solver.Generate(ctx, []messages.Message{messages.Human(fmt.Sprintf("Task: %s\nEvidence: %v", s["task"], s["results"]))})
			return graph.State{"result": out.Text()}, err
		}).
		AddEdge(graph.Start, "plan").AddEdge("plan", "tool").
		AddConditionalEdges("tool", func(_ context.Context, s graph.State) (any, error) {
			if len(s["results"].(map[string]string)) < len(s["steps"].([]step)) {
				return "tool", nil
			}
			return "solve", nil
		}).AddEdge("solve", graph.End)
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"task": "Where was the 2024 Australian Open champion born?"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out["result"])
}
