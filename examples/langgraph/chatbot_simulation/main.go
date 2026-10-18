// chatbot_simulation: a simulated user talks to the assistant and an
// evaluator scores the exchange (port of
// chatbot-simulation-evaluation/agent-simulation-evaluation.ipynb).
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

func main() {
	assistant := demo.Smart(func(msgs []messages.Message) messages.Message {
		if strings.Contains(strings.ToLower(demo.LastHuman(msgs)), "refund") {
			return messages.AI("I can refund orders within 30 days. Please share the order number.")
		}
		return messages.AI("How can I help you today?")
	})
	user := demo.Smart(func(msgs []messages.Message) messages.Message {
		switch {
		case len(msgs) <= 2:
			return messages.AI("Hi, I want a refund for my order.")
		case len(msgs) <= 4:
			return messages.AI("Order 4521.")
		}
		return messages.AI("FINISHED")
	})
	grader := demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI(`{"score":5,"comment":"Asked for the order number and stated the refund policy."}`)
	})

	flip := func(ms []messages.Message) []messages.Message { // swap roles for the other party
		out := make([]messages.Message, len(ms))
		for i, m := range ms {
			m.ID = ""
			if m.Role == messages.RoleAI {
				m.Role = messages.RoleHuman
			} else if m.Role == messages.RoleHuman {
				m.Role = messages.RoleAI
			}
			out[i] = m
		}
		return out
	}
	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages(), "verdict": channels.LastValue()}).
		AddNode("simulated_user", func(ctx context.Context, s graph.State) (any, error) {
			ms, _ := messages.Coerce(s["messages"])
			out, err := user.Generate(ctx, flip(ms))
			fmt.Println("user     :", out.Text())
			return graph.State{"messages": []messages.Message{messages.Human(out.Text())}}, err
		}).
		AddNode("assistant", func(ctx context.Context, s graph.State) (any, error) {
			ms, _ := messages.Coerce(s["messages"])
			out, err := assistant.Generate(ctx, ms)
			fmt.Println("assistant:", out.Text())
			return graph.State{"messages": []messages.Message{out}}, err
		}).
		AddNode("evaluate", func(ctx context.Context, s graph.State) (any, error) {
			ms, _ := messages.Coerce(s["messages"])
			out, err := grader.Generate(ctx, []messages.Message{messages.Human("Score this support chat 1-5 as JSON:\n" + messages.Buffer(ms))})
			return graph.State{"verdict": out.Text()}, err
		}).
		AddEdge(graph.Start, "assistant").
		AddConditionalEdges("assistant", func(_ context.Context, s graph.State) (any, error) {
			if ms, _ := messages.Coerce(s["messages"]); len(ms) > 6 {
				return "evaluate", nil
			}
			return "simulated_user", nil
		}).
		AddConditionalEdges("simulated_user", func(_ context.Context, s graph.State) (any, error) {
			if strings.Contains(demo.Last(s), "FINISHED") {
				return "evaluate", nil
			}
			return "assistant", nil
		}).AddEdge("evaluate", graph.End)
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"messages": "Hi"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("verdict  :", out["verdict"])
}
