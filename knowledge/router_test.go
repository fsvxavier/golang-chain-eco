package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// stub is an Answerer that answers with its name, or fails with err.
type stub struct {
	name string
	err  error
}

func (s stub) Answer(_ context.Context, q Question) (Answer, error) {
	if s.err != nil {
		return Answer{}, s.err
	}
	return Answer{Text: s.name + ": " + q.Text, Trace: []Step{{Name: s.name, Detail: "ok"}}}, nil
}

var (
	errBoom    = errors.New("boom")
	ragStub    = stub{name: "rag"}
	cagStub    = stub{name: "cag"}
	kagStub    = stub{name: "kag"}
	smallCorpu = Profile{CorpusTokens: 1000, ContextWindow: 8000}
	bigCorpus  = Profile{CorpusTokens: 100000, ContextWindow: 8000}
	multiHop   = "quem é o responsável pelo módulo que depende da biblioteca X?"
	plain      = "qual é o prazo de entrega?"
)

func withGraph(p Profile) Profile { p.HasGraph = true; return p }

func TestRouterRoutes(t *testing.T) {
	cases := []struct {
		name string
		r    Router
		q    Question
		want string
	}{
		{"multi-hop com grafo vai para kag", Router{RAG: ragStub, CAG: cagStub, KAG: kagStub, Profile: withGraph(smallCorpu)}, Question{Text: multiHop}, "kag"},
		{"multi-hop sem grafo vai para cag", Router{RAG: ragStub, CAG: cagStub, KAG: kagStub, Profile: smallCorpu}, Question{Text: multiHop}, "cag"},
		{"corpus pequeno e estável vai para cag", Router{RAG: ragStub, CAG: cagStub, Profile: smallCorpu}, Question{Text: plain}, "cag"},
		{"corpus no limite de 80% vai para cag", Router{RAG: ragStub, CAG: cagStub, Profile: Profile{CorpusTokens: 6400, ContextWindow: 8000}}, Question{Text: plain}, "cag"},
		{"corpus acima de 80% vai para rag", Router{RAG: ragStub, CAG: cagStub, Profile: Profile{CorpusTokens: 6401, ContextWindow: 8000}}, Question{Text: plain}, "rag"},
		{"corpus grande vai para rag", Router{RAG: ragStub, CAG: cagStub, Profile: bigCorpus}, Question{Text: plain}, "rag"},
		{"corpus volátil vai para rag", Router{RAG: ragStub, CAG: cagStub, Profile: Profile{CorpusTokens: 1000, ContextWindow: 8000, Volatile: true}}, Question{Text: plain}, "rag"},
		{"corpus vazio nunca vai para cag", Router{RAG: ragStub, CAG: cagStub, Profile: Profile{ContextWindow: 8000}}, Question{Text: plain}, "rag"},
		{"force kag honrado", Router{RAG: ragStub, KAG: kagStub, Profile: bigCorpus}, Question{Text: plain, Force: "kag"}, "kag"},
		{"force rag honrado sobre cag", Router{RAG: ragStub, CAG: cagStub, Profile: smallCorpu}, Question{Text: plain, Force: "rag"}, "rag"},
		{"rag ausente usa a próxima disponível", Router{CAG: cagStub, Profile: bigCorpus}, Question{Text: plain}, "cag"},
		{"cag ausente cai em rag", Router{RAG: ragStub, Profile: smallCorpu}, Question{Text: plain}, "rag"},
		{"classificador LLM sim vai para kag", Router{RAG: ragStub, KAG: kagStub, Profile: withGraph(bigCorpus), Classifier: LLMClassifier{Model: models.NewFake(messages.AI("Sim"))}}, Question{Text: plain}, "kag"},
		{"classificador LLM não vai para rag", Router{RAG: ragStub, KAG: kagStub, Profile: withGraph(bigCorpus), Classifier: LLMClassifier{Model: models.NewFake(messages.AI("não"))}}, Question{Text: plain}, "rag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason, err := c.r.Route(context.Background(), c.q)
			if err != nil {
				t.Fatalf("Route error: %v", err)
			}
			if got != c.want {
				t.Errorf("strategy = %q (%s), want %q", got, reason, c.want)
			}
			if reason == "" {
				t.Error("reason is empty")
			}
		})
	}
}

