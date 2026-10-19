// Command langgraph serves graphs for local development.
//
//	langgraph dev [--addr 127.0.0.1:2024]
//
// dev serves a built-in demo agent (a fake model that calls a tool) so you
// can see the shape of the API. To serve your own graphs, write a main
// package that calls dev.Serve (see docs/DEV.md); this command cannot load
// Go code at runtime.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/dev"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

const usage = `langgraph - development server for graphs

  langgraph dev [--addr 127.0.0.1:2024]   serve the built-in demo agent "echo"
`

type timeArgs struct {
	Zone string `json:"zone"`
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "dev" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:2024", "listen address")
	_ = fs.Parse(os.Args[2:])

	g, err := demoAgent()
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(dev.Serve(*addr, map[string]*graph.Compiled{"echo": g}))
}

// demoAgent is a scripted agent: on the first turn it calls a tool, on the
// second it answers with the tool's result. No API key needed.
func demoAgent() (*graph.Compiled, error) {
	clock := tools.New("get_time", "Current time in a zone.", func(_ context.Context, a timeArgs) (any, error) {
		return "12:00 in " + a.Zone, nil
	})
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		if last, _ := messages.Last(msgs); last.Role == messages.RoleTool {
			return messages.AI("Here is what I found: " + last.Text()), nil
		}
		return messages.AI("Let me check.", messages.ToolCall{Name: "get_time", Args: map[string]any{"zone": "UTC"}}), nil
	}}
	return agents.Create(agents.Options{
		Model: model,
		Tools: []tools.Tool{clock},
		Compile: []graph.CompileOption{
			graph.WithCheckpointer(checkpoint.NewMemory()),
		},
	})
}
