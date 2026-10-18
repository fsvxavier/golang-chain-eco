// corrective_rag: retrieve, grade documents, fall back to "web search" when
// they are irrelevant, then answer (port of rag/langgraph_crag.ipynb).
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
	store, err := vectorstores.FromDocuments(ctx, embeddings.Fake{Dim: 128}, []documents.Document{
		{PageContent: "Agent memory stores observations and reflections."},
		{PageContent: "Prompt engineering shapes model behaviour with instructions."},
	})
	if err != nil {
		log.Fatal(err)
	}
	retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 2})

	grader := demo.Smart(func(msgs []messages.Message) messages.Message {
		if strings.Contains(strings.ToLower(demo.LastHuman(msgs)), "memory") && strings.Contains(demo.LastHuman(msgs), "Question: how does agent memory work") {
			return messages.AI("yes")
		}
		return messages.AI("no")
	})
	llm := demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI("Answer based on context: " + strings.SplitN(demo.LastHuman(msgs), "\n", 2)[0])
	})

	g := graph.NewStateGraph(graph.Schema{
		"question": channels.LastValue(), "docs": channels.LastValue(),
		"web": channels.LastValue(), "answer": channels.LastValue(),
	}).
		AddNode("retrieve", func(ctx context.Context, s graph.State) (any, error) {
			d, err := retr.Invoke(ctx, s["question"].(string))
			return graph.State{"docs": d}, err
		}).
		AddNode("grade", func(ctx context.Context, s graph.State) (any, error) {
			var kept []documents.Document
			for _, d := range s["docs"].([]documents.Document) {
				out, err := grader.Generate(ctx, []messages.Message{messages.Human(fmt.Sprintf("Document: %s\nQuestion: %s\nRelevant? yes/no", d.PageContent, s["question"]))})
				if err != nil {
					return nil, err
				}
				if strings.HasPrefix(strings.ToLower(out.Text()), "yes") {
					kept = append(kept, d)
				}
			}
			fmt.Printf("graded: %d relevant document(s)\n", len(kept))
			return graph.State{"docs": kept, "web": len(kept) == 0}, nil
		}).
		AddNode("web_search", func(_ context.Context, s graph.State) (any, error) {
			fmt.Println("no relevant docs: searching the web")
			return graph.State{"docs": []documents.Document{{PageContent: "Web result: " + s["question"].(string)}}}, nil
		}).
		AddNode("generate", func(ctx context.Context, s graph.State) (any, error) {
			out, err := llm.Generate(ctx, []messages.Message{messages.Human(retrievers.FormatDocuments(s["docs"].([]documents.Document)) + "\n" + s["question"].(string))})
			return graph.State{"answer": out.Text()}, err
		}).
		AddEdge(graph.Start, "retrieve").AddEdge("retrieve", "grade").
		AddConditionalEdges("grade", func(_ context.Context, s graph.State) (any, error) {
			if s["web"] == true {
				return "web_search", nil
			}
			return "generate", nil
		}).
		AddEdge("web_search", "generate").AddEdge("generate", graph.End)
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	for _, q := range []string{"how does agent memory work", "who won the 2024 election"} {
		out, err := app.Invoke(ctx, graph.State{"question": q})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Q: %s\nA: %v\n\n", q, out["answer"])
	}
}
