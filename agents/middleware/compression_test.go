package middleware

import (
	"context"
	"errors"
	"fmt"
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
	return decider(level, true, reply, false, false)
}

// decider answers each question by id: the level question picks level, the
// yes/no questions take the given flags.
func decider(level string, history, reply, ponytail, toon bool) compress.Decider {
	yes := map[string]bool{
		compress.QCompressHistory: history,
		compress.QCompressReply:   reply,
		compress.QPonytail:        ponytail,
		compress.QTOON:            toon,
	}
	return compress.FuncDecider(func(_ context.Context, _ compress.State, qs []compress.Question) ([]compress.Answer, error) {
		out := make([]compress.Answer, 0, len(qs))
		for _, q := range qs {
			if q.ID == compress.QLevel {
				out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, Pick: level, Dist: map[string]float64{level: 1}})
				continue
			}
			p := 0.0
			if yes[q.ID] {
				p = 1
			}
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, True: yes[q.ID], P: p})
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

func TestNewCompressionGuardsDecider(t *testing.T) {
	c := NewCompression(answerDecider("ultra", false))
	if _, ok := c.Decider.(compress.Guarded); !ok {
		t.Fatalf("NewCompression decider = %T, want compress.Guarded", c.Decider)
	}
}

const usersJSON = `{"users":[{"id":1,"name":"Ada","role":"admin"},{"id":2,"name":"Bob","role":"user"}]}`

func TestCompressionPonytailNote(t *testing.T) {
	in := turns(3)
	in = append(in, messages.Human("question"))
	req := &agents.ModelRequest{System: "be helpful", Messages: in}
	levels := map[string]compress.Level{"lite": compress.Lite, "full": compress.Full, "ultra": compress.Ultra, "bogus": compress.Full}
	for name, lvl := range levels {
		c := Compression{Decider: decider(name, false, false, true, false), Keep: 4}
		want := "question\n\n" + compress.PonytailDirective(lvl)
		var got *agents.ModelRequest
		for call := 0; call < 2; call++ { // repeated calls with the same request
			if _, err := c.WrapModelCall(context.Background(), req, capture(&got)); err != nil {
				t.Fatal(err)
			}
			last := len(got.Messages) - 1
			if got.Messages[last].Text() != want {
				t.Fatalf("level %s call %d: last human = %q, want %q", name, call, got.Messages[last].Text(), want)
			}
			if got.System != "be helpful" {
				t.Fatalf("system prompt changed: %q", got.System)
			}
			for i := range in[:last] {
				if !reflect.DeepEqual(got.Messages[i], in[i]) {
					t.Fatalf("level %s: message %d changed", name, i)
				}
			}
		}
	}
	if !reflect.DeepEqual(in[len(in)-1], messages.Human("question")) {
		t.Fatal("input mutated with ponytail note")
	}
}

func TestCompressionTOONToolResults(t *testing.T) {
	in := []messages.Message{
		messages.Human("list users"),
		messages.AI("", messages.ToolCall{ID: "c1", Name: "users"}, messages.ToolCall{ID: "c2", Name: "text"}, messages.ToolCall{ID: "c3", Name: "parts"}, messages.ToolCall{ID: "c4", Name: "tiny"}),
		messages.Tool(usersJSON, "c1"),
		messages.Tool("plain text output", "c2"),
		{Role: messages.RoleTool, Content: usersJSON, ToolCallID: "c3", Parts: []messages.ContentPart{{Type: "text", Text: usersJSON}}},
		messages.Tool("[1]", "c4"),
	}
	orig := append([]messages.Message(nil), in...)
	c := Compression{Decider: decider("full", false, false, false, true), Keep: 4}
	var got *agents.ModelRequest
	if _, err := c.WrapModelCall(context.Background(), &agents.ModelRequest{Messages: in}, capture(&got)); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != len(in) {
		t.Fatalf("len changed: %d -> %d", len(in), len(got.Messages))
	}
	want := "users[2]{id,name,role}:\n  1,Ada,admin\n  2,Bob,user"
	if got.Messages[2].Content != want {
		t.Fatalf("json tool result = %q, want TOON %q", got.Messages[2].Content, want)
	}
	if got.Messages[2].ToolCallID != "c1" {
		t.Fatalf("tool call id changed: %q", got.Messages[2].ToolCallID)
	}
	if got.Messages[3].Content != "plain text output" {
		t.Fatalf("non-JSON tool result changed: %q", got.Messages[3].Content)
	}
	if !reflect.DeepEqual(got.Messages[4], orig[4]) {
		t.Fatalf("tool result with Parts changed: %+v", got.Messages[4])
	}
	if got.Messages[5].Content != "[1]" {
		t.Fatalf("not-shorter JSON tool result changed: %q", got.Messages[5].Content)
	}
	for i := range in {
		if got.Messages[i].Role != in[i].Role || got.Messages[i].ToolCallID != in[i].ToolCallID || len(got.Messages[i].ToolCalls) != len(in[i].ToolCalls) {
			t.Fatalf("message %d role, id or calls changed", i)
		}
	}
	if !reflect.DeepEqual(in, orig) {
		t.Fatal("input mutated by TOON rewrite")
	}
}

