// Command bridge is an MCP server on stdio that lets chats and consoles
// (Copilot, Claude Code, Kilo, Cursor, Opencode) send demands to the team and
// follow them. Environment: MANAGER_URL, MANAGER_API_KEY, HUB_URL.
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
	if err := team.NewBridge().MCPServer().ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
