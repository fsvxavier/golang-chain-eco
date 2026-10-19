// cag: cache-augmented answers. The whole corpus goes into the system prompt
// as one byte-stable prefix, so a provider can cache it between calls. The
// example prints the corpus hash and token estimate, asks two questions (the
// second one reports a cache read, shown as the cache_hit trace step), then
// shrinks the context window so the engine falls back to a keyword answerer
// (the fallback trace step). Runs offline with a scripted model.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/cag"
	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
)

// corpus returns the five embedded documents (pt-BR, fictional Zefiro X200 router).
func corpus() []documents.Document {
	return []documents.Document{
		{ID: "manual", PageContent: "Para configurar o roteador Zefiro X200, conecte o cabo WAN à porta azul, ligue a energia e acesse o painel em 192.168.0.1 com o usuário admin.", Metadata: map[string]any{"source": "manual.md"}},
		{ID: "garantia", PageContent: "A garantia do Zefiro X200 é de dois anos para o hardware e de noventa dias para firmware de terceiros.", Metadata: map[string]any{"source": "garantia.md"}},
		{ID: "wifi", PageContent: "O Zefiro X200 transmite Wi-Fi 6 nas bandas de 2,4 GHz e 5 GHz e forma malha com até quatro nós para cobrir casas grandes.", Metadata: map[string]any{"source": "wifi.md"}},
		{ID: "firmware", PageContent: "Para atualizar o firmware, baixe a versão mais recente no portal, aguarde o roteador reiniciar e nunca desligue a energia durante a gravação.", Metadata: map[string]any{"source": "firmware.md"}},
		{ID: "erro-krx", PageContent: "Luzes piscando em âmbar indicam falha de sincronização. O código KRX-9921 aparece quando a tabela de rotas local está corrompida; mantenha o botão de reset pressionado por dez segundos.", Metadata: map[string]any{"source": "erros.md"}},
	}
}

// keywordAnswerer is the fallback used when the corpus does not fit: it
// returns the single document sharing the most words with the question.
type keywordAnswerer struct{ docs []documents.Document }

func (k keywordAnswerer) Answer(_ context.Context, q knowledge.Question) (knowledge.Answer, error) {
	best, bestScore := documents.Document{}, -1
	for _, d := range k.docs {
		score := overlap(q.Text, d.PageContent)
		if score > bestScore {
			best, bestScore = d, score
		}
	}
	return knowledge.Answer{
		Text:     best.PageContent,
		Sources:  knowledge.SourcesFromDocs([]documents.Document{best}),
		Strategy: "keyword",
		Trace:    []knowledge.Step{{Name: "keyword", Detail: fmt.Sprintf("best of %d documents", len(k.docs))}},
	}, nil
}

// overlap counts words of at least five letters shared by a and b.
func overlap(a, b string) int {
	b = strings.ToLower(b)
	n := 0
	for _, w := range strings.Fields(strings.ToLower(a)) {
		w = strings.Trim(w, "?.,;:!")
		if len(w) >= 5 && strings.Contains(b, w) {
			n++
		}
	}
	return n
}

// reply plays the model offline: it quotes the corpus document that shares
// the most words with the question.
func reply(msgs []messages.Message) messages.Message {
	q := demo.LastHuman(msgs)
	best, bestScore := "", -1
	for _, block := range strings.Split(msgs[0].Text(), "</document>") {
		j := strings.Index(block, "<document")
		if j < 0 {
			continue
		}
		text := strings.TrimSpace(block[j+strings.Index(block[j:], ">\n")+2:])
		if score := overlap(q, text); score > bestScore {
			best, bestScore = text, score
		}
	}
	return messages.AI(best)
}

func printAnswer(ans knowledge.Answer) {
	fmt.Println("answer:", ans.Text)
	fmt.Println("strategy:", ans.Strategy)
	for _, st := range ans.Trace {
		fmt.Printf("  trace: %s %s\n", st.Name, st.Detail)
	}
	if ans.Usage != nil {
		fmt.Printf("usage: input=%d cache_read=%d cache_creation=%d\n",
			ans.Usage.InputTokens, ans.Usage.CacheReadTokens, ans.Usage.CacheCreationTokens)
	}
	fmt.Println("sources:")
	for i, s := range ans.Sources {
		fmt.Printf("  [%d] %s (%v)\n", i+1, s.ID, s.Metadata["source"])
	}
}

func main() {
	ctx := context.Background()
	docs := corpus()
	corp := cag.NewCorpus(docs)

	demo.Title("corpus")
	fmt.Println("hash:", corp.Hash())
	fmt.Println("tokens (estimate):", corp.Tokens(nil))

	// The fake reports the prompt as written on the first call and as a cache
	// read from the second call on, the way a provider with prefix caching does.
	calls := 0
	tokens := corp.Tokens(nil)
	model := demo.Smart(func(msgs []messages.Message) messages.Message {
		calls++
		m := reply(msgs)
		if calls == 1 {
			m.Usage = &messages.Usage{InputTokens: tokens, CacheCreationTokens: tokens}
		} else {
			m.Usage = &messages.Usage{InputTokens: 40, CacheReadTokens: tokens}
		}
		return m
	})

	engine := &cag.Engine{Model: model, Corpus: corp}

	demo.Title("question 1 (cold prefix)")
	ans, err := engine.Answer(ctx, knowledge.Question{Text: "Qual é a garantia do Zefiro X200?"})
	if err != nil {
		log.Fatal(err)
	}
	printAnswer(ans)

	demo.Title("question 2 (same prefix, cache read)")
	ans, err = engine.Answer(ctx, knowledge.Question{Text: "Como atualizar o firmware do roteador?"})
	if err != nil {
		log.Fatal(err)
	}
	printAnswer(ans)

	demo.Title("small window: fallback")
	small := &cag.Engine{Model: model, Corpus: corp, Window: 50, Reserve: 1, Fallback: keywordAnswerer{docs: docs}}
	ans, err = small.Answer(ctx, knowledge.Question{Text: "Como atualizar o firmware do roteador?"})
	if err != nil {
		log.Fatal(err)
	}
	printAnswer(ans)
}
