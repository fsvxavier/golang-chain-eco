// extraction_retries: structured extraction that feeds validation errors
// back to the model and retries (port of extraction/retries.ipynb).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type Person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func (p Person) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if p.Age < 0 || p.Age > 130 {
		return fmt.Errorf("age %d is out of range 0..130", p.Age)
	}
	return nil
}

func main() {
	n := 0
	model := demo.Smart(func([]messages.Message) messages.Message {
		n++
		if n == 1 {
			return messages.AI(`{"name":"Ana","age":311}`) // typo: 31 -> 311
		}
		return messages.AI(`{"name":"Ana","age":31}`)
	})
	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages(), "person": channels.LastValue(), "attempts": graph.Sum()}).
		AddNode("extract", func(ctx context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			out, err := model.Generate(ctx, append([]messages.Message{messages.System(`Reply with JSON {"name":string,"age":int}.`)}, msgs...))
			return graph.State{"messages": []messages.Message{out}, "attempts": 1}, err
		}).
		AddNode("validate", func(_ context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			last := msgs[len(msgs)-1]
			var p Person
			err := json.Unmarshal([]byte(models.ExtractJSON(last.Text())), &p)
			if err == nil {
				err = p.Validate()
			}
			if err != nil {
				fmt.Println("validation error:", err)
				return graph.State{"messages": []messages.Message{messages.Human("Validation failed: " + err.Error() + ". Try again.")}}, nil
			}
			return graph.State{"person": p}, nil
		}).
		AddEdge(graph.Start, "extract").AddEdge("extract", "validate").
		AddConditionalEdges("validate", func(_ context.Context, s graph.State) (any, error) {
			if s["person"] == nil && s["attempts"].(int) < 3 {
				return "extract", nil
			}
			return graph.End, nil
		})
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(context.Background(), graph.State{"messages": "Ana is thirty-one years old."})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("extracted after %d attempts: %+v\n", out["attempts"], out["person"])
}
