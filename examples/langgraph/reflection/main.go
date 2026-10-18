// reflection: a generator and a critic loop until the critic is satisfied
// (port of reflection.ipynb).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	draft := 0
	generator := demo.Smart(func(msgs []messages.Message) messages.Message {
		draft++
		if draft == 1 {
			return messages.AI("Go is a language.")
		}
		return messages.AI("Go is a statically typed, compiled language with built-in concurrency.")
	})
	critic := demo.Smart(func(msgs []messages.Message) messages.Message {
		if strings.Contains(demo.LastHuman(msgs), "concurrency") {
			return messages.AI("APPROVED")
		}
		return messages.AI("Too short. Mention typing, compilation and concurrency.")
	})

	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages()}).
		AddNode("generate", func(ctx context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			out, err := generator.Generate(ctx, append([]messages.Message{messages.System("Write a 1-sentence description.")}, msgs...))
			fmt.Println("draft:", out.Text())
			return graph.State{"messages": []messages.Message{out}}, err
		}).
		AddNode("reflect", func(ctx context.Context, s graph.State) (any, error) {
			msgs, _ := messages.Coerce(s["messages"])
			last := msgs[len(msgs)-1]
			out, err := critic.Generate(ctx, []messages.Message{messages.System("Critique the text. Reply APPROVED if good."), messages.Human(last.Text())})
			fmt.Println("critic:", out.Text())
			// the critique goes back to the generator as a human message
			return graph.State{"messages": []messages.Message{messages.Human(out.Text())}}, err
		}).
		AddEdge(graph.Start, "generate").
		AddConditionalEdges("generate", func(_ context.Context, s graph.State) (any, error) {
			if msgs, _ := messages.Coerce(s["messages"]); len(msgs) > 6 {
				return graph.End, nil
			}
			return "reflect", nil
		}).
		AddConditionalEdges("reflect", func(_ context.Context, s graph.State) (any, error) {
			if strings.Contains(demo.Last(s), "APPROVED") {
				return graph.End, nil
			}
			return "generate", nil
		})
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	if _, err := app.Invoke(context.Background(), graph.State{"messages": "Describe Go"}); err != nil {
		log.Fatal(err)
	}
}
