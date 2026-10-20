// Command manager runs the Software Engineer Manager: it receives a request
// over the Agent Server API and drives the team through the hub.
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
	ln, err := net.Listen("tcp", team.Env("LISTEN", ":8080"))
	if err != nil {
		log.Fatal(err)
	}
	if err := team.RunManager(ctx, team.Env("HUB_URL", "http://localhost:8000"), ln); err != nil {
		log.Fatal(err)
	}
}
