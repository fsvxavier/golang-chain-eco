package cag

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/providers/anthropic"
)

func testDocs() []documents.Document {
	return []documents.Document{
		{ID: "b", PageContent: "Segundo documento.", Metadata: map[string]any{"source": "manual.md"}},
		{ID: "a", PageContent: "Primeiro documento.", Metadata: map[string]any{"source": "faq.md"}},
		{ID: "c", PageContent: "Terceiro.", Metadata: map[string]any{"source": "manual.md"}},
	}
}

func reversed(docs []documents.Document) []documents.Document {
	out := make([]documents.Document, 0, len(docs))
	for i := len(docs) - 1; i >= 0; i-- {
		out = append(out, docs[i])
	}
	return out
}

func answerModel(t *testing.T, usage *messages.Usage) *models.Fake {
	t.Helper()
	return &models.Fake{Name: "anthropic:test", Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		m := messages.AI("resposta")
		m.Usage = usage
		return m, nil
	}}
}

func TestPrefixByteStable(t *testing.T) {
	m := answerModel(t, nil)
	e := &Engine{Model: m, Corpus: NewCorpus(testDocs())}
	if _, err := e.Answer(context.Background(), knowledge.Question{Text: "q1",
		History: []messages.Message{messages.Human("oi"), messages.AI("olá")}}); err != nil {
		t.Fatal(err)
	}
	e2 := &Engine{Model: m, Corpus: NewCorpus(reversed(testDocs()))}
	if _, err := e2.Answer(context.Background(), knowledge.Question{Text: "q2"}); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(m.Calls[0][0])
	b, _ := json.Marshal(m.Calls[1][0])
	if string(a) != string(b) {
		t.Fatalf("system prefix differs:\n%s\n%s", a, b)
	}
	if got := len(m.Calls[0]); got != 4 {
		t.Fatalf("messages = %d, want system+2 history+question", got)
	}
	if m.Calls[0][1].Role != messages.RoleHuman || m.Calls[0][3].Text() != "q1" {
		t.Fatalf("history must follow the system message: %+v", m.Calls[0])
	}
}

func TestCorpusOrderAndText(t *testing.T) {
	c := NewCorpus(testDocs())
	want := `<document source="faq.md">
Primeiro documento.
</document>
<document source="manual.md">
Segundo documento.
</document>
<document source="manual.md">
Terceiro.
</document>`
	if c.Text() != want {
		t.Fatalf("Text() =\n%s\nwant\n%s", c.Text(), want)
	}
	if NewCorpus(reversed(testDocs())).Hash() != c.Hash() {
		t.Fatal("hash depends on input order")
	}
}

func TestAnthropicCacheMarker(t *testing.T) {
	m := answerModel(t, nil)
	e := &Engine{Model: m, Corpus: NewCorpus(testDocs()), Cacher: AnthropicCache{}}
	if _, err := e.Answer(context.Background(), knowledge.Question{Text: "q"}); err != nil {
		t.Fatal(err)
	}
	parts := m.Calls[0][0].Parts
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("system parts = %+v", parts)
	}
	cc, ok := parts[0].Extra["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Fatalf("cache_control missing: %+v", parts[0].Extra)
	}
}

func TestStablePrefixHasNoMarker(t *testing.T) {
	sys := messages.System("x")
	got := StablePrefix{}.Mark(sys)
	if len(got.Parts) != 0 || got.Content != "x" {
		t.Fatalf("StablePrefix changed the message: %+v", got)
	}
	if _, ok := CacherFor("openai:gpt").(StablePrefix); !ok {
		t.Fatal("CacherFor non-anthropic should be StablePrefix")
	}
	if _, ok := CacherFor("anthropic:claude").(AnthropicCache); !ok {
		t.Fatal("CacherFor anthropic should be AnthropicCache")
	}
}

