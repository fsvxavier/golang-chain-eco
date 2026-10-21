// compress_ponytail: appends the Ponytail directive to the last Human message
// of the per-call copy, at each level, and proves that the system prompt and
// the stored history are untouched.
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

const (
	system   = "You are a Go developer working in this repository."
	first    = "Add a cache in front of the user lookup."
	reply    = "Done. The cache wraps the lookup."
	followUp = "Now write a test for it."
)

// sniff records the request the model actually receives. It runs after
// Compression, so it sees the per-call copy.
type sniff struct{ got *agents.ModelRequest }

func (sniff) Name() string { return "sniff" }

func (s sniff) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	cp := *req
	cp.Messages = append([]messages.Message(nil), req.Messages...)
	*s.got = cp
	return next(ctx, req)
}

// scripted answers the history questions: ponytail is on or off, the level is
// fixed, and every other question is no. Confidence is 0.9 so the guard keeps
// these answers.
func scripted(on bool, lvl compress.Level) compress.Decider {
	return compress.FuncDecider(func(_ context.Context, _ compress.State, qs []compress.Question) ([]compress.Answer, error) {
		out := make([]compress.Answer, 0, len(qs))
		for _, q := range qs {
			switch q.ID {
			case compress.QLevel:
				out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, Pick: string(lvl), Dist: map[string]float64{string(lvl): 0.9}})
			case compress.QPonytail:
				out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, True: on, P: probOf(on)})
			default:
				out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, True: false, P: 0.1})
			}
		}
		return out, nil
	})
}

func probOf(yes bool) float64 {
	if yes {
		return 0.9
	}
	return 0.1
}

func history() []messages.Message {
	return []messages.Message{
		messages.Human(first),
		messages.AI(reply),
		messages.Human(followUp),
	}
}

// run invokes a fresh agent once and returns what the model saw.
func run(on bool, lvl compress.Level, stored []messages.Message) agents.ModelRequest {
	var seen agents.ModelRequest
	agent, err := agents.Create(agents.Options{
		Model: demo.Smart(func(msgs []messages.Message) messages.Message {
			return messages.AI("Test added: the second lookup hits the cache.")
		}),
		SystemPrompt: system,
		Middleware: []agents.Middleware{
			middleware.NewCompression(scripted(on, lvl)),
			sniff{got: &seen},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := agent.Invoke(context.Background(), graph.State{"messages": stored}); err != nil {
		log.Fatal(err)
	}
	return seen
}

func main() {
	stored := history()
	want := history()

	fmt.Println("== ponytail off ==")
	off := run(false, compress.Full, stored)
	lastOff := off.Messages[len(off.Messages)-1].Text()
	fmt.Printf("last human unchanged: %v\n", lastOff == followUp)
	if lastOff != followUp || off.System != system {
		log.Fatal("ponytail off: the model saw a changed message or system prompt")
	}

	for _, lvl := range []compress.Level{compress.Lite, compress.Full, compress.Ultra} {
		fmt.Printf("\n== ponytail level %s ==\n", lvl)
		got := run(true, lvl, stored)

		var humans []string
		for _, m := range got.Messages {
			if m.Role == messages.RoleHuman {
				humans = append(humans, m.Text())
			}
		}
		last := humans[len(humans)-1]
		note := strings.TrimPrefix(last, followUp)
		directive := compress.PonytailDirective(lvl)

		fmt.Printf("note bytes: %d\n", len(note))
		fmt.Printf("note is the directive: %v\n", note == "\n\n"+directive)
		fmt.Printf("first line: %q\n", strings.SplitN(directive, "\n", 2)[0])
		onlyLast := humans[0] == first && strings.Count(strings.Join(humans, "\n"), directive) == 1
		fmt.Printf("only last human has the note: %v\n", onlyLast)
		fmt.Printf("system prompt unchanged: %v\n", got.System == system)
		if lvl == compress.Full {
			fmt.Println("full note:")
			fmt.Println(directive)
		}
		if note != "\n\n"+directive || got.System != system || !onlyLast {
			log.Fatalf("level %s: note or system prompt check failed", lvl)
		}
	}

	fmt.Println("\n== stored history ==")
	untouched := len(stored) == len(want)
	for i := range want {
		untouched = untouched && stored[i].Text() == want[i].Text()
	}
	fmt.Printf("stored history untouched: %v\n", untouched)
	if !untouched {
		log.Fatal("stored history was changed")
	}
	fmt.Println("Ponytail is a behaviour nudge: how much less code the model writes must be measured with a real model.")
}
