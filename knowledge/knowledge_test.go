package knowledge

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

func TestHeuristic(t *testing.T) {
	cases := []struct {
		h    Heuristic
		text string
		want int
	}{
		{Heuristic{}, "abcdefgh", 2},
		{Heuristic{}, "ação", 1},
		{Heuristic{}, "", 0},
		{Heuristic{CharsPerToken: 2}, "abcdefgh", 4},
		{Heuristic{}, "abcde", 2},
	}
	for _, c := range cases {
		if got := c.h.Count(c.text); got != c.want {
			t.Errorf("Heuristic%+v.Count(%q) = %d, want %d", c.h, c.text, got, c.want)
		}
	}
}

func TestPromptNumbersSources(t *testing.T) {
	srcs := []Source{{ID: "a", Content: "primeiro trecho"}, {ID: "b", Content: "segundo trecho"}}
	q := Question{Text: "pergunta?", History: []messages.Message{messages.Human("oi"), messages.AI("olá")}}
	msgs := Prompt(DefaultSystem, srcs, q)

	if msgs[0].Role != messages.RoleSystem {
		t.Fatalf("first message role = %v, want system", msgs[0].Role)
	}
	for _, want := range []string{"[1] primeiro trecho", "[2] segundo trecho"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Errorf("system prompt missing %q:\n%s", want, msgs[0].Content)
		}
	}
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4 (system + 2 history + question)", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != messages.RoleHuman || last.Content != "pergunta?" {
		t.Errorf("last message = %+v, want human question", last)
	}
}

func TestSourcesFromDocs(t *testing.T) {
	docs := []documents.Document{
		{ID: "d1", PageContent: "x", Metadata: map[string]any{"score": 0.75, "source": "s"}},
		{ID: "d2", PageContent: "y"},
	}
	srcs := SourcesFromDocs(docs)
	if srcs[0].ID != "d1" || srcs[0].Content != "x" || srcs[0].Score != 0.75 {
		t.Errorf("source[0] = %+v", srcs[0])
	}
	if srcs[1].Score != 0 {
		t.Errorf("source[1].Score = %v, want 0 without metadata score", srcs[1].Score)
	}
}

type fakeAnswerer struct{ got Question }

func (f *fakeAnswerer) Answer(_ context.Context, q Question) (Answer, error) {
	f.got = q
	return Answer{Text: "ok", Strategy: "fake"}, nil
}

func TestAsRunnable(t *testing.T) {
	f := &fakeAnswerer{}
	r := AsRunnable(f)
	ans, err := r.Invoke(context.Background(), Question{Text: "q", Force: "rag"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "ok" || ans.Strategy != "fake" {
		t.Errorf("answer = %+v", ans)
	}
	if f.got.Text != "q" || f.got.Force != "rag" {
		t.Errorf("answerer received %+v", f.got)
	}
}
