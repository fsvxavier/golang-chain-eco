// mcp_server: serves knowledge.Toolkit over MCP. With -demo it feeds one
// tools/list and one tools/call request through the server in-process and
// prints the replies (offline check). Without it, the server speaks MCP on
// stdio, for editors such as VS Code or Claude Desktop.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

var chunks = []documents.Document{
	{ID: "chunk-1", PageContent: "Dr. Silva prescreveu Varfarina ao paciente João.", Metadata: map[string]any{"fonte": "receita.md"}},
	{ID: "chunk-2", PageContent: "Varfarina interage com Aspirina.", Metadata: map[string]any{"fonte": "bula.md"}},
}

func main() {
	demoMode := flag.Bool("demo", false, "print replies to one tools/list and one tools/call instead of serving stdio")
	flag.Parse()
	ctx := context.Background()

	vs := vectorstores.NewMemory(embeddings.Fake{Dim: 128})
	if _, err := vs.AddDocuments(ctx, chunks); err != nil {
		log.Fatal(err)
	}
	rg := &rag.Pipeline{Store: vs, Model: demo.Model(messages.AI("A Varfarina foi prescrita a João. [1]")), K: 2}
	kb := knowledge.Toolkit{
		RAG:    rg,
		Router: &knowledge.Router{RAG: rg},
		Ingest: func(ctx context.Context, docs []documents.Document) error {
			_, err := vs.AddDocuments(ctx, docs)
			return err
		},
	}
	srv := kb.MCPServer(nil)

	if !*demoMode {
		if err := srv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"rag_answer","arguments":{"question":"Qual remédio foi prescrito a João?"}}}`,
	}
	for _, req := range requests {
		fmt.Printf("-> %s\n<- %s\n\n", req, srv.Handle(ctx, []byte(req)))
	}
}
