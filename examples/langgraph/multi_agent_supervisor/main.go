// multi_agent_supervisor: a supervisor routes work to specialist agents
// (port of multi_agent/multi-agent-collaboration + hierarchical teams).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func worker(role, answer string) *graph.Compiled {
	a, err := agents.Create(agents.Options{
		Model:        demo.Smart(func([]messages.Message) messages.Message { return messages.AI(answer) }),
		SystemPrompt: "You are the " + role + ".",
	})
	if err != nil {
		log.Fatal(err)
	}
	return a
}

func main() {
	team := map[string]*graph.Compiled{
		"researcher": worker("researcher", "Fact: Go 1.0 was released in 2012."),
		"writer":     worker("writer", "Go reached 1.0 in 2012, promising long-term compatibility."),
	}
	supervisor := demo.Smart(func(msgs []messages.Message) messages.Message {
		text := messages.Buffer(msgs)
		switch {
		case !strings.Contains(text, "Fact:"):
			return messages.AI("researcher")
		case !strings.Contains(text, "promising"):
			return messages.AI("writer")
		}
		return messages.AI("FINISH")
	})

	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages(), "next": channels.LastValue()}).
		AddNode("supervisor", func(ctx context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			out, err := supervisor.Generate(ctx, append([]messages.Message{messages.System("Pick researcher, writer or FINISH.")}, msgs...))
			next := strings.TrimSpace(out.Text())
			fmt.Println("supervisor ->", next)
			return graph.State{"next": next}, err
		})
	for name, w := range team {
		g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
			out, err := w.Invoke(ctx, graph.State{"messages": s["messages"]})
			if err != nil {
				return nil, err
			}
			msgs, _ := messages.Coerce(out["messages"])
			last := msgs[len(msgs)-1]
			last.ID, last.Name = "", name
			return graph.State{"messages": []messages.Message{messages.Human(name + ": " + last.Text())}}, nil
		})
		g.AddEdge(name, "supervisor")
	}
	g.AddEdge(graph.Start, "supervisor").
		AddConditionalEdges("supervisor", func(_ context.Context, s graph.State) (any, error) {
			if n := s["next"].(string); n != "FINISH" {
				return n, nil
			}
			return graph.End, nil
		})
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"messages": "Write one sentence about Go's first release."})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(demo.Last(out))
}
