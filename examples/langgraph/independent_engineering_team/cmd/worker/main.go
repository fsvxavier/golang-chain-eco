// Command worker runs one agent (WORKER_ROLE) as its own service: it serves
// the agent over Agent Protocol and takes jobs from the hub's queue.
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
	role := team.Env("WORKER_ROLE", "")
	cfg := team.WorkerConfig{
		ID: team.Env("WORKER_ID", role), Role: role,
		Hub: team.Env("HUB_URL", "http://localhost:8000"), URL: os.Getenv("WORKER_URL"),
	}
	if cfg.Role == "" {
		log.Fatal("WORKER_ROLE is required (po, architect, leader, junior, mid, senior, specialist, qa)")
	}
	ln, err := net.Listen("tcp", team.Env("LISTEN", ":9000"))
	if err != nil {
		log.Fatal(err)
	}
	if err := team.RunWorker(ctx, cfg, ln); err != nil {
		log.Fatal(err)
	}
}
