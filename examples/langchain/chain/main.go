// chain: prompt | model | parser, the LCEL equivalent.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/outputparsers"
	"github.com/fsvxavier/golang-chain-eco/core/prompts"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
)

func main() {
	prompt := prompts.Chat([2]string{"system", "You answer in one sentence."}, [2]string{"human", "Explain {topic}."})
	model := models.AsRunnable(demo.Model(messages.AI("Goroutines are lightweight threads managed by the Go runtime.")))
	chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
		prompt, model, outputparsers.Str{},
	)
	out, err := chain.Invoke(context.Background(), prompts.Values{"topic": "goroutines"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out)
}
