// rag: retrieval-augmented answers over a small fictional knowledge base.
// It indexes five embedded documents (pt-BR, about the Zefiro X200 router),
// compares plain vector search with hybrid search (vector + BM25), answers
// with [n] citations, and shows that a second Index call skips unchanged
// sources and that editing one document removes and re-adds only its chunks.
// Runs offline: fake embeddings and a scripted model via demo.Smart.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

const question = "Zefiro X200 com garantia e firmware: erro KRX-9921"

// corpus returns the five embedded documents; each one is its own source.
func corpus() []documents.Document {
	return []documents.Document{
		{ID: "manual", PageContent: "Para configurar o roteador Zefiro X200, conecte o cabo WAN à porta azul, ligue a energia e acesse o painel em 192.168.0.1 com o usuário admin.", Metadata: map[string]any{"source": "manual.md"}},
		{ID: "garantia", PageContent: "A garantia do Zefiro X200 é de dois anos para o hardware e de noventa dias para firmware de terceiros.", Metadata: map[string]any{"source": "garantia.md"}},
		{ID: "wifi", PageContent: "O Zefiro X200 transmite Wi-Fi 6 nas bandas de 2,4 GHz e 5 GHz e forma malha com até quatro nós para cobrir casas grandes.", Metadata: map[string]any{"source": "wifi.md"}},
		{ID: "firmware", PageContent: "Para atualizar o firmware, baixe a versão mais recente no portal, aguarde o roteador reiniciar e nunca desligue a energia durante a gravação.", Metadata: map[string]any{"source": "firmware.md"}},
		{ID: "erro-krx", PageContent: "Luzes piscando em âmbar indicam falha de sincronização. O código KRX-9921 aparece quando a tabela de rotas local está corrompida; mantenha o botão de reset pressionado por dez segundos.", Metadata: map[string]any{"source": "erros.md"}},
	}
}

// reply plays the model offline: it cites the context line that shares the
// most words with the question.
func reply(msgs []messages.Message) messages.Message {
	q := strings.ToLower(demo.LastHuman(msgs))
	best, bestN, bestScore := "", 0, 0
	for _, line := range strings.Split(msgs[0].Text(), "\n") {
		var n int
		if _, err := fmt.Sscanf(line, "[%d]", &n); err != nil {
			continue
		}
		text := line[strings.Index(line, "] ")+2:]
		score := 0
		for _, w := range strings.Fields(q) {
			w = strings.Trim(w, "?.,;:!")
			if strings.Contains(w, "-") && strings.Contains(strings.ToLower(text), strings.ToLower(w)) {
				score += 100 // a code such as KRX-9921 decides the citation
			} else if len(w) >= 4 && strings.Contains(strings.ToLower(text), w) {
				score++
			}
		}
		if score > bestScore {
			best, bestN, bestScore = text, n, score
		}
	}
	if bestScore == 0 {
		return messages.AI("Não sei: o contexto não traz a resposta.")
	}
	return messages.AI(fmt.Sprintf("%s [%d]", best, bestN))
}

func statsLine(s rag.IndexStats) string {
	return fmt.Sprintf("Added=%d Skipped=%d Removed=%d", s.Added, s.Skipped, s.Removed)
}

func printSources(srcs []knowledge.Source) {
	for i, s := range srcs {
		fmt.Printf("  [%d] %s (%v) %s\n", i+1, s.ID, s.Metadata["source"], s.Content)
	}
}

func main() {
	ctx := context.Background()
	store := vectorstores.NewMemory(embeddings.Fake{Dim: 512})
	model := demo.Smart(reply)

	docs := corpus()
	loader := documents.LoaderFunc(func(context.Context) ([]documents.Document, error) {
		return append([]documents.Document(nil), docs...), nil
	})
	indexer := rag.NewIndexer(store, nil)
	pipe := &rag.Pipeline{Store: store, Model: model, K: 3}

	demo.Title("index")
	stats, err := indexer.Index(ctx, loader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("first run:", statsLine(stats))
	pipe.Docs = indexer.Documents()

	demo.Title("vector search only (Hybrid false)")
	fmt.Printf("question: %s\n", question)
	srcs, err := pipe.Retrieve(ctx, knowledge.Question{Text: question})
	if err != nil {
		log.Fatal(err)
	}
	printSources(srcs)

	demo.Title("hybrid search (Hybrid true)")
	pipe.Hybrid = true
	ans, err := pipe.Answer(ctx, knowledge.Question{Text: question})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("answer:", ans.Text)
	fmt.Println("strategy:", ans.Strategy)
	for _, st := range ans.Trace {
		fmt.Printf("  trace: %s %s\n", st.Name, st.Detail)
	}
	fmt.Println("sources:")
	printSources(ans.Sources)

	demo.Title("re-index unchanged corpus")
	stats, err = indexer.Index(ctx, loader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("second run:", statsLine(stats))

	demo.Title("re-index after editing one document")
	docs[1].PageContent = "A garantia do Zefiro X200 é de três anos para o hardware e de noventa dias para firmware de terceiros."
	stats, err = indexer.Index(ctx, loader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("third run:", statsLine(stats))
}
