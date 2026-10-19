// agent_with_context: an agent that uses the ciam tools instead of reading
// files blindly. Before it implements anything it calls ciam_decision_context
// and receives the code AND the ADR that constrains it, so the change respects
// a decision nobody mentioned in the request.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/ciam"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	ctx := context.Background()
	root := filepath.Join(must(os.MkdirTemp("", "ciam-agent-")), "shop")
	defer os.RemoveAll(filepath.Dir(root))
	_ = os.MkdirAll(filepath.Join(root, "billing"), 0o755)
	must0(os.WriteFile(filepath.Join(root, "billing", "invoice.go"), []byte("package billing\n\n// Invoice is what we charge.\ntype Invoice struct {\n\tTotalCents int64\n}\n"), 0o644))
	docs := ciam.NewDocs(root)
	adr := must(docs.New(ciam.ADR, "Money is integer cents", ""))
	must0(os.WriteFile(adr, []byte("---\nstatus: accepted\ntitle: \"Money is integer cents\"\n---\n\n# ADR-001\n\nNever use float for money: rounding errors broke invoice totals. Use int64 cents.\n"), 0o644))

	mgr := ciam.New(embeddings.Fake{}, nil)
	must(mgr.Index(ctx, root, ciam.IndexOptions{}))
	kit := ciam.Toolkit{M: mgr, Project: root}

	// the "model": asks the context manager first, then answers using what it learned
	calls := 0
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		calls++
		if calls == 1 {
			return messages.AI("Before changing billing I will check the project's decisions.",
				messages.ToolCall{ID: "1", Name: "ciam_decision_context", Args: map[string]any{"query": "add a discount to an invoice total"}}), nil
		}
		ctxText := ""
		for _, m := range msgs {
			if m.ToolCallID == "1" {
				ctxText = m.Text()
			}
		}
		if strings.Contains(ctxText, "Never use float for money") {
			return messages.AI("ADR-001 says money must be int64 cents, so the discount will be `DiscountCents int64`, not a percentage float."), nil
		}
		return messages.AI("I will use a float percentage."), nil
	}}
	agent := must(agents.Create(agents.Options{Model: model, Tools: kit.All(), SystemPrompt: ciam.Instructions}))

	demo.Title("agent run")
	out := must(agent.Invoke(ctx, graph.State{"messages": "Add a discount to the invoice total"}))
	for _, m := range out["messages"].([]messages.Message) {
		switch m.Role {
		case messages.RoleAI:
			if len(m.ToolCalls) > 0 {
				fmt.Printf("agent -> tool %s(%v)\n", m.ToolCalls[0].Name, m.ToolCalls[0].Args["query"])
			} else {
				fmt.Println("agent:", m.Text())
			}
		case messages.RoleTool:
			fmt.Printf("tool result: %d bytes, includes the ADR: %v\n", len(m.Text()), strings.Contains(m.Text(), "ADR-001"))
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func must0(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
