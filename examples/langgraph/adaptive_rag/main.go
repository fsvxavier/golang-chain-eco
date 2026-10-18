// adaptive_rag: a router decides between the vector store and web search per
// question (port of rag/langgraph_adaptive_rag.ipynb).
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
		{PageContent: "LLM agents combine planning, memory and tool use."},
		{PageContent: "Prompt injection is an attack on LLM applications."},
	})
	retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 1})
	web := retrievers.Func(func(_ context.Context, q string) ([]documents.Document, error) {
		return []documents.Document{{PageContent: "(web) latest news about: " + q}}, nil
	})
	router := demo.Smart(func(msgs []messages.Message) messages.Message {
		if strings.Contains(strings.ToLower(demo.LastHuman(msgs)), "agent") || strings.Contains(strings.ToLower(demo.LastHuman(msgs)), "injection") {
			return messages.AI("vectorstore")
		}
		return messages.AI("web_search")
	})
	llm := demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI("Answer from: " + strings.SplitN(demo.LastHuman(msgs), "\n", 2)[0])
	})
	fetch := func(r retrievers.Retriever) graph.NodeFunc {
		return func(ctx context.Context, s graph.State) (any, error) {
			d, err := r.Invoke(ctx, s["question"].(string))
			return graph.State{"docs": d}, err
		}
	}
	g := graph.NewStateGraph(graph.Schema{"question": channels.LastValue(), "docs": channels.LastValue(), "answer": channels.LastValue()}).
		AddNode("vectorstore", fetch(retr)).AddNode("web_search", fetch(web)).
		AddNode("generate", func(ctx context.Context, s graph.State) (any, error) {
			out, err := llm.Generate(ctx, []messages.Message{messages.Human(retrievers.FormatDocuments(s["docs"].([]documents.Document)) + "\n" + s["question"].(string))})
			return graph.State{"answer": out.Text()}, err
		}).
		AddConditionalEdges(graph.Start, func(ctx context.Context, s graph.State) (any, error) {
			out, err := router.Generate(ctx, []messages.Message{messages.System("Route to vectorstore (agents, prompt attacks) or web_search."), messages.Human(s["question"].(string))})
			fmt.Printf("route(%q) -> %s\n", s["question"], out.Text())
			return strings.TrimSpace(out.Text()), err
		}).
		AddEdge("vectorstore", "generate").AddEdge("web_search", "generate").AddEdge("generate", graph.End)
	app, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	for _, q := range []string{"What are LLM agents made of?", "Who won the match yesterday?"} {
		out, err := app.Invoke(ctx, graph.State{"question": q})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(" ->", out["answer"])
	}
}
