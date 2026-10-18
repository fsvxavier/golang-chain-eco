// react_from_scratch: the ReAct loop wired by hand with a StateGraph
// (port of langgraph/examples/react-agent-from-scratch.ipynb).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

type CityArgs struct {
	City string `json:"city" description:"City name"`
}

func main() {
	weather := tools.New("get_weather", "Weather for a city.", func(_ context.Context, a CityArgs) (any, error) {
		return "It is sunny in " + a.City, nil
	})
	model := models.BindTools(demo.Smart(func(msgs []messages.Message) messages.Message {
		last := msgs[len(msgs)-1]
		if last.Role == messages.RoleTool {
			return messages.AI("Final answer: " + last.Text())
		}
		return messages.AI("", messages.ToolCall{ID: "1", Name: "get_weather", Args: map[string]any{"city": "San Francisco"}})
	}), weather)

	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages()}).
		AddNode("agent", func(ctx context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			out, err := model.Generate(ctx, msgs)
			return graph.State{"messages": []messages.Message{out}}, err
		}).
		AddNode("tools", prebuilt.NewToolNode(weather).Node()).
		AddEdge(graph.Start, "agent").
		AddConditionalEdges("agent", prebuilt.ToolsCondition).
		AddEdge("tools", "agent")
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(app.Mermaid())
	for ev, err := range app.StreamEvents(context.Background(), graph.State{"messages": "weather in SF?"}, graph.ModeUpdates) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%-6s %v\n", ev.Node, ev.Data)
	}
}
