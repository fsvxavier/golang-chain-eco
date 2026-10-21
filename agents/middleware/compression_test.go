package middleware

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

const verbose = "Sure! I think you should basically just run the tests and then check the output carefully."

// turns builds n Human/AI exchanges; each Human and AI text is verbose.
func turns(n int) []messages.Message {
	var out []messages.Message
	for i := 0; i < n; i++ {
		out = append(out, messages.Human(verbose), messages.AI(verbose))
	}
	return out
}

// answerDecider says yes to history compression, answers reply for
// compress_reply, and picks level.
func answerDecider(level string, reply bool) compress.Decider {
	return compress.FuncDecider(func(_ context.Context, _ compress.State, qs []compress.Question) ([]compress.Answer, error) {
		out := make([]compress.Answer, 0, len(qs))
		for _, q := range qs {
			switch q.ID {
			case compress.QLevel:
				out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, Pick: level, Dist: map[string]float64{level: 1}})
			case compress.QCompressReply:
				out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, True: reply, P: 1})
			default:
				out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, True: true, P: 1})
			}
		}
		return out, nil
	})
}

// capture records the request passed to next and returns an AI reply.
func capture(got **agents.ModelRequest) agents.ModelCaller {
	return func(_ context.Context, req *agents.ModelRequest) (messages.Message, error) {
		*got = req
		return messages.AI("ok"), nil
	}
}

func TestCompressionOldTurnsOnly(t *testing.T) {
	in := turns(10)
	c := Compression{Decider: answerDecider("full", false), Keep: 4}
	var got *agents.ModelRequest
	if _, err := c.WrapModelCall(context.Background(), &agents.ModelRequest{Messages: in}, capture(&got)); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != len(in) {
		t.Fatalf("len = %d, want %d", len(got.Messages), len(in))
	}
	// last 4 turns = last 8 messages, identical
	cut := len(in) - 8
	if !reflect.DeepEqual(got.Messages[cut:], in[cut:]) {
		t.Fatalf("last 4 turns changed")
	}
	for i := 0; i < cut; i++ {
		if in[i].Role == messages.RoleTool {
			continue
		}
		if len(got.Messages[i].Text()) >= len(in[i].Text()) {
			t.Fatalf("old message %d not shorter: %q", i, got.Messages[i].Text())
		}
	}
}

func TestCompressionKeepsToolPairs(t *testing.T) {
	big := strings.Repeat(verbose+" ", 20)
	in := []messages.Message{
		messages.Human(verbose),
		messages.AI(verbose, messages.ToolCall{ID: "c1", Name: "read", Args: map[string]any{"path": "a.go"}}),
		messages.Tool(big, "c1"),
		messages.Human(verbose),
		messages.AI(verbose, messages.ToolCall{ID: "c2", Name: "run"}, messages.ToolCall{ID: "c3", Name: "run"}),
		messages.Tool(big, "c2"),
		messages.Tool(big, "c3"),
		messages.Human("latest"),
	}
	c := Compression{Decider: answerDecider("ultra", false), Keep: 1}
	var got *agents.ModelRequest
	if _, err := c.WrapModelCall(context.Background(), &agents.ModelRequest{Messages: in}, capture(&got)); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != len(in) {
		t.Fatalf("len changed: %d -> %d", len(in), len(got.Messages))
	}
	for i := range in {
		g, w := got.Messages[i], in[i]
		if len(g.ToolCalls) != len(w.ToolCalls) {
			t.Fatalf("msg %d tool call count changed", i)
		}
		for j := range w.ToolCalls {
			if g.ToolCalls[j].ID != w.ToolCalls[j].ID || g.ToolCalls[j].Name != w.ToolCalls[j].Name {
				t.Fatalf("msg %d tool call %d changed", i, j)
			}
		}
		if w.Role == messages.RoleTool {
			if g.ToolCallID != w.ToolCallID || g.Content != w.Content {
				t.Fatalf("tool message %d changed", i)
			}
		}
	}
}

func TestCompressionDoesNotMutateInput(t *testing.T) {
	in := turns(6)
	orig := append([]messages.Message(nil), in...)
	c := Compression{Decider: answerDecider("ultra", false), Keep: 2}
	var got *agents.ModelRequest
	if _, err := c.WrapModelCall(context.Background(), &agents.ModelRequest{Messages: in}, capture(&got)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, orig) {
		t.Fatal("input messages were mutated")
	}
}

func TestCompressionReplyDirective(t *testing.T) {
	in := turns(3)
	in = append(in, messages.Human("question"))
	req := &agents.ModelRequest{System: "be helpful", Messages: in}
	c := Compression{Decider: answerDecider("full", true), Keep: 4}
	var got *agents.ModelRequest
	if _, err := c.WrapModelCall(context.Background(), req, capture(&got)); err != nil {
		t.Fatal(err)
	}
	if got.System != "be helpful" {
		t.Fatalf("system prompt changed: %q", got.System)
	}
	last := len(got.Messages) - 1
	if !strings.HasSuffix(got.Messages[last].Text(), "\n\n"+compress.ReplyDirective) {
		t.Fatalf("directive missing on last human: %q", got.Messages[last].Text())
	}
	for i, m := range got.Messages {
		if i != last && strings.Contains(m.Text(), compress.ReplyDirective) {
			t.Fatalf("directive leaked to message %d", i)
		}
	}
	if strings.Contains(in[last].Text(), compress.ReplyDirective) {
		t.Fatal("input mutated with directive")
	}
}

func TestCompressionDeciderErrorStillCalls(t *testing.T) {
	failing := compress.FuncDecider(func(context.Context, compress.State, []compress.Question) ([]compress.Answer, error) {
		return nil, errors.New("boom")
	})
	c := Compression{Decider: failing, Keep: 4}
	calls := 0
	next := func(_ context.Context, req *agents.ModelRequest) (messages.Message, error) {
		calls++
		return messages.AI("ok"), nil
	}
	if _, err := c.WrapModelCall(context.Background(), &agents.ModelRequest{Messages: turns(10)}, next); err != nil {
		t.Fatalf("decider error failed the call: %v", err)
	}
	if calls != 1 {
		t.Fatalf("next called %d times", calls)
	}
}

func TestCompressionLevelUsed(t *testing.T) {
	in := turns(6)
	c := Compression{Decider: answerDecider("ultra", false), Keep: 1}
	var got *agents.ModelRequest
	if _, err := c.WrapModelCall(context.Background(), &agents.ModelRequest{Messages: in}, capture(&got)); err != nil {
		t.Fatal(err)
	}
	if want := compress.Compress(verbose, compress.Ultra); got.Messages[0].Text() != want {
		t.Fatalf("got %q, want ultra %q", got.Messages[0].Text(), want)
	}
}
