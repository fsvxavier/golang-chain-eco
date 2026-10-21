// compress_prompt_caveman: caveman-compresses old turns of a 12-turn history
// before each model call, and shows the code blocks survive unchanged.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

const codeBlock = "```go\nfunc Total(xs []int) int {\n\tsum := 0\n\tfor _, x := range xs {\n\t\tsum += x\n\t}\n\treturn sum\n}\n```"

// sniff records the request the model actually receives. It runs after
// Compression, so it sees the per-call copy.
type sniff struct{ got *[]messages.Message }

func (sniff) Name() string { return "sniff" }

func (s sniff) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	*s.got = req.Messages
	return next(ctx, req)
}

// yes answers every question affirmatively: compress the old turns at full
// level and ask for a terse reply.
var yes = compress.FuncDecider(func(_ context.Context, _ compress.State, qs []compress.Question) ([]compress.Answer, error) {
	out := make([]compress.Answer, 0, len(qs))
	for _, q := range qs {
		a := compress.Answer{ID: q.ID, Kind: q.Kind, True: true, P: 0.9}
		if q.ID == compress.QLevel {
			a = compress.Answer{ID: q.ID, Kind: q.Kind, Pick: "full", Dist: map[string]float64{"full": 0.9}}
		}
		out = append(out, a)
	}
	return out, nil
})

func history() []messages.Message {
	var h []messages.Message
	for i := 1; i <= 12; i++ {
		h = append(h,
			messages.Human(fmt.Sprintf("Sure! I think you should basically just check step %d of the build, please.", i)),
			messages.AI(fmt.Sprintf("Sure! Basically you should really run this now, it is important:\n\n%s\n\nThe result is in the output.", codeBlock)),
		)
	}
	return h
}

func main() {
	var seen []messages.Message
	agent, err := agents.Create(agents.Options{
		Model: demo.Smart(func(msgs []messages.Message) messages.Message {
			return messages.AI("Step 12 is the last one. The build is green.")
		}),
		SystemPrompt: "You are a build assistant.",
		Middleware: []agents.Middleware{
			middleware.NewCompression(yes),
			sniff{got: &seen},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	before := history()
	fmt.Printf("tokens before: %d\n", messages.ApproxTokens(before))

	if _, err := agent.Invoke(context.Background(), graph.State{"messages": before}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("tokens after:  %d\n", messages.ApproxTokens(seen))

	// Turn 1 is old (compressed); its code block must be byte-identical.
	fmt.Printf("old code block survives: %v\n", strings.Contains(seen[1].Text(), codeBlock))
	fmt.Printf("old prose: %q\n", seen[0].Text())
	fmt.Printf("stored history untouched: %v\n", before[0].Text() == history()[0].Text())
}
