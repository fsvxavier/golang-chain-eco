// router: one knowledge.Router in front of RAG, CAG and KAG. Each question
// lands on a different technique: a "quem ... relaciona" question goes to the
// graph, a plain question over a small stable corpus goes to CAG, and the same
// question on a volatile corpus goes to RAG. Runs offline (scripted fake model).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/cag"
	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/kag"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

const (
	qKag   = "Quem prescreveu um remédio que interage com a Aspirina prescrita a João?"
	qPlain = "Qual remédio foi prescrito a João?"
)

var chunks = []documents.Document{
	{ID: "chunk-1", PageContent: "Dr. Silva prescreveu Varfarina ao paciente João."},
	{ID: "chunk-2", PageContent: "Varfarina interage com Aspirina."},
	{ID: "chunk-3", PageContent: "A Aspirina foi prescrita a João em março."},
}

var extractions = map[string]string{
	"Dr. Silva prescreveu Varfarina ao paciente João.": `{"entities":[{"name":"Dr. Silva","type":"Medico"},{"name":"Varfarina","type":"Droga"},{"name":"João","type":"Paciente"}],"relations":[{"from":"Dr. Silva","type":"PRESCREVEU","to":"Varfarina"},{"from":"Varfarina","type":"PARA_PACIENTE","to":"João"}]}`,
	"Varfarina interage com Aspirina.":                 `{"entities":[{"name":"Varfarina","type":"Droga"},{"name":"Aspirina","type":"Droga"}],"relations":[{"from":"Varfarina","type":"INTERAGE_COM","to":"Aspirina"}]}`,
	"A Aspirina foi prescrita a João em março.":        `{"entities":[{"name":"Aspirina","type":"Droga"},{"name":"João","type":"Paciente"}],"relations":[{"from":"Aspirina","type":"PRESCRITA_A","to":"João"}]}`,
}

var plans = map[string]string{
	qKag: `{"steps":[
		{"id":"s1","goal":"o que foi prescrito a João","entity":"Aspirina","relations":["PRESCRITA_A"],"dir":"out"},
		{"id":"s2","goal":"com que a Aspirina interage","entity":"Aspirina","relations":["INTERAGE_COM"],"dir":"both"},
		{"id":"s3","goal":"quem prescreveu","entity":"$s2","relations":["PRESCREVEU"],"dir":"in"}]}`,
}

// scripted is a synthesis reply; need lists terms (lower case) the context must contain.
type scripted struct {
	text string
	need []string
}

var answers = map[string]scripted{
	qKag:   {"Dr. Silva prescreveu a Varfarina, que interage com a Aspirina prescrita a João. [1][2][3]", []string{"silva", "varfarina", "aspirina"}},
	qPlain: {"A Varfarina foi prescrita a João. [1]", []string{"varfarina"}},
}

// reply plays every model role: extraction, planning, synthesis and verification.
func reply(msgs []messages.Message) messages.Message {
	sys, q := msgs[0].Content, demo.LastHuman(msgs)
	switch {
	case strings.Contains(sys, "Extract a knowledge graph"):
		return call("extraction", extractions[q])
	case strings.Contains(sys, "Você planeja"):
		return call("Plan", plans[q])
	case strings.Contains(sys, "Verifique se"):
		return call("Verdict", `{"supported":true,"unsupported":[]}`)
	}
	s, ok := answers[q]
	if !ok {
		return messages.AI("Não sei.")
	}
	var ctx strings.Builder
	for _, m := range msgs[:len(msgs)-1] {
		ctx.WriteString(strings.ToLower(m.Content))
	}
	for _, term := range s.need {
		if !strings.Contains(ctx.String(), term) {
			return messages.AI("Não sei com base no contexto.")
		}
	}
	return messages.AI(s.text)
}

func call(name, body string) messages.Message {
	var args map[string]any
	if err := json.Unmarshal([]byte(body), &args); err != nil {
		panic(err) // programmer error in the scripted replies
	}
	return messages.AI("", messages.ToolCall{ID: name, Name: name, Args: args})
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	ctx := context.Background()
	m := demo.Smart(reply)

	// Shared data: the chunks feed the vector store (RAG), the cached corpus (CAG)
	// and the extracted graph (KAG).
	vs := vectorstores.NewMemory(embeddings.Fake{Dim: 128})
	_, err := vs.AddDocuments(ctx, chunks)
	must(err)
	g := kag.NewMemory()
	_, err = (&kag.Extractor{Model: m, Embedder: embeddings.Fake{}, Graph: g}).Extract(ctx, chunks)
	must(err)

	var texts []string
	for _, c := range chunks {
		texts = append(texts, c.PageContent)
	}
	stable := knowledge.Profile{
		HasGraph:      true,
		CorpusTokens:  knowledge.Heuristic{}.Count(strings.Join(texts, " ")),
		ContextWindow: 200000,
	}
	r := &knowledge.Router{
		RAG:     &rag.Pipeline{Store: vs, Model: m, K: 3},
		CAG:     &cag.Engine{Model: m, Corpus: cag.NewCorpus(chunks)},
		KAG:     &kag.Engine{Model: m, Graph: g},
		Profile: stable,
	}

	ask := func(title string, rt *knowledge.Router, q knowledge.Question) {
		demo.Title(title)
		fmt.Printf("pergunta: %s\n", q.Text)
		ans, err := rt.Answer(ctx, q)
		must(err)
		fmt.Printf("resposta: %s\n", ans.Text)
		for _, s := range ans.Trace {
			if s.Name == "route" || s.Name == "fallback" {
				fmt.Printf("%s: %s\n", s.Name, s.Detail)
			}
		}
	}

	ask("1. Pergunta multi-hop: grafo", r, knowledge.Question{Text: qKag})
	ask("2. Pergunta simples: corpus pequeno e estável", r, knowledge.Question{Text: qPlain})

	volatile := *r
	volatile.Profile.Volatile = true
	ask("3. Mesma pergunta, corpus volátil", &volatile, knowledge.Question{Text: qPlain})

	ask("4. Force kag", r, knowledge.Question{Text: qKag, Force: "kag"})

	broken := &knowledge.Router{
		RAG:      r.RAG,
		CAG:      r.CAG,
		KAG:      &kag.Engine{Model: m}, // no Graph: the engine fails
		Profile:  stable,
		Fallback: true,
	}
	// No Force here: Router passes the question unchanged to RAG on fallback, and
	// rag.Pipeline rejects any Force other than "rag".
	ask("5. Fallback: kag falha, rag responde", broken, knowledge.Question{Text: qKag})
}
