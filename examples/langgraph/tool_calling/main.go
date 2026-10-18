// tool_calling: bind tools to a model, inspect the tool calls it makes, run
// them in parallel with ToolNode (port of tool-calling.ipynb).
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

type MathArgs struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
}

func main() {
	add := tools.New("add", "Add two numbers.", func(_ context.Context, a MathArgs) (any, error) { return a.A + a.B, nil })
	mul := tools.New("multiply", "Multiply two numbers.", func(_ context.Context, a MathArgs) (any, error) { return a.A * a.B, nil })
	fail := tools.New("divide", "Divide.", func(_ context.Context, a MathArgs) (any, error) {
		if a.B == 0 {
			return nil, tools.Errorf("division by zero")
		}
		return a.A / a.B, nil
	})
	ts := []tools.Tool{add, mul, fail}

	model := models.BindTools(demo.Smart(func([]messages.Message) messages.Message {
		return messages.AI("",
			messages.ToolCall{ID: "a", Name: "add", Args: map[string]any{"a": 3, "b": 4}},
			messages.ToolCall{ID: "b", Name: "multiply", Args: map[string]any{"a": 6, "b": 7}},
			messages.ToolCall{ID: "c", Name: "divide", Args: map[string]any{"a": 1, "b": 0}},
			messages.ToolCall{ID: "d", Name: "sqrt", Args: map[string]any{}})
	}), ts...)
	ai, err := model.Generate(context.Background(), []messages.Message{messages.Human("do some math")})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("model requested", len(ai.ToolCalls), "tool calls (run concurrently):")
	out, err := prebuilt.NewToolNode(ts...).Node()(context.Background(), graph.State{"messages": []messages.Message{ai}})
	if err != nil {
		log.Fatal(err)
	}
	for _, x := range out.([]any) {
		if st, ok := x.(graph.State); ok {
			for _, m := range st["messages"].([]messages.Message) {
				fmt.Printf("  %-9s status=%-7q -> %s\n", m.Name, m.Status, m.Text())
			}
		}
	}
}
