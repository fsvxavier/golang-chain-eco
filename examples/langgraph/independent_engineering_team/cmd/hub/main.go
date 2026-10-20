// Command hub runs the registry, the job queue and the knowledge/memory MCP
// server that every other service uses.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/independent_engineering_team/internal/team"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ln, err := net.Listen("tcp", team.Env("LISTEN", ":8000"))
	if err != nil {
		log.Fatal(err)
	}
	if err := team.RunHub(ctx, ln); err != nil {
		log.Fatal(err)
	}
}
