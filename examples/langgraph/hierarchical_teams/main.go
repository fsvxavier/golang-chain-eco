// hierarchical_teams: a top supervisor delegates to team subgraphs, each with
// its own supervisor and workers (port of multi_agent/hierarchical_agent_teams.ipynb).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// worker appends its contribution to the shared message list.
func worker(name, text string) graph.NodeFunc {
	return func(context.Context, graph.State) (any, error) {
		fmt.Printf("    %s works\n", name)
		return graph.State{"messages": []messages.Message{messages.Human(name + ": " + text)}}, nil
	}
}

// team builds a supervisor graph over workers executed in order.
func team(name string, workers ...[2]string) *graph.Compiled {
	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages(), "next": channels.LastValue()})
	g.AddNode("supervisor", func(_ context.Context, s graph.State) (any, error) {
		ms, _ := messages.Coerce(s["messages"])
		txt := messages.Buffer(ms)
		for _, w := range workers {
			if !strings.Contains(txt, w[0]+":") {
				return graph.State{"next": w[0]}, nil
			}
		}
		return graph.State{"next": "FINISH"}, nil
	}).AddEdge(graph.Start, "supervisor")
	for _, w := range workers {
		g.AddNode(w[0], worker(w[0], w[1])).AddEdge(w[0], "supervisor")
	}
	g.AddConditionalEdges("supervisor", func(_ context.Context, s graph.State) (any, error) {
		if n := s["next"].(string); n != "FINISH" {
			return n, nil
		}
		return graph.End, nil
	})
	app, err := g.Compile(graph.WithName(name))
	if err != nil {
		log.Fatal(err)
	}
	return app
}

func main() {
	research := team("research_team", [2]string{"searcher", "found 3 sources on Go"}, [2]string{"scraper", "extracted key facts"})
	writing := team("writing_team", [2]string{"drafter", "wrote the draft"}, [2]string{"editor", "polished the draft"})
	teams := map[string]*graph.Compiled{"research_team": research, "writing_team": writing}

	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages(), "next": channels.LastValue()}).
		AddNode("top_supervisor", func(_ context.Context, s graph.State) (any, error) {
			ms, _ := messages.Coerce(s["messages"])
			txt := messages.Buffer(ms)
			switch {
			case !strings.Contains(txt, "searcher:"):
				return graph.State{"next": "research_team"}, nil
			case !strings.Contains(txt, "drafter:"):
				return graph.State{"next": "writing_team"}, nil
			}
			return graph.State{"next": "FINISH"}, nil
		})
	for name, sub := range teams {
		g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
			fmt.Println("  entering", name)
			out, err := sub.Invoke(ctx, graph.State{"messages": s["messages"]})
			if err != nil {
				return nil, err
			}
			ms, _ := messages.Coerce(out["messages"])
			prev, _ := messages.Coerce(s["messages"])
			return graph.State{"messages": ms[len(prev):]}, nil // only what the team added
		}).AddEdge(name, "top_supervisor")
	}
	g.AddEdge(graph.Start, "top_supervisor").
		AddConditionalEdges("top_supervisor", func(_ context.Context, s graph.State) (any, error) {
			if n := s["next"].(string); n != "FINISH" {
				return n, nil
			}
			return graph.End, nil
		})
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"messages": "Write a short article about Go."})
	if err != nil {
		log.Fatal(err)
	}
	ms, _ := messages.Coerce(out["messages"])
	for _, m := range ms {
		fmt.Println("-", m.Text())
	}
}
