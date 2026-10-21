package middleware

import (
	"context"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Compression shrinks the per-call copy of the context: old Human and AI turns
// are caveman-compressed and the last Human message can carry a terse-reply
// directive. Tool calls and tool results are never touched, so every tool-call
// id and order survives. Stored history is never rewritten.
//
// Keep is the number of recent turns left alone (default 4).
//
// NewCompression wraps the decider in compress.NewGuarded (2s timeout, 0.7
// confidence floor, rules as fallback). A literal Compression{Decider: x} uses
// x as is: no timeout, no confidence check. Use NewCompression unless you
// wrap the decider yourself.
//
// Summarization stays a separate BeforeModel middleware: set its MaxTokens
// above the decider's threshold so it acts as a backstop.
type Compression struct {
	Decider compress.Decider
	Keep    int
}

// NewCompression wraps d in compress.NewGuarded, so a slow or wrong decider
// falls back to the rules.
func NewCompression(d compress.Decider) Compression {
	return Compression{Decider: compress.NewGuarded(d), Keep: 4}
}

func (Compression) Name() string { return "compression" }

func (c Compression) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	keep := c.Keep
	if keep <= 0 {
		keep = 4
	}
	d := c.Decider
	if d == nil {
		d = compress.RuleDecider{}
	}

	sum := compress.Summarize(req.Messages, compress.SummaryOptions{})
	answers, err := d.Decide(ctx, sum.State(), compress.HistoryQuestions())
	if err != nil {
		// a failing decider never fails the call: use the rules instead
		answers, err = compress.RuleDecider{}.Decide(ctx, sum.State(), compress.HistoryQuestions())
		if err != nil {
			return next(ctx, req)
		}
	}
	byID := make(map[string]compress.Answer, len(answers))
	for _, a := range answers {
		byID[a.ID] = a
	}

	compressHistory := byID[compress.QCompressHistory].True
	compressReply := byID[compress.QCompressReply].True
	if !compressHistory && !compressReply {
		return next(ctx, req)
	}

	r := *req
	r.Messages = append([]messages.Message(nil), req.Messages...)

	if compressHistory {
		level := compress.Full
		switch p := compress.Level(byID[compress.QLevel].Pick); p {
		case compress.Lite, compress.Full, compress.Ultra:
			level = p
		}
		cut := oldTurnsEnd(r.Messages, keep)
		for i := 0; i < cut; i++ {
			m := &r.Messages[i]
			if m.Role != messages.RoleHuman && m.Role != messages.RoleAI {
				continue
			}
			m.Content = compress.Compress(m.Content, level)
			if len(m.Parts) > 0 {
				parts := append([]messages.ContentPart(nil), m.Parts...)
				for j := range parts {
					if parts[j].Type == "text" {
						parts[j].Text = compress.Compress(parts[j].Text, level)
					}
				}
				m.Parts = parts
			}
		}
	}

	if compressReply {
		for i := len(r.Messages) - 1; i >= 0; i-- {
			if r.Messages[i].Role != messages.RoleHuman {
				continue
			}
			m := &r.Messages[i]
			if len(m.Parts) > 0 && m.Content == "" {
				// text lives in Parts; append the directive there
				m.Parts = append(append([]messages.ContentPart(nil), m.Parts...), messages.ContentPart{Type: "text", Text: "\n\n" + compress.ReplyDirective})
			} else {
				m.Content = m.Content + "\n\n" + compress.ReplyDirective
			}
			break
		}
	}

	return next(ctx, &r)
}

// oldTurnsEnd returns the index where the last keep turns start. A turn starts
// at a Human message. Messages before the returned index are old.
func oldTurnsEnd(msgs []messages.Message, keep int) int {
	var humans []int
	for i, m := range msgs {
		if m.Role == messages.RoleHuman {
			humans = append(humans, i)
		}
	}
	if len(humans) <= keep {
		return 0
	}
	return humans[len(humans)-keep]
}