func TestTooLargeFallsBack(t *testing.T) {
	m := answerModel(t, nil)
	fb := answererFunc(func(ctx context.Context, q knowledge.Question) (knowledge.Answer, error) {
		return knowledge.Answer{Text: "via rag", Strategy: "rag"}, nil
	})
	e := &Engine{Model: m, Corpus: NewCorpus(testDocs()), Window: 50, Reserve: 10, Fallback: fb}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "via rag" || len(m.Calls) != 0 {
		t.Fatalf("fallback not used: %+v calls=%d", ans, len(m.Calls))
	}
	if len(ans.Trace) == 0 || !strings.Contains(ans.Trace[0].Detail, "window") {
		t.Fatalf("trace does not explain fallback: %+v", ans.Trace)
	}

	e.Fallback = nil
	if _, err := e.Answer(context.Background(), knowledge.Question{Text: "q"}); !errors.Is(err, ErrCorpusTooLarge) {
		t.Fatalf("err = %v, want ErrCorpusTooLarge", err)
	}
}

func TestSetCorpusHash(t *testing.T) {
	e := &Engine{Model: answerModel(t, nil), Corpus: NewCorpus(testDocs())}
	if e.SetCorpus(NewCorpus(reversed(testDocs()))) {
		t.Fatal("same corpus reported as changed")
	}
	if !e.SetCorpus(NewCorpus([]documents.Document{{ID: "z", PageContent: "outro"}})) {
		t.Fatal("different corpus reported as unchanged")
	}
}

func TestCacheHitTrace(t *testing.T) {
	m := answerModel(t, &messages.Usage{CacheReadTokens: 10})
	e := &Engine{Model: m, Corpus: NewCorpus(testDocs())}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "q"})
	if err != nil {
		t.Fatal(err)
	}
	var hit, hash bool
	for _, s := range ans.Trace {
		hit = hit || s.Name == "cache_hit"
		hash = hash || s.Name == "corpus_hash"
	}
	if !hit || !hash {
		t.Fatalf("trace = %+v", ans.Trace)
	}
	if ans.Strategy != "cag" || ans.Usage == nil || ans.Usage.CacheReadTokens != 10 {
		t.Fatalf("answer = %+v", ans)
	}
}

func TestEmptyCorpus(t *testing.T) {
	m := answerModel(t, nil)
	e := &Engine{Model: m, Corpus: NewCorpus(nil)}
	if _, err := e.Answer(context.Background(), knowledge.Question{Text: "q"}); !errors.Is(err, ErrEmptyCorpus) {
		t.Fatalf("err = %v, want ErrEmptyCorpus", err)
	}
	if len(m.Calls) != 0 {
		t.Fatal("model called with empty corpus")
	}
}

func TestAnthropicRequestHasCacheControl(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","model":"claude-test",`+
			`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",`+
			`"usage":{"input_tokens":3,"output_tokens":1,"cache_read_input_tokens":7}}`)
	}))
	defer srv.Close()

	chat := &anthropic.Chat{Model: "claude-test", APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	e := &Engine{Model: chat, Corpus: NewCorpus(testDocs()), Cacher: CacherFor(chat.ModelID())}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "pergunta"})
	if err != nil {
		t.Fatal(err)
	}
	system, ok := body["system"].([]any)
	if !ok || len(system) == 0 {
		t.Fatalf("no system blocks in request: %v", body)
	}
	block := system[0].(map[string]any)
	if _, ok := block["cache_control"]; !ok {
		t.Fatalf("cache_control missing on system block: %v", block)
	}
	if ans.Usage == nil || ans.Usage.CacheReadTokens != 7 {
		t.Fatalf("usage = %+v", ans.Usage)
	}
}

type answererFunc func(ctx context.Context, q knowledge.Question) (knowledge.Answer, error)

func (f answererFunc) Answer(ctx context.Context, q knowledge.Question) (knowledge.Answer, error) {
	return f(ctx, q)
}
