// research: planning + virtual filesystem + parallel research subagents
// (port of deepagents/examples/deep_research).
package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type SearchArgs struct {
	Query string `json:"query"`
}

func call(id, name string, args map[string]any) messages.ToolCall {
	return messages.ToolCall{ID: id, Name: name, Args: args}
}

func main() {
	search := tools.New("internet_search", "Search the web.", func(_ context.Context, a SearchArgs) (any, error) {
		return "Top result for " + a.Query + ": (stub) sodium-ion cells cost ~30% less than LFP.", nil
	})
	// scripted lead agent: plan, delegate two sub-questions in parallel, write report
	lead := models.NewFake(
		messages.AI("", call("t", "write_todos", map[string]any{"todos": []any{
			map[string]any{"content": "research cost", "status": "in_progress"},
			map[string]any{"content": "research density", "status": "pending"},
			map[string]any{"content": "write report", "status": "pending"}}})),
		messages.AI("",
			call("a", "task", map[string]any{"description": "Find cost of sodium-ion vs LFP", "subagent_type": "researcher"}),
			call("b", "task", map[string]any{"description": "Find energy density of sodium-ion", "subagent_type": "researcher"})),
		messages.AI("", call("w", "write_file", map[string]any{"file_path": "/report.md", "content": "# Sodium-ion batteries\n- cheaper than LFP\n- lower energy density"})),
		messages.AI("Report saved to /report.md."),
	)
	sub := &models.Fake{Cycle: true, Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		if last := msgs[len(msgs)-1]; last.Role == messages.RoleTool {
			return messages.AI("Finding: " + last.Text()), nil
		}
		return messages.AI("", call("s", "internet_search", map[string]any{"query": "sodium-ion"})), nil
	}}
	agent, err := deepagents.Create(deepagents.Options{
		Model: lead, SummarizeAt: -1, Tools: []tools.Tool{search},
		SubAgents: []deepagents.SubAgentSpec{{Name: "researcher", Description: "Focused researcher.", Model: sub}},
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(context.Background(), graph.State{"messages": "Compare sodium-ion and LFP batteries."})
	if err != nil {
		log.Fatal(err)
	}
	files := backends.FilesFromState(out)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Printf("--- %s ---\n%s\n", p, strings.TrimSpace(files[p].Content))
	}
	fmt.Println("todos:", out["todos"])
	fmt.Println(strings.TrimSpace(func() string { ms, _ := messages.Coerce(out["messages"]); return ms[len(ms)-1].Text() }()))
	_ = graph.End
}
