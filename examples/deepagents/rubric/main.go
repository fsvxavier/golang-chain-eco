// rubric: a grader reviews the final answer against criteria and sends the
// agent back to fix gaps (port of deepagents/examples/rubric_middleware).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	dmw "github.com/fsvxavier/golang-chain-eco/deepagents/middleware"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	writer := demo.Smart(func(msgs []messages.Message) messages.Message {
		if strings.Contains(messages.Buffer(msgs), "source") {
			return messages.AI("Go powers Docker and Kubernetes [source: go.dev/solutions].")
		}
		return messages.AI("Go powers Docker and Kubernetes.")
	})
	grader := demo.Smart(func(msgs []messages.Message) messages.Message {
		if strings.Contains(messages.Buffer(msgs), "[source:") {
			return messages.AI(`{"result":"satisfied","criteria":[{"name":"cites a source","passed":true}]}`)
		}
		return messages.AI(`{"result":"needs_revision","criteria":[{"name":"cites a source","passed":false,"gap":"add a [source: url] citation"}]}`)
	})
	agent, err := deepagents.Create(deepagents.Options{
		Model: writer, SummarizeAt: -1,
		Middleware: []agents.Middleware{dmw.Rubric{Grader: grader, Criteria: []string{"cites a source"}, MaxIterations: 3}},
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(context.Background(), graph.State{"messages": "One sentence on what Go is used for."})
	if err != nil {
		log.Fatal(err)
	}
	for _, ev := range out["rubric_evaluations"].([]any) {
		e := ev.(dmw.RubricEvaluation)
		fmt.Printf("grade: %-14s %+v\n", e.Result, e.Criteria)
	}
	fmt.Println("final:", demo.Last(out))
}
