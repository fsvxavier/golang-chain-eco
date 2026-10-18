// llm_compiler: a planner emits a DAG of tool calls; every call whose
// dependencies are done runs in parallel via Send (idea of
// llm-compiler/LLMCompiler.ipynb), then a joiner builds the answer.
package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type task struct {
	ID   int
	Tool string
	Args string
	Deps []int
}

// merge is the reducer of "done": parallel tasks each add their own result.
func merge(cur, upd any) (any, error) {
	m := map[int]int{}
	for k, v := range cur.(map[int]int) {
		m[k] = v
	}
	for k, v := range upd.(map[int]int) {
		m[k] = v
	}
	return m, nil
}

func main() {
	// planner output: tasks 1 and 2 are independent, task 3 needs both
	plan := []task{{1, "population", "France", nil}, {2, "population", "Germany", nil}, {3, "sum", "", []int{1, 2}}}
	pop := map[string]int{"France": 68, "Germany": 84}

	g := graph.NewStateGraph(graph.Schema{
		"plan": channels.LastValue(), "answer": channels.LastValue(),
		"done": channels.BinOp(merge, func() any { return map[int]int{} }),
	}).
		AddNode("plan", func(context.Context, graph.State) (any, error) { return graph.State{"plan": plan}, nil }).
		AddNode("schedule", func(context.Context, graph.State) (any, error) { return nil, nil }).
		AddNode("exec", func(_ context.Context, s graph.State) (any, error) {
			t, done := s["task"].(task), s["deps"].(map[int]int)
			time.Sleep(20 * time.Millisecond) // pretend this is a slow API call
			r := 0
			switch t.Tool {
			case "population":
				r = pop[t.Args]
			case "sum":
				for _, d := range t.Deps {
					r += done[d]
				}
			}
			fmt.Printf("  task %d %s(%s) = %d\n", t.ID, t.Tool, t.Args, r)
			return graph.State{"done": map[int]int{t.ID: r}}, nil
		}).
		AddNode("join", func(_ context.Context, s graph.State) (any, error) {
			return graph.State{"answer": fmt.Sprintf("France + Germany population: %dM", s["done"].(map[int]int)[3])}, nil
		}).
		AddEdge(graph.Start, "plan").AddEdge("plan", "schedule").
		AddConditionalEdges("schedule", func(_ context.Context, s graph.State) (any, error) {
			done := s["done"].(map[int]int)
			var sends []graph.Send
			var ids []int
			for _, t := range s["plan"].([]task) {
				if _, ok := done[t.ID]; ok {
					continue
				}
				ready := true
				for _, d := range t.Deps {
					if _, ok := done[d]; !ok {
						ready = false
					}
				}
				if ready {
					sends = append(sends, graph.Send{Node: "exec", Arg: graph.State{"task": t, "deps": done}})
					ids = append(ids, t.ID)
				}
			}
			if len(sends) == 0 {
				return "join", nil
			}
			sort.Ints(ids)
			fmt.Println("running in parallel:", ids)
			return sends, nil
		}).
		AddEdge("exec", "schedule").AddEdge("join", graph.End)
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	out, err := app.Invoke(context.Background(), graph.State{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s (%dms)\n", out["answer"], time.Since(start).Milliseconds())
}
