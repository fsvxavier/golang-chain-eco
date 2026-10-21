// Command bridge is one MCP server on stdio for chats and consoles (Copilot,
// Claude Code, Kilo, Cursor, Opencode): team tools, generic graph tools and,
// when set, ciam and knowledge. Environment: MANAGER_URL, MANAGER_API_KEY,
// HUB_URL, BRIDGE_CIAM_CMD, BRIDGE_KNOWLEDGE_CMD.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/independent_engineering_team/internal/team"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := team.NewBridge().MCPServer(ctx).ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
