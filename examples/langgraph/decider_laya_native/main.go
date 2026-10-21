// decider_laya_native: answers the history and shell questions with an
// in-process laya-go model (build tag laya_native), or with the rules when the
// binary has no native support or LAYA_BUNDLE_DIR is unset.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

func main() {
	ctx := context.Background()

	native := true
	inner, closeFn, err := openDecider(ctx, os.Getenv("LAYA_BUNDLE_DIR"))
	if err != nil {
		fmt.Println("laya unavailable, using RuleDecider:", err)
		native = false
		inner = compress.RuleDecider{}
		closeFn = func() {}
	}
	defer closeFn()

	// probe records what the inner decider said, so the output shows who answered.
	var raw []compress.Answer
	probe := compress.FuncDecider(func(ctx context.Context, st compress.State, qs []compress.Question) ([]compress.Answer, error) {
		a, err := inner.Decide(ctx, st, qs)
		raw = a
		return a, err
	})
	g := compress.NewGuarded(probe)

	msgs := []messages.Message{
		messages.Human("Please check the build and tell me what fails."),
		messages.AI("Running the tests now."),
		messages.Human("Thanks, and summarise the result briefly."),
	}
	history := compress.Summarize(msgs, compress.SummaryOptions{}).State()
	show(g, native, &raw, history, compress.HistoryQuestions())
	show(g, native, &raw, compress.ShellState("go test ./...", 5000), []compress.Question{compress.ShellQuestion()})
}

// show asks the guarded decider and prints each answer with its source.
func show(g compress.Guarded, native bool, raw *[]compress.Answer, st compress.State, qs []compress.Question) {
	*raw = nil
	answers, err := g.Decide(context.Background(), st, qs)
	if err != nil {
		log.Fatal(err)
	}
	for i, a := range answers {
		who := "rules"
		if r, ok := find(*raw, a.ID); native && ok && r.Kind == a.Kind && r.Confident(g.Floor) {
			who = "laya"
		}
		if a.Kind == compress.KindChoice {
			fmt.Printf("%-16s %-6s choice %s (p=%.2f)\n", qs[i].ID, who, a.Pick, a.Dist[a.Pick])
		} else {
			fmt.Printf("%-16s %-6s noul   %v (p=%.2f)\n", qs[i].ID, who, a.True, a.P)
		}
	}
}

func find(as []compress.Answer, id string) (compress.Answer, bool) {
	for _, a := range as {
		if a.ID == id {
			return a, true
		}
	}
	return compress.Answer{}, false
}
