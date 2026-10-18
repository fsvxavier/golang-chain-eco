// rag_chain: a classic retrieval-augmented chain — split, embed, retrieve,
// prompt, answer — built with Parallel/Assign runnables.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/outputparsers"
	"github.com/fsvxavier/golang-chain-eco/core/prompts"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/textsplitters"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
)

const corpus = `Go was designed at Google in 2007 by Griesemer, Pike and Thompson.

Goroutines are lightweight threads scheduled by the Go runtime.

Channels let goroutines communicate safely without sharing memory.`

func main() {
	ctx := context.Background()
	var docs []documents.Document
	for _, c := range textsplitters.NewRecursive(80, 0).SplitText(corpus) {
		docs = append(docs, documents.Document{PageContent: c})
	}
	store, err := vectorstores.FromDocuments(ctx, embeddings.Fake{Dim: 128}, docs)
	if err != nil {
		log.Fatal(err)
	}
	retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 2})

	prompt := prompts.Chat(
		[2]string{"system", "Answer using only this context:\n{context}"},
		[2]string{"human", "{question}"},
	)
	model := models.AsRunnable(demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI("(answer grounded in) " + strings.ReplaceAll(strings.SplitN(msgs[0].Text(), "\n", 3)[1], "\n", " "))
	}))

	// {"question": q} -> add {"context": retrieved docs} -> prompt -> model -> string
	withCtx := runnables.Assign(map[string]runnables.Runnable[map[string]any, any]{
		"context": runnables.Lambda("retrieve", func(ctx context.Context, in map[string]any) (any, error) {
			d, err := retr.Invoke(ctx, in["question"].(string))
			return retrievers.FormatDocuments(d), err
		}),
	})
	// map -> map(+context) -> []Message -> Message, then parse to string
	answerer := runnables.Pipe3[map[string]any, map[string]any, []messages.Message, messages.Message](withCtx, prompt, model)
	chain := runnables.Pipe[map[string]any, messages.Message, string](answerer, outputparsers.Str{})

	out, err := chain.Invoke(ctx, map[string]any{"question": "What are goroutines?"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out)
}
