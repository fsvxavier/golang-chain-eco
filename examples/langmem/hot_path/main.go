// hot_path: an agent that curates its own long-term memory with the
// manage_memory and search_memory tools, isolated per user
// (port of LangMem's hot-path quickstart).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

func main() {
	ctx := context.Background()
	// Semantic search over an in-memory store (swap for sqlstore/redis/mongo,
	// and embeddings.Fake for a real embedder, in production).
	st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})
	ns := langmem.Namespace{"memories", "{langgraph_user_id}"}
	cfg := langmem.ToolConfig{Namespace: ns}

	// Offline stand-in for the LLM: saves what the user says to remember and
	// searches before answering questions.
	model := demo.Smart(func(msgs []messages.Message) messages.Message {
		last, _ := messages.Last(msgs)
		switch {
		case last.Role == messages.RoleTool && strings.HasPrefix(last.Text(), "created"):
			return messages.AI("Got it, I'll remember that.")
		case last.Role == messages.RoleTool:
			return messages.AI("From my memory: " + last.Text())
		case strings.Contains(last.Text(), "remember"):
			fact := strings.TrimSpace(strings.SplitN(last.Text(), "remember", 2)[1])
			return messages.AI("", messages.ToolCall{ID: "1", Name: "manage_memory", Args: map[string]any{"content": fact}})
		}
		return messages.AI("", messages.ToolCall{ID: "2", Name: "search_memory", Args: map[string]any{"query": last.Text()}})
	})
	agent, err := agents.Create(agents.Options{
		Model:        model,
		SystemPrompt: "You are a helpful assistant with long-term memory. Use your memory tools.",
		Tools:        []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
		Compile:      []graph.CompileOption{graph.WithStore(st)},
		Middleware:   []agents.Middleware{middleware.Compaction()},
	})
	if err != nil {
		log.Fatal(err)
	}

	ask := func(user, text string) {
		c := langmem.WithConfigurable(ctx, "langgraph_user_id", user)
		out, err := agent.Invoke(c, graph.State{"messages": text})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("[%s] %s\n  -> %s\n", user, text, demo.Last(out))
	}
	demo.Title("alice teaches the agent")
	ask("alice", "Please remember that I love sushi and hate cilantro")
	demo.Title("recall, scoped by user")
	ask("alice", "what food do I love?")
	ask("bob", "what food do I love?")
}
