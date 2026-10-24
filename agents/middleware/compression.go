package middleware

import (
	"context"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Compression shrinks the per-call copy of the context. Old Human and AI turns
// are caveman-compressed. The last Human message can carry two notes: the
// Ponytail note (when the decider asks for it) and the terse-reply directive.
// JSON tool results (messages with no Parts) are the one exception to "tool
// results are never touched": each is rewritten as TOON, but only when the TOON
// form is strictly shorter and the decider asks for it. Tool-call ids and order
// always survive, and stored history is never rewritten.
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
	ponytail := byID[compress.QPonytail].True
	toonTools := byID[compress.QTOON].True
	if !compressHistory && !compressReply && !ponytail && !toonTools {
		return next(ctx, req)
	}

	level := compress.Full
	switch p := compress.Level(byID[compress.QLevel].Pick); p {
	case compress.Lite, compress.Full, compress.Ultra:
		level = p
	}

	r := *req
	r.Messages = append([]messages.Message(nil), req.Messages...)

	if compressHistory {
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

	// the Ponytail note goes before the reply directive when both apply
	if ponytail {
		appendToLastHuman(r.Messages, "\n\n"+compress.PonytailDirective(level))
	}
	if compressReply {
		appendToLastHuman(r.Messages, "\n\n"+compress.ReplyDirective)
	}

	if toonTools {
		for i := range r.Messages {
			m := &r.Messages[i]
			if m.Role != messages.RoleTool || len(m.Parts) > 0 {
				continue
			}
			if out, ok := compress.TOONShorter(m.Content); ok {
				m.Content = out
			}
		}
	}

	return next(ctx, &r)
}

// appendToLastHuman adds text to the last Human message of msgs, in Parts when
// that is where its text lives. msgs must be the per-call copy.
func appendToLastHuman(msgs []messages.Message, text string) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != messages.RoleHuman {
			continue
		}
		m := &msgs[i]
		if len(m.Parts) > 0 && m.Content == "" {
			m.Parts = append(append([]messages.ContentPart(nil), m.Parts...), messages.ContentPart{Type: "text", Text: text})
		} else {
			m.Content = m.Content + text
		}
		return
	}
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

// Compaction is the default compaction for any agent: Compression decided by
// the rules (compress.RuleDecider), so it needs no model and no network. Old
// turns are caveman-compressed, the Ponytail note rides on the last Human
// message when the conversation has a real share of code, and JSON tool results
// become TOON when that is shorter. Use NewCompression with another decider
// (a laya model, say) to decide with more than the rules.
func Compaction() Compression { return NewCompression(compress.RuleDecider{}) }