func TestRouterErrors(t *testing.T) {
	cases := []struct {
		name    string
		r       Router
		q       Question
		wantNSt bool // expect ErrNoStrategy
		wantErr bool
	}{
		{"force kag sem KAG", Router{RAG: ragStub, Profile: bigCorpus}, Question{Text: plain, Force: "kag"}, true, true},
		{"force inválido", Router{RAG: ragStub, Profile: bigCorpus}, Question{Text: plain, Force: "graph"}, false, true},
		{"nenhuma técnica configurada", Router{Profile: bigCorpus}, Question{Text: plain}, true, true},
		{"classificador falha", Router{RAG: ragStub, KAG: kagStub, Profile: withGraph(bigCorpus), Classifier: LLMClassifier{}}, Question{Text: plain}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := c.r.Route(context.Background(), c.q)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if errors.Is(err, ErrNoStrategy) != c.wantNSt {
				t.Errorf("errors.Is(ErrNoStrategy) = %v, want %v (err %v)", !c.wantNSt, c.wantNSt, err)
			}
		})
	}
}

func TestRouterTraceFirstStep(t *testing.T) {
	r := &Router{RAG: ragStub, CAG: cagStub, Profile: smallCorpu}
	ans, err := r.Answer(context.Background(), Question{Text: plain})
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Trace) < 2 {
		t.Fatalf("trace = %+v, want route step plus answerer step", ans.Trace)
	}
	first := ans.Trace[0]
	if first.Name != "route" || !strings.HasPrefix(first.Detail, "cag — ") {
		t.Errorf("first trace step = %+v, want route with detail \"cag — <reason>\"", first)
	}
	if ans.Strategy != "cag" {
		t.Errorf("Strategy = %q, want cag", ans.Strategy)
	}
}

func TestRouterFallback(t *testing.T) {
	failingKAG := stub{name: "kag", err: errBoom}
	failingRAG := stub{name: "rag", err: errBoom}
	cases := []struct {
		name         string
		r            Router
		wantStrategy string
		wantErr      bool
	}{
		{"kag falha com fallback para rag", Router{RAG: ragStub, KAG: failingKAG, Profile: withGraph(smallCorpu), Fallback: true}, "rag", false},
		{"kag falha sem fallback retorna erro", Router{RAG: ragStub, KAG: failingKAG, Profile: withGraph(smallCorpu)}, "", true},
		{"fallback também falha retorna erro", Router{RAG: failingRAG, KAG: failingKAG, Profile: withGraph(smallCorpu), Fallback: true}, "", true},
		{"cag falha com fallback para rag", Router{RAG: ragStub, CAG: stub{name: "cag", err: errBoom}, Profile: smallCorpu, Fallback: true}, "rag", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ans, err := c.r.Answer(context.Background(), Question{Text: multiHop})
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if c.wantErr {
				if !errors.Is(err, errBoom) {
					t.Errorf("err = %v, want wrapping errBoom", err)
				}
				return
			}
			if ans.Strategy != c.wantStrategy {
				t.Errorf("Strategy = %q, want %q", ans.Strategy, c.wantStrategy)
			}
			found := false
			for _, s := range ans.Trace {
				if s.Name == "fallback" {
					found = true
				}
			}
			if !found {
				t.Errorf("trace %+v has no fallback step", ans.Trace)
			}
			if ans.Trace[0].Name != "route" {
				t.Errorf("first trace step = %+v, want route", ans.Trace[0])
			}
		})
	}
}

func TestKeywordClassifier(t *testing.T) {
	cases := map[string]bool{
		"Quem escreveu o livro?":                     true,
		"Como funciona o cache?":                     false,
		"Who wrote the design doc?":                  true,
		"Quais documentos RELACIONAM com o módulo?":  true,
		"Qual é a DEPENDÊNCIA do serviço X?":         true,
		"What is the PATH from A to B?":              true,
		"Explique o conceito de chunking":            false,
		"Entre os documentos, qual é o mais novo?":   true,
		"Which services depends on the cache layer?": true,
		"Mostre o caminho até a config":              true,
		"prazo de entrega do pedido":                 false,
		"Who is related to Ana, and between whom?":   true,
	}
	for q, want := range cases {
		got, err := KeywordClassifier{}.MultiHop(context.Background(), Question{Text: q})
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if got != want {
			t.Errorf("MultiHop(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestLLMClassifier(t *testing.T) {
	cases := []struct {
		answer  string
		want    bool
		wantErr bool
	}{
		{"Sim.", true, false},
		{"yes", true, false},
		{"Não", false, false},
		{"no, it is not", false, false},
		{"talvez", false, true},
	}
	for _, c := range cases {
		m := models.NewFake(messages.AI(c.answer))
		got, err := LLMClassifier{Model: m}.MultiHop(context.Background(), Question{Text: plain})
		if (err != nil) != c.wantErr {
			t.Fatalf("answer %q: err = %v, wantErr %v", c.answer, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("answer %q: got %v, want %v", c.answer, got, c.want)
		}
	}
}
