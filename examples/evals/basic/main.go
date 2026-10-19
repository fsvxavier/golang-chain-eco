// evals/basic: score a target over a JSONL dataset with exact, contains,
// embedding and LLM-judge evaluators, then print the report as text and JSON.
// Offline: the target and the judge are fake models unless an API key is set.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/evals"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
)

func main() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "evals-basic")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "qa.jsonl")
	dataset := []evals.Example{
		{ID: "capital-fr", Inputs: map[string]any{"question": "Capital of France?"}, Outputs: map[string]any{"answer": "Paris"}},
		{ID: "capital-jp", Inputs: map[string]any{"question": "Capital of Japan?"}, Outputs: map[string]any{"answer": "Tokyo"}},
		{ID: "sum", Inputs: map[string]any{"question": "2 + 2?"}, Outputs: map[string]any{"answer": "4"}},
	}
	if err := evals.SaveJSONL(path, dataset); err != nil {
		log.Fatal(err)
	}
	dataset, err = evals.LoadJSONL(path) // the usual flow: the dataset lives in a file
	if err != nil {
		log.Fatal(err)
	}

	// Target: the system under test. The fake answers from a lookup table.
	answers := map[string]string{"Capital of France?": "Paris", "Capital of Japan?": "Tokyo", "2 + 2?": "It is 4."}
	target := demo.Smart(func(msgs []messages.Message) messages.Message {
		q, _ := messages.LastOfRole(msgs, messages.RoleHuman)
		return messages.AI(answers[q.Text()])
	})
	run := func(ctx context.Context, in map[string]any) (map[string]any, error) {
		msg, err := target.Generate(ctx, []messages.Message{messages.Human(in["question"].(string))})
		return map[string]any{"answer": msg.Text()}, err
	}

	// Judge: a second model grading the answer against a rubric.
	judge := demo.Smart(func([]messages.Message) messages.Message {
		return messages.AI(`{"score": 1, "reasoning": "answers the question correctly"}`)
	})

	report, err := evals.Evaluate(ctx, run, dataset, []evals.Evaluator{
		evals.ExactMatch("answer"),
		evals.Contains("answer"),
		evals.EmbeddingDistance(embeddings.Fake{}, "answer", 0.8),
		evals.LLMJudge(judge, "The answer must be a correct, direct reply to the question."),
	}, evals.WithConcurrency(3))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println()
	if err := evals.WriteText(os.Stdout, report); err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nJSON:")
	if err := evals.WriteJSON(os.Stdout, report); err != nil {
		log.Fatal(err)
	}
}
