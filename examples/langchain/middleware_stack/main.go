// middleware_stack: several agent middlewares composed together — PII
// redaction, retry, model fallback, call limits, summarization (patterns from
// langchain_v1 agents/middleware).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type LookupArgs struct {
	Email string `json:"email"`
}

func main() {
	lookup := tools.New("lookup", "Look up a customer.", func(_ context.Context, a LookupArgs) (any, error) {
		return "customer found for " + a.Email, nil
	})
	calls := 0
	flaky := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		calls++
		return messages.Message{}, errors.New("503 primary model unavailable")
	}}
	backup := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		seen := strings.Contains(messages.Buffer(msgs), "[REDACTED_EMAIL]")
		return messages.AI(fmt.Sprintf("backup model answered; email redacted before reaching it: %v", seen)), nil
	}}

	agent, err := agents.Create(agents.Options{
		Model: flaky, Tools: []tools.Tool{lookup},
		Middleware: []agents.Middleware{
			middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail}},
			middleware.ModelRetry{Options: runnables.RetryOptions{MaxAttempts: 2, InitialInterval: 1}},
			middleware.ModelFallback{Fallbacks: []models.ChatModel{backup}},
			middleware.ModelCallLimit{RunLimit: 5},
			middleware.ToolCallLimit{RunLimit: 3},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(context.Background(), graph.State{"messages": "Find alice@example.com please"})
	if err != nil {
		log.Fatal(err)
	}
	ms := out["messages"].([]messages.Message)
	fmt.Println("stored user message:", ms[0].Text())
	fmt.Println("primary model attempts:", calls)
	fmt.Println(ms[len(ms)-1].Text())
}
