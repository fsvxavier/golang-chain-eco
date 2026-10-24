// async_subagents: the main agent launches a long task in the background and
// checks it later (port of deepagents/examples/async-subagent-server, using the
// in-process runner instead of a server).
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	dmw "github.com/fsvxavier/golang-chain-eco/deepagents/middleware"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	researcher, _ := agents.Create(agents.Options{
		Model: &models.Fake{Func: func([]messages.Message, models.Options) (messages.Message, error) {
			time.Sleep(50 * time.Millisecond) // pretend this takes a while
			return messages.AI("Report: sodium-ion batteries are cheaper but less energy dense."), nil
		}},
		Middleware: []agents.Middleware{middleware.Compaction()},
	})
	runner := &dmw.LocalRunner{Agent: researcher}
	main := models.NewFake(
		messages.AI("", messages.ToolCall{Name: "start_async_task", Args: map[string]any{"description": "research sodium batteries", "subagent_type": "researcher"}}),
		messages.AI("Started the research; I will check back."),
	)
	agent, err := deepagents.Create(deepagents.Options{
		Model: main, SummarizeAt: -1,
		Middleware: []agents.Middleware{dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
			{Name: "researcher", Description: "long-running research", Runner: runner},
		}}},
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(context.Background(), graph.State{"messages": "Research sodium batteries in the background."})
	if err != nil {
		log.Fatal(err)
	}
	for id, t := range out["async_tasks"].(map[string]dmw.AsyncTask) {
		fmt.Printf("task %s launched on %s (%s)\n", id, t.Agent, t.Status)
		for {
			st, res, _ := runner.Poll(context.Background(), t.Handle)
			if st != dmw.AsyncRunning {
				fmt.Printf("task %s finished: %s -> %s\n", id, st, res)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
