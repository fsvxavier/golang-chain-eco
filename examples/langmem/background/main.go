// background: memories formed "subconsciously" after the conversation goes
// quiet. ReflectionExecutor debounces: three quick turns trigger ONE
// extraction over the full conversation (port of LangMem's background
// quickstart and delayed-processing guide).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

func main() {
	var extractions atomic.Int32
	// Offline extractor: one memory per "I ..." sentence in the session.
	model := demo.Smart(func(msgs []messages.Message) messages.Message {
		extractions.Add(1)
		var calls []messages.ToolCall
		for _, line := range strings.Split(msgs[1].Text(), "\n") {
			if strings.HasPrefix(line, "Human: I ") {
				calls = append(calls, messages.ToolCall{ID: fmt.Sprint(len(calls)), Name: "Memory",
					Args: map[string]any{"content": "User " + strings.TrimPrefix(line, "Human: I ")}})
			}
		}
		return messages.AI("", calls...)
	})

	st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})
	manager := langmem.NewStoreManager(model, langmem.Namespace{"memories", "{langgraph_user_id}"}, langmem.WithStore(st))
	executor := &langmem.ReflectionExecutor{Manager: manager}

	ctx := langmem.WithConfigurable(context.Background(), "langgraph_user_id", "alice", "thread_id", "t1")
	turns := []string{"I like sushi", "I work with Go", "I live in Lisbon"}
	var convo []messages.Message
	var last *langmem.Future
	demo.Title("three quick turns, 300ms debounce")
	for _, t := range turns {
		convo = append(convo, messages.Human(t), messages.AI("noted"))
		var err error
		// Same thread: each submit cancels the pending run and reschedules.
		last, err = executor.Submit(ctx, langmem.Payload{Messages: convo}, langmem.SubmitOptions{After: 300 * time.Millisecond})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("submitted turn:", t)
		time.Sleep(50 * time.Millisecond)
	}
	puts, err := last.Wait()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("extractions run: %d, memories written: %d\n", extractions.Load(), len(puts))

	demo.Title("search the memories (semantic)")
	hits, _ := executor.Search(ctx, "work with Go", nil, 2)
	for _, h := range hits {
		fmt.Printf("%.2f  %s\n", h.Score, langmem.Text(h.Value["content"]))
	}
	executor.Shutdown(true, false) // flush anything still pending before exit
}
