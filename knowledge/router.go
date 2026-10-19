package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// ErrNoStrategy is returned when no configured strategy can answer the question.
var ErrNoStrategy = errors.New("knowledge: no strategy available")

// Profile describes the corpus and the context the router decides on.
type Profile struct {
	CorpusTokens  int
	ContextWindow int
	Volatile      bool
	HasGraph      bool
}

// Classifier decides whether a question needs multi-hop reasoning over relations.
type Classifier interface {
	MultiHop(ctx context.Context, q Question) (bool, error)
}

const multiHopPrompt = "Responda apenas 'sim' ou 'não'. " +
	"A pergunta exige encadear relações entre entidades (múltiplos saltos em um grafo)?"

// LLMClassifier asks a chat model a yes/no question about the question.
type LLMClassifier struct{ Model models.ChatModel }

// MultiHop implements Classifier.
func (c LLMClassifier) MultiHop(ctx context.Context, q Question) (bool, error) {
	if c.Model == nil {
		return false, errors.New("knowledge: LLMClassifier has no model")
	}
	out, err := c.Model.Generate(ctx, []messages.Message{messages.System(multiHopPrompt), messages.Human(q.Text)})
	if err != nil {
		return false, err
	}
	fields := strings.Fields(foldText(out.Text()))
	if len(fields) > 0 {
		switch strings.Trim(fields[0], ".,!;:") {
		case "sim", "yes":
			return true, nil
		case "nao", "no":
			return false, nil
		}
	}
	return false, fmt.Errorf("knowledge: unexpected classifier answer %q", out.Text())
}

// KeywordClassifier is a pt-BR/en heuristic: it looks for relation cues in the question.
type KeywordClassifier struct{}

var (
	cueExact  = map[string]bool{"quem": true, "entre": true, "who": true, "between": true, "path": true, "depends": true, "depende": true}
	cuePrefix = []string{"relaciona", "relacao", "relacoes", "related", "dependencia", "caminho"}
)

// MultiHop implements Classifier.
func (KeywordClassifier) MultiHop(_ context.Context, q Question) (bool, error) {
	toks := strings.FieldsFunc(foldText(q.Text), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, tok := range toks {
		if cueExact[tok] {
			return true, nil
		}
		for _, p := range cuePrefix {
			if strings.HasPrefix(tok, p) {
				return true, nil
			}
		}
	}
	return false, nil
}

var accentFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// foldText lowercases and strips the common Latin accents.
func foldText(s string) string { return accentFold.Replace(strings.ToLower(s)) }

// Router picks the strategy for each question and answers with it.
type Router struct {
	RAG, CAG, KAG Answerer
	Profile       Profile
	// Classifier defaults to KeywordClassifier when nil.
	Classifier Classifier
	// Fallback retries with RAG when the chosen technique returns an error.
	Fallback bool
}

// Answer implements Answerer. The first Trace step is the routing decision.
func (r *Router) Answer(ctx context.Context, q Question) (Answer, error) {
	strategy, reason, err := r.Route(ctx, q)
	if err != nil {
		return Answer{}, err
	}
	ans, err := r.get(strategy).Answer(ctx, q)
	var fallback *Step
	if err != nil && r.Fallback && strategy != "rag" && r.RAG != nil {
		fallback = &Step{Name: "fallback", Detail: fmt.Sprintf("%s → rag: %v", strategy, err)}
		strategy = "rag"
		ans, err = r.RAG.Answer(ctx, q)
	}
	if err != nil {
		return Answer{}, fmt.Errorf("knowledge: %s: %w", strategy, err)
	}
	ans.Strategy = strategy
	trace := []Step{{Name: "route", Detail: strategy + " — " + reason}}
	if fallback != nil {
		trace = append(trace, *fallback)
	}
	ans.Trace = append(trace, ans.Trace...)
	return ans, nil
}

// Route returns the strategy ("rag", "cag" or "kag") and the reason for it.
func (r *Router) Route(ctx context.Context, q Question) (strategy, reason string, err error) {
	switch q.Force {
	case "":
	case "rag", "cag", "kag":
		if r.get(q.Force) == nil {
			return "", "", fmt.Errorf("%w: %s is not configured", ErrNoStrategy, q.Force)
		}
		return q.Force, "forçada pela chamada", nil
	default:
		return "", "", fmt.Errorf("knowledge: invalid strategy %q", q.Force)
	}

	multi := false
	if r.Profile.HasGraph && r.KAG != nil {
		clf := r.Classifier
		if clf == nil {
			clf = KeywordClassifier{}
		}
		if multi, err = clf.MultiHop(ctx, q); err != nil {
			return "", "", fmt.Errorf("knowledge: classify: %w", err)
		}
	}

	p := r.Profile
	want, why := "rag", "demais casos"
	switch {
	case multi && r.KAG != nil:
		want, why = "kag", "pergunta multi-hop com grafo disponível"
	case !p.Volatile && p.CorpusTokens > 0 && float64(p.CorpusTokens) <= float64(p.ContextWindow)*0.8 && r.CAG != nil:
		want, why = "cag", "corpus pequeno e estável cabe na janela"
	}
	if r.get(want) != nil {
		return want, why, nil
	}
	for _, s := range []string{"rag", "cag", "kag"} {
		if r.get(s) != nil {
			return s, fmt.Sprintf("%s: %s indisponível", why, want), nil
		}
	}
	return "", "", ErrNoStrategy
}

func (r *Router) get(strategy string) Answerer {
	switch strategy {
	case "rag":
		return r.RAG
	case "cag":
		return r.CAG
	case "kag":
		return r.KAG
	}
	return nil
}
