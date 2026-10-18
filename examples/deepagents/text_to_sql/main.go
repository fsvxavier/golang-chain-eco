// text_to_sql: a deep agent that plans, explores a schema stored as files
// and delegates query validation to a subagent (port of
// deepagents/examples/text-to-sql-agent). Skills hold the schema notes.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type sqlArgs struct {
	Query string `json:"query"`
}

func call(id, name string, args map[string]any) messages.ToolCall {
	return messages.ToolCall{ID: id, Name: name, Args: args}
}

func main() {
	db := tools.New("sql_query", "Run a read-only SELECT on the Chinook database.", func(_ context.Context, a sqlArgs) (any, error) {
		return "Rock|1297\nLatin|579\nMetal|374", nil
	})
	// the skill is a file in the virtual filesystem; the agent reads it on demand
	seed := backends.State{}
	_ = seed
	lead := models.NewFake(
		messages.AI("", call("1", "write_todos", map[string]any{"todos": []any{
			map[string]any{"content": "read schema skill", "status": "in_progress"},
			map[string]any{"content": "write and validate query", "status": "pending"}}})),
		messages.AI("", call("2", "write_file", map[string]any{"file_path": "/schema.md", "content": "tracks(genre_id, ...)\ngenres(id, name)"})),
		messages.AI("", call("3", "task", map[string]any{"description": "Validate: SELECT g.name, COUNT(*) FROM tracks t JOIN genres g ON g.id=t.genre_id GROUP BY g.name ORDER BY 2 DESC LIMIT 3", "subagent_type": "query-validator"})),
		messages.AI("", call("4", "sql_query", map[string]any{"query": "SELECT g.name, COUNT(*) FROM tracks t JOIN genres g ON g.id=t.genre_id GROUP BY g.name ORDER BY 2 DESC LIMIT 3"})),
		messages.AI("Top genres by track count: Rock (1297), Latin (579), Metal (374)."),
	)
	validator := models.NewFake(messages.AI("VALID: read-only SELECT, joins on indexed keys, LIMIT present."))
	agent, err := deepagents.Create(deepagents.Options{
		Model: lead, Tools: []tools.Tool{db}, SummarizeAt: -1,
		SystemPrompt: "You are a careful SQL analyst. Plan first, validate queries, never modify data.",
		SubAgents: []deepagents.SubAgentSpec{{
			Name: "query-validator", Description: "Reviews SQL for safety and correctness.", Model: validator,
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	for ev, err := range agent.StreamEvents(context.Background(), graph.State{"messages": "Which 3 genres have the most tracks?"}, graph.ModeUpdates) {
		if err != nil {
			log.Fatal(err)
		}
		if ev.Node != "model" {
			continue
		}
		ms := ev.Data.(map[string]any)["model"].(graph.State)["messages"].([]messages.Message)
		for _, tc := range ms[0].ToolCalls {
			fmt.Printf("agent calls %-12s %v\n", tc.Name, shorten(fmt.Sprint(tc.Args)))
		}
		if len(ms[0].ToolCalls) == 0 {
			fmt.Println("answer:", ms[0].Text())
		}
	}
}

func shorten(s string) string {
	if len(s) > 90 {
		return s[:90] + "…"
	}
	return s
}
