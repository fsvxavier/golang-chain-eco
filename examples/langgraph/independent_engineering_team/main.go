// independent_engineering_team: the engineering_team flow with every agent as
// its own service. A hub keeps the registry (atomic reservation with leases)
// and the job queue and serves RAG, CAG, KAG and the team memory over MCP;
// each worker is a separate process that serves its agent over Agent
// Protocol and pulls jobs from the queue; the manager serves the delivery
// graph through the Agent Server API.
//
// This main starts all of them in one process, each on its own loopback port
// and talking over real HTTP, with a scripted model, so it runs offline. The
// same code runs as separate processes: see docker-compose.yml and cmd/.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/independent_engineering_team/internal/team"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	l, err := team.StartLocal(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("hub %s, manager %s, %d workers\n", l.HubURL, l.ManagerURL, len(l.Workers))

	out, err := team.Invoke(ctx, l.ManagerURL, messages.NewID(), team.DefaultRequest)
	if err != nil {
		log.Fatal(err)
	}
	team.Print(out)
	cancel()
	l.Wait()
}
