// code_assistant: generate code, check it (here: with a Go-side validator),
// reflect on errors and retry (port of code_assistant/langgraph_code_assistant.ipynb).
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

// check is a stand-in for compiling/running the code: it must define Add.
func check(code string) error {
	if !strings.Contains(code, "func Add(") {
		return fmt.Errorf("undefined: Add")
	}
	if strings.Contains(code, "a - b") {
		return fmt.Errorf("Add(2,3) = -1, want 5")
	}
	return nil
}

func main() {
	n := 0
	coder := demo.Smart(func(msgs []messages.Message) messages.Message {
		n++
		switch n {
		case 1:
			return messages.AI("func Sum(a, b int) int { return a + b }")
		case 2:
			return messages.AI("func Add(a, b int) int { return a - b }")
		}
		return messages.AI("func Add(a, b int) int { return a + b }")
	})
	g := graph.NewStateGraph(graph.Schema{
		"messages": graph.Messages(), "code": channels.LastValue(), "error": channels.LastValue(), "iterations": graph.Sum(),
	}).
		AddNode("generate", func(ctx context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			out, err := coder.Generate(ctx, append([]messages.Message{messages.System("Write Go code. Reply with code only.")}, msgs...))
			fmt.Printf("attempt %d: %s\n", s["iterations"].(int)+1, out.Text())
			return graph.State{"code": out.Text(), "iterations": 1, "messages": []messages.Message{out}}, err
		}).
		AddNode("check", func(_ context.Context, s graph.State) (any, error) {
			if err := check(s["code"].(string)); err != nil {
				fmt.Println("  check failed:", err)
				return graph.State{"error": err.Error(), "messages": []messages.Message{messages.Human("Your code failed: " + err.Error() + ". Fix it.")}}, nil
			}
			fmt.Println("  check passed")
			return graph.State{"error": ""}, nil
		}).
		AddEdge(graph.Start, "generate").AddEdge("generate", "check").
		AddConditionalEdges("check", func(_ context.Context, s graph.State) (any, error) {
			if s["error"] != "" && s["iterations"].(int) < 4 {
				return "generate", nil
			}
			return graph.End, nil
		})
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"messages": "Write func Add(a, b int) int"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("final code:", out["code"])
}
