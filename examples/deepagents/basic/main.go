// deepagent: planning + virtual filesystem + subagent delegation.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	agent, err := deepagents.Create(deepagents.Options{
		Model: demo.Model(
			messages.AI("", messages.ToolCall{ID: "1", Name: "write_file",
				Args: map[string]any{"file_path": "/notes.md", "content": "# Notes\n- deep agents in Go"}}),
			messages.AI("Wrote /notes.md."),
		),
		SystemPrompt: "You are a research assistant.",
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(context.Background(), graph.State{"messages": "Save a note about deep agents."})
	if err != nil {
		log.Fatal(err)
	}
	for path, f := range backends.FilesFromState(out) {
		fmt.Printf("%s:\n%s\n", path, f.Content)
	}
}