func TestCompressionBothDirectivesOrder(t *testing.T) {
	in := turns(2)
	in = append(in, messages.Human("question"))
	c := Compression{Decider: decider("full", false, true, true, false), Keep: 4}
	var got *agents.ModelRequest
	if _, err := c.WrapModelCall(context.Background(), &agents.ModelRequest{Messages: in}, capture(&got)); err != nil {
		t.Fatal(err)
	}
	want := "question\n\n" + compress.PonytailDirective(compress.Full) + "\n\n" + compress.ReplyDirective
	if last := got.Messages[len(got.Messages)-1].Text(); last != want {
		t.Fatalf("last human = %q, want %q", last, want)
	}
}

func TestCompressionDeciderNoLeavesUntouched(t *testing.T) {
	in := turns(6)
	in = append(in, messages.Human("question"))
	orig := append([]messages.Message(nil), in...)
	req := &agents.ModelRequest{Messages: in}
	c := Compression{Decider: decider("ultra", false, false, false, false), Keep: 1}
	calls := 0
	next := func(_ context.Context, r *agents.ModelRequest) (messages.Message, error) {
		calls++
		if r != req {
			t.Fatal("request copied although nothing applies")
		}
		return messages.AI("ok"), nil
	}
	if _, err := c.WrapModelCall(context.Background(), req, next); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !reflect.DeepEqual(in, orig) {
		t.Fatal("request changed although every answer is no")
	}
}

// Compaction needs no model: the rules alone must shrink a long (over 4000 tokens), wordy history
// before the call, and leave a short one alone.
func TestCompactionDefaultShrinksLongHistory(t *testing.T) {
	size := func(ms []messages.Message) (n int) {
		for _, m := range ms {
			n += len(m.Text())
		}
		return
	}
	run := func(in []messages.Message) []messages.Message {
		var got []messages.Message
		_, err := Compaction().WrapModelCall(context.Background(), &agents.ModelRequest{Messages: in},
			func(_ context.Context, r *agents.ModelRequest) (messages.Message, error) {
				got = r.Messages
				return messages.AI("ok"), nil
			})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	long := append(turns(120), messages.Human(verbose)) // the rules compress above 4000 estimated tokens
	if got := run(long); size(got) >= size(long) {
		t.Fatalf("long history not shrunk: %d >= %d", size(got), size(long))
	}
	short := []messages.Message{messages.Human(verbose)}
	if got := run(short); !reflect.DeepEqual(got, short) {
		t.Fatalf("a short history must pass untouched: %+v", got)
	}
}

func TestOldBigToolResultsAreElided(t *testing.T) {
	big := strings.Repeat("x", 10000)
	msgs := []messages.Message{{Role: messages.RoleHuman, Content: "go"}}
	for i := 0; i < 4; i++ {
		msgs = append(msgs, messages.Message{Role: messages.RoleTool, ToolCallID: fmt.Sprint(i), Content: big})
	}
	if n := elideOldToolResults(msgs); n != 2 {
		t.Fatalf("cut %d, want 2", n)
	}
	if len(msgs[1].Content) > 1200 || len(msgs[2].Content) > 1200 || len(msgs[3].Content) != 10000 || len(msgs[4].Content) != 10000 {
		t.Fatalf("wrong results elided: %d %d %d %d", len(msgs[1].Content), len(msgs[2].Content), len(msgs[3].Content), len(msgs[4].Content))
	}
}
