// sql_agent: an agent that inspects a schema, writes a query, validates it
// with a read-only guard, and answers (port of tutorials/sql-agent.ipynb).
// The "database" is an in-memory table so the example needs no driver.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type noArgs struct{}

type queryArgs struct {
	SQL string `json:"query" description:"A single read-only SELECT statement"`
}

// readOnly rejects anything but a single SELECT (defence in depth: also use a
// read-only database role in real deployments).
func readOnly(q string) error {
	l := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(q), ";")))
	if !strings.HasPrefix(l, "select") || strings.Contains(l, ";") {
		return fmt.Errorf("only a single SELECT statement is allowed")
	}
	for _, bad := range []string{"insert ", "update ", "delete ", "drop ", "alter ", "attach ", "pragma "} {
		if strings.Contains(l, bad) {
			return fmt.Errorf("forbidden keyword %q", strings.TrimSpace(bad))
		}
	}
	return nil
}

func main() {
	schema := tools.New("list_tables", "Show tables and columns.", func(context.Context, noArgs) (any, error) {
		return "albums(id, title, artist_id)\nartists(id, name)", nil
	})
	run := tools.New("run_query", "Run a read-only SQL query.", func(_ context.Context, a queryArgs) (any, error) {
		if err := readOnly(a.SQL); err != nil {
			return nil, tools.Errorf("%v", err)
		}
		return "[(\"Abbey Road\",), (\"Let It Be\",)]", nil
	})
	model := demo.Smart(func(msgs []messages.Message) messages.Message {
		var tool int
		for _, m := range msgs {
			if m.Role == messages.RoleTool {
				tool++
			}
		}
		switch tool {
		case 0:
			return messages.AI("", messages.ToolCall{Name: "list_tables", Args: map[string]any{}})
		case 1:
			return messages.AI("", messages.ToolCall{Name: "run_query", Args: map[string]any{"query": "DROP TABLE albums"}})
		case 2:
			return messages.AI("", messages.ToolCall{Name: "run_query", Args: map[string]any{"query": "SELECT title FROM albums JOIN artists ON artists.id = albums.artist_id WHERE name = 'The Beatles'"}})
		}
		return messages.AI("The Beatles albums: Abbey Road, Let It Be.")
	})
	agent, err := agents.Create(agents.Options{
		Model: model, Tools: []tools.Tool{schema, run},
		SystemPrompt: "You are a SQL analyst. Look at the tables first. Never modify data.",
		Middleware:   []agents.Middleware{middleware.Compaction()},
	})
	if err != nil {
		log.Fatal(err)
	}
	for ev, err := range agent.StreamEvents(context.Background(), graph.State{"messages": "Which albums are by The Beatles?"}, graph.ModeUpdates) {
		if err != nil {
			log.Fatal(err)
		}
		if ev.Node == "tools" {
			upd := ev.Data.(map[string]any)["tools"].(graph.State)
			for _, m := range upd["messages"].([]messages.Message) {
				fmt.Printf("tool %-11s [%s] %s\n", m.Name, orOK(m.Status), strings.Split(m.Text(), "\n")[0])
			}
		}
		if ev.Node == "model" {
			upd := ev.Data.(map[string]any)["model"].(graph.State)
			ms := upd["messages"].([]messages.Message)
			if len(ms[0].ToolCalls) == 0 {
				fmt.Println("answer:", ms[0].Text())
			}
		}
	}
}

func orOK(s string) string {
	if s == "" {
		return "ok"
	}
	return s
}
