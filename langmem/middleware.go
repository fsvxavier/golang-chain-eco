package langmem

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Middleware gives an agents.Create agent long-term memory with the three
// LangMem patterns, each optional:
//
//   - Inject: before each model call, search the store with the latest user
//     message and put the hits in the system prompt (the "prompt function"
//     of the LangMem quickstarts).
//   - ExposeTools: expose manage_memory and search_memory so the agent curates its
//     own memory ("hot path").
//   - Manager: after the agent finishes, extract memories from the
//     conversation, either in the background (Executor) or right away.
type Middleware struct {
	// Namespace may hold {variables}; empty means DefaultNamespace.
	Namespace Namespace
	// Store defaults to the store attached to the graph.
	Store store.Store
	// Inject memories into the system prompt (top Limit, default 5).
	Inject bool
	Limit  int
	// ExposeTools adds manage_memory and search_memory.
	ExposeTools bool
	// Manager forms memories after each run. Its namespace should match.
	Manager *StoreManager
	// Executor, when set, defers Manager to the background after Delay.
	Executor *ReflectionExecutor
	Delay    time.Duration
}

func (Middleware) Name() string { return "langmem" }

func (m Middleware) toolConfig() ToolConfig {
	return ToolConfig{Namespace: m.Namespace, Store: m.Store}
}

// Tools implements agents.WithTools.
func (m Middleware) Tools() []tools.Tool {
	if !m.ExposeTools {
		return nil
	}
	return []tools.Tool{ManageMemory(m.toolConfig()), SearchMemory(m.toolConfig())}
}

// WrapModelCall injects relevant memories into the system prompt.
func (m Middleware) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	if !m.Inject {
		return next(ctx, req)
	}
	last, ok := messages.LastOfRole(req.Messages, messages.RoleHuman)
	if !ok {
		return next(ctx, req)
	}
	cfg := m.toolConfig()
	st, ns, err := cfg.resolve(ctx)
	if err != nil {
		return messages.Message{}, err
	}
	hits, err := Query(ctx, st, ns, last.Text(), nil, cmpInt(m.Limit, 5))
	if err != nil {
		return messages.Message{}, err
	}
	if len(hits) == 0 {
		return next(ctx, req)
	}
	var b strings.Builder
	b.WriteString(req.System + "\n\n## Memories\n<memories>\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "- %s\n", Text(normalize(h.Value["content"])))
	}
	b.WriteString("</memories>")
	r := *req
	r.System = strings.TrimSpace(b.String())
	return next(ctx, &r)
}

func cmpInt(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

// AfterAgent forms memories from the finished conversation.
func (m Middleware) AfterAgent(ctx context.Context, st graph.State) (any, error) {
	if m.Manager == nil {
		return nil, nil
	}
	msgs, err := messages.Coerce(st["messages"])
	if err != nil {
		return nil, err
	}
	if m.Executor != nil {
		_, err := m.Executor.Submit(ctx, Payload{Messages: msgs}, SubmitOptions{After: m.Delay})
		return nil, err
	}
	_, err = m.Manager.Process(ctx, msgs)
	return nil, err
}
