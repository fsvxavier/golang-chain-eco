// hitl: pause a tool call for human approval, then resume.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type wireArgs struct {
	Amount int `json:"amount"`
}

func main() {
	wire := tools.New("wire_money", "Send money.", func(_ context.Context, a wireArgs) (any, error) {
		return fmt.Sprintf("sent $%d", a.Amount), nil
	})
	agent, err := agents.Create(agents.Options{
		Model: demo.Model(
			messages.AI("", messages.ToolCall{ID: "w1", Name: "wire_money", Args: map[string]any{"amount": 500}}),
			messages.AI("Done."),
		),
		Tools: []tools.Tool{wire},
		Middleware: []agents.Middleware{middleware.HumanInTheLoop{
			Tools: map[string]middleware.ToolPolicy{"wire_money": {}},
		}, middleware.Compaction()},
		Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())},
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("demo"))

	out, err := agent.Invoke(ctx, graph.State{"messages": "Wire $500 to Bob"})
	if err != nil {
		log.Fatal(err)
	}
	for _, i := range graph.Interrupts(out) {
		fmt.Printf("paused at %s: %+v\n", i.Node, i.Value)
	}
	// a human reviews the request and approves
	out, err = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
		Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	}})
	if err != nil {
		log.Fatal(err)
	}
	ms := out["messages"].([]messages.Message)
	fmt.Println("final:", ms[len(ms)-1].Content)
}
