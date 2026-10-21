// decider_fallback: rule-based answers to the history questions, and a
// Guarded decider that falls back to those rules when the model fails.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

func main() {
	ctx := context.Background()
	short := history(2, 40)
	long := history(60, 400)

	fmt.Println("== RuleDecider ==")
	for _, h := range []struct {
		name string
		msgs []messages.Message
	}{{"short", short}, {"long", long}} {
		st := compress.Summarize(h.msgs, compress.SummaryOptions{}).State()
		fmt.Printf("history %s: messages=%d tokens=%d\n", h.name, st["messages"], st["tokens"])
		answers, err := (compress.RuleDecider{}).Decide(ctx, st, compress.HistoryQuestions())
		if err != nil {
			log.Fatal(err)
		}
		printAnswers("  ", answers)
	}

	fmt.Println("== Guarded over a failing model ==")
	failing := compress.FuncDecider(func(context.Context, compress.State, []compress.Question) ([]compress.Answer, error) {
		return nil, errors.New("model unavailable")
	})
	g := compress.NewGuarded(failing)
	st := compress.Summarize(long, compress.SummaryOptions{}).State()
	answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("inner decider failed, rules answered:")
	printAnswers("  ", answers)
}

// history returns n Human/AI pairs, each text about size bytes long.
func history(n, size int) []messages.Message {
	var msgs []messages.Message
	for i := 0; i < n; i++ {
		msgs = append(msgs,
			messages.Human(fmt.Sprintf("Question %d: %s", i, strings.Repeat("please check the build log. ", size/28))),
			messages.AI(fmt.Sprintf("Answer %d: %s", i, strings.Repeat("the build passed on the second run. ", size/35))),
		)
	}
	return msgs
}

func printAnswers(indent string, answers []compress.Answer) {
	for _, a := range answers {
		switch a.Kind {
		case compress.KindNoul:
			fmt.Printf("%s%s: %t (P=%.2f)\n", indent, a.ID, a.True, a.P)
		case compress.KindChoice:
			fmt.Printf("%s%s: %s\n", indent, a.ID, a.Pick)
		}
	}
}
