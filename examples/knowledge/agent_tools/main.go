// agent_tools: an agents.Create agent that answers from the knowledge base
// through knowledge.Toolkit. The scripted model calls rag_answer, then replies
// with the cited answer. Runs offline.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

const question = "Qual remédio foi prescrito a João?"

var chunks = []documents.Document{
	{ID: "chunk-1", PageContent: "Dr. Silva prescreveu Varfarina ao paciente João.", Metadata: map[string]any{"fonte": "receita.md"}},
	{ID: "chunk-2", PageContent: "Varfarina interage com Aspirina.", Metadata: map[string]any{"fonte": "bula.md"}},
}

func main() {
	ctx := context.Background()

	// The RAG pipeline has its own model call: a scripted citation here.
	vs := vectorstores.NewMemory(embeddings.Fake{Dim: 128})
	_, err := vs.AddDocuments(ctx, chunks)
	if err != nil {
		log.Fatal(err)
	}
	rg := &rag.Pipeline{Store: vs, Model: demo.Model(messages.AI("A Varfarina foi prescrita a João. [1]")), K: 2}
	kb := knowledge.Toolkit{RAG: rg, Router: &knowledge.Router{RAG: rg, Profile: knowledge.Profile{}}}

	agent, err := agents.Create(agents.Options{
		Model: demo.Model(
			messages.AI("", messages.ToolCall{ID: "call-1", Name: "rag_answer", Args: map[string]any{"question": question}}),
			messages.AI("A Varfarina foi prescrita a João [1]."),
		),
		Tools:        kb.All(),
		SystemPrompt: "Responda com base na base de conhecimento e cite as fontes.",
	})
	if err != nil {
		log.Fatal(err)
	}
	st, err := agent.Invoke(ctx, graph.State{"messages": question})
	if err != nil {
		log.Fatal(err)
	}
	msgs := st["messages"].([]messages.Message)
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			fmt.Printf("tool call: %s %v\n", c.Name, c.Args)
		}
	}
	fmt.Printf("tool result: %s\n", msgs[len(msgs)-2].Content)
	fmt.Printf("resposta: %s\n", msgs[len(msgs)-1].Content)
}
