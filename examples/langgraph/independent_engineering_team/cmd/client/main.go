// Command client sends a request to the manager service and prints the report
// and the handoff history.
package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/independent_engineering_team/internal/team"
)

func main() {
	request := strings.Join(os.Args[1:], " ")
	if request == "" {
		request = team.DefaultRequest
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	out, err := team.Invoke(ctx, team.Env("MANAGER_URL", "http://localhost:8080"), messages.NewID(), request)
	if err != nil {
		log.Fatal(err)
	}
	team.Print(out)
}
