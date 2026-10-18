// self_rag: retrieve, grade relevance, generate, then grade the answer for
// hallucination and usefulness, retrying generation or rewriting the query
// (port of rag/langgraph_self_rag.ipynb).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	ctx := context.Background()
	store, _ := vectorstores.FromDocuments(ctx, embeddings.Fake{Dim: 128}, []documents.Document{
		{PageContent: "Reflection lets an agent critique and improve its own output."},
		{PageContent: "Tool use lets agents call external APIs."},
	})
	retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 1})

	attempt := 0
	generator := demo.Smart(func(msgs []messages.Message) messages.Message {
		attempt++
		if attempt == 1 {
			return messages.AI("Reflection makes agents faster than light.") // hallucination
		}
		return messages.AI("Reflection lets an agent critique and improve its own output.")
	})
	judge := demo.Smart(func(msgs []messages.Message) messages.Message {
		p := demo.LastHuman(msgs)
		switch {
		case strings.Contains(p, "grounded") && strings.Contains(p, "faster than light"):
			return messages.AI("no")
		}
		return messages.AI("yes")
	})
	ask := func(ctx context.Context, q string) bool {
		out, _ := judge.Generate(ctx, []messages.Message{messages.Human(q)})
		return strings.HasPrefix(strings.ToLower(out.Text()), "yes")
	}

	g := graph.NewStateGraph(graph.Schema{
		"question": channels.LastValue(), "docs": channels.LastValue(),
		"answer": channels.LastValue(), "tries": graph.Sum(),
	}).
		AddNode("retrieve", func(ctx context.Context, s graph.State) (any, error) {
			d, err := retr.Invoke(ctx, s["question"].(string))
			return graph.State{"docs": d}, err
		}).
		AddNode("generate", func(ctx context.Context, s graph.State) (any, error) {
			out, err := generator.Generate(ctx, []messages.Message{messages.Human(retrievers.FormatDocuments(s["docs"].([]documents.Document)) + "\n" + s["question"].(string))})
			fmt.Println("generated:", out.Text())
			return graph.State{"answer": out.Text(), "tries": 1}, err
		}).
		AddEdge(graph.Start, "retrieve").AddEdge("retrieve", "generate").
		AddConditionalEdges("generate", func(ctx context.Context, s graph.State) (any, error) {
			grounded := ask(ctx, fmt.Sprintf("Is the answer grounded in the documents? yes/no\nDocs: %s\nAnswer: %s", retrievers.FormatDocuments(s["docs"].([]documents.Document)), s["answer"]))
			if !grounded && s["tries"].(int) < 3 {
				fmt.Println("grader: not grounded, regenerating")
				return "generate", nil
			}
			fmt.Println("grader: grounded and useful")
			return graph.End, nil
		})
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	out, err := app.Invoke(ctx, graph.State{"question": "What does reflection do for agents?"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("final:", out["answer"])
}
