// async_subagent_server: serve a worker agent over the Agent Server HTTP API
// and let a supervisor drive it as an async subagent through a RemoteRunner
// (port of deepagents/examples/async-subagent-server; server and client run
// in one process here, but the client only uses HTTP).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http/httptest"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	dmw "github.com/fsvxavier/golang-chain-eco/deepagents/middleware"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/remote"
	"github.com/fsvxavier/golang-chain-eco/remote/server"
)

func main() {
	// --- the remote worker -------------------------------------------------
	worker, err := agents.Create(agents.Options{
		Model: &models.Fake{Func: func([]messages.Message, models.Options) (messages.Message, error) {
			time.Sleep(150 * time.Millisecond) // a "long" research job
			return messages.AI("Findings: solid-state batteries reach ~400 Wh/kg in lab cells."), nil
		}},
		Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())},
	})
	if err != nil {
		log.Fatal(err)
	}
	ts := httptest.NewServer(server.New(map[string]*graph.Compiled{"researcher": worker}))
	defer ts.Close()
	fmt.Println("agent server listening on", ts.URL)

	// --- the supervisor ----------------------------------------------------
	runner := dmw.RemoteRunner{Client: remote.NewClient(ts.URL), AssistantID: "researcher"}
	supervisor, err := deepagents.Create(deepagents.Options{
		Model: models.NewFake(
			messages.AI("", messages.ToolCall{Name: "start_async_task", Args: map[string]any{"description": "Research solid-state batteries", "subagent_type": "researcher"}}),
			messages.AI("The research is running in the background."),
		),
		SummarizeAt: -1,
		Middleware:  []agents.Middleware{dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{{Name: "researcher", Description: "remote researcher", Runner: runner}}}},
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := supervisor.Invoke(context.Background(), graph.State{"messages": "Start researching solid-state batteries."})
	if err != nil {
		log.Fatal(err)
	}
	for id, t := range out["async_tasks"].(map[string]dmw.AsyncTask) {
		fmt.Printf("task %s -> %s (%s)\n", id, t.Agent, t.Status)
		for {
			st, res, err := runner.Poll(context.Background(), t.Handle)
			if err != nil {
				log.Fatal(err)
			}
			if st != dmw.AsyncRunning {
				fmt.Printf("task %s %s: %s\n", id, st, res)
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
}
