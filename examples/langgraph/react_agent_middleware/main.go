// react_agent: a tool-calling agent built with agents.Create plus middleware.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type weatherArgs struct {
	City string `json:"city" description:"City name"`
}

func main() {
	weather := tools.New("get_weather", "Current weather for a city.",
		func(_ context.Context, a weatherArgs) (any, error) { return "Sunny, 24C in " + a.City, nil })

	agent, err := agents.Create(agents.Options{
		Model: demo.Model(
			messages.AI("", messages.ToolCall{ID: "c1", Name: "get_weather", Args: map[string]any{"city": "Lisbon"}}),
			messages.AI("It is sunny and 24C in Lisbon."),
		),
		Tools:        []tools.Tool{weather},
		SystemPrompt: "You are a helpful weather assistant.",
		Middleware:   []agents.Middleware{middleware.ModelCallLimit{RunLimit: 5}},
	})
	if err != nil {
		log.Fatal(err)
	}
	// stream tokens/updates: here, node-level updates
	for ev, err := range agent.StreamEvents(context.Background(),
		graph.State{"messages": "Weather in Lisbon?"}, graph.ModeUpdates) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("[%s] %v\n", ev.Node, ev.Data)
	}
}
