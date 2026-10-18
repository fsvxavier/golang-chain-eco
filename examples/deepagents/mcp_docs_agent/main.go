// mcp_docs_agent: a docs-search agent whose tools come from an MCP server
// (port of deepagents/examples/deploy-mcp-docs-agent). An in-process MCP
// server stands in for a real one; swap mcp.NewPipeTransport for mcp.HTTP(url)
// or mcp.Stdio(...) to use a real server.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/mcp"
)

// serve is a tiny MCP server exposing one tool, "search_docs".
func serve(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var req map[string]any
		if json.Unmarshal(sc.Bytes(), &req) != nil || req["id"] == nil {
			continue
		}
		var res any
		switch req["method"] {
		case "initialize":
			res = map[string]any{"serverInfo": map[string]any{"name": "docs"}}
		case "tools/list":
			res = map[string]any{"tools": []any{map[string]any{
				"name": "search_docs", "description": "Search the LangGraph documentation.",
				"inputSchema": map[string]any{"type": "object", "required": []string{"query"},
					"properties": map[string]any{"query": map[string]any{"type": "string"}}}}}}
		case "tools/call":
			q := req["params"].(map[string]any)["arguments"].(map[string]any)["query"]
			res = map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("Docs hit for %q: a checkpointer persists state per thread_id.", q)}}}
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": res})
		fmt.Fprintf(w, "%s\n", b)
	}
}

func main() {
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	go serve(sr, sw)

	ctx := context.Background()
	client, err := mcp.Connect(ctx, mcp.NewPipeTransport(cw, cr))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	ts, err := client.Tools(ctx, "docs")
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range ts {
		fmt.Printf("MCP tool loaded: %s - %s\n", t.Name(), t.Description())
	}
	agent, err := agents.Create(agents.Options{
		Model: models.NewFake(
			messages.AI("", messages.ToolCall{Name: "docs_search_docs", Args: map[string]any{"query": "persistence"}}),
			messages.AI("Use a checkpointer: it saves state for each thread_id."),
		),
		Tools: ts, SystemPrompt: "Answer from the docs tool only.",
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(ctx, graph.State{"messages": "How does persistence work?"})
	if err != nil {
		log.Fatal(err)
	}
	ms := out["messages"].([]messages.Message)
	fmt.Println("tool said:", ms[2].Text())
	fmt.Println("answer   :", ms[3].Text())
}
