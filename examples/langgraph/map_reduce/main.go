// map_reduce: fan out with Send, run workers in parallel, reduce the results
// (the Send API from the LangGraph docs / LLMCompiler-style parallel tasks).
package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	g := graph.NewStateGraph(graph.Schema{
		"subjects": channels.LastValue(), "jokes": graph.Append(), "best": channels.LastValue(),
	}).
		AddNode("worker", func(_ context.Context, s graph.State) (any, error) {
			subj := s["subject"].(string)
			return graph.State{"jokes": []any{"Why did the " + subj + " cross the road? To get to the other side."}}, nil
		}).
		AddNode("pick_best", func(_ context.Context, s graph.State) (any, error) {
			js := s["jokes"].([]any)
			strs := make([]string, len(js))
			for i, j := range js {
				strs[i] = j.(string)
			}
			sort.Strings(strs) // parallel workers finish in any order
			return graph.State{"best": strs[0]}, nil
		}).
		AddNode("start", func(context.Context, graph.State) (any, error) { return nil, nil }).
		AddEdge(graph.Start, "start").
		AddConditionalEdges("start", func(_ context.Context, s graph.State) (any, error) {
			var sends []graph.Send
			for _, subj := range s["subjects"].([]string) {
				sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"subject": subj}})
			}
			return sends, nil
		}).
		AddEdge("worker", "pick_best").AddEdge("pick_best", graph.End)
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"subjects": []string{"chicken", "gopher", "robot"}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(out["jokes"].([]any)), "jokes generated in parallel; best:", strings.TrimSpace(out["best"].(string)))
}
