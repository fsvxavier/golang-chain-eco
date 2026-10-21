// compress_history_summary: builds a long history and shows the bounded
// Summary that deciders read instead of the full transcript.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

const (
	minHistoryTokens = 8000
	summaryBudget    = 700
)

func main() {
	msgs := longHistory()

	s := compress.Summarize(msgs, compress.SummaryOptions{})
	tokens := 0
	for _, m := range msgs {
		tokens += len(m.Text()) / 4
	}
	if tokens < minHistoryTokens {
		log.Fatalf("history too short: %d tokens", tokens)
	}

	encoded := s.EncodedTokens()
	fmt.Printf("messages: %d\n", len(msgs))
	fmt.Printf("history tokens: %d\n", tokens)
	fmt.Printf("summary tokens: %d (budget %d)\n", encoded, summaryBudget)
	if encoded > summaryBudget {
		log.Fatalf("summary over budget: %d > %d tokens", encoded, summaryBudget)
	}

	out, err := json.MarshalIndent(s.State(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("summary:\n%s\n", out)
}

// longHistory returns 40 turns (human, AI and a tool result each) of about
// 13k estimated tokens in total.
func longHistory() []messages.Message {
	var msgs []messages.Message
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("call-%02d", i)
		msgs = append(msgs,
			messages.Human(fmt.Sprintf("Turn %d: can you check the deploy for service %d? %s", i, i%5, strings.Repeat("Details of the rollout and the error budget. ", 20))),
			messages.AI("I will call the status tool now.", messages.ToolCall{ID: id, Name: "status", Args: map[string]any{"service": i % 5}}),
			messages.Tool(fmt.Sprintf(`{"service":%d,"status":"ok","log":%q}`, i%5, strings.Repeat("ready 200 ", 30)), id),
		)
	}
	return msgs
}
