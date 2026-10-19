// kag: multi-hop questions that plain vector search cannot answer. A small
// medical-style corpus is turned into a knowledge graph; the question needs
// three hops across chunks, so the single best chunk is not enough and the
// graph engine walks the relations instead. Runs offline (scripted fake model).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/kag"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

const qKag = "Quem prescreveu um remédio que interage com a Aspirina prescrita a João?"

var chunks = []documents.Document{
	{ID: "chunk-1", PageContent: "Dr. Silva prescreveu Varfarina ao paciente João."},
	{ID: "chunk-2", PageContent: "Varfarina interage com Aspirina."},
	{ID: "chunk-3", PageContent: "A Aspirina foi prescrita a João em março."},
}

// extractions is what the fake model returns for each chunk (structured extraction).
var extractions = map[string]string{
	"Dr. Silva prescreveu Varfarina ao paciente João.": `{"entities":[{"name":"Dr. Silva","type":"Medico"},{"name":"Varfarina","type":"Droga"},{"name":"João","type":"Paciente"}],"relations":[{"from":"Dr. Silva","type":"PRESCREVEU","to":"Varfarina"},{"from":"Varfarina","type":"PARA_PACIENTE","to":"João"}]}`,
	"Varfarina interage com Aspirina.":                 `{"entities":[{"name":"Varfarina","type":"Droga"},{"name":"Aspirina","type":"Droga"}],"relations":[{"from":"Varfarina","type":"INTERAGE_COM","to":"Aspirina"}]}`,
	"A Aspirina foi prescrita a João em março.":        `{"entities":[{"name":"Aspirina","type":"Droga"},{"name":"João","type":"Paciente"}],"relations":[{"from":"Aspirina","type":"PRESCRITA_A","to":"João"}]}`,
}

// plans is what the fake planner returns for each question (structured planning).
var plans = map[string]string{
	qKag: `{"steps":[
		{"id":"s1","goal":"o que foi prescrito a João","entity":"Aspirina","relations":["PRESCRITA_A"],"dir":"out"},
		{"id":"s2","goal":"com que a Aspirina interage","entity":"Aspirina","relations":["INTERAGE_COM"],"dir":"both"},
		{"id":"s3","goal":"quem prescreveu","entity":"$s2","relations":["PRESCREVEU"],"dir":"in"}]}`,
	"Quem está ligado a Alpha em um ciclo?": `{"steps":[{"id":"s1","goal":"percorrer o ciclo","entity":"Alpha","relations":[],"dir":"both"}]}`,
}

// answers are the synthesis replies. need lists terms (lower case) the context
// must contain; without them the fake says it does not know.
type scripted struct {
	text string
	need []string
}

var answers = map[string]scripted{
	qKag:                                    {"Dr. Silva prescreveu a Varfarina, que interage com a Aspirina prescrita a João. [1][2][3]", []string{"silva", "varfarina", "aspirina"}},
	"Quem está ligado a Alpha em um ciclo?": {"Beta e Gamma formam um ciclo com Alpha. [1][2][3]", []string{"alpha", "beta", "gamma"}},
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

	g := kag.NewMemory()
	ex := &kag.Extractor{Model: m, Embedder: embeddings.Fake{}, Graph: g}
	stats, err := ex.Extract(ctx, chunks)
	must(err)
	demo.Title("1. Grafo extraído dos chunks")
	fmt.Printf("entities=%d relations=%d dropped=%d\n", stats.Entities, stats.Relations, stats.Dropped)

	vs := vectorstores.NewMemory(embeddings.Fake{Dim: 128})
	_, err = vs.AddDocuments(ctx, chunks)
	must(err)
	demo.Title("2. Busca vetorial sozinha (K=1)")
	fmt.Printf("pergunta: %s\n", qKag)
	top, err := retrievers.FromVectorStore(vs, retrievers.VectorStoreOptions{K: 1}).Invoke(ctx, qKag)
	must(err)
	for _, d := range top {
		fmt.Printf("chunk: %s\n", d.PageContent)
	}
	pipe := &rag.Pipeline{Store: vs, Model: m, K: 1}
	ra, err := pipe.Answer(ctx, knowledge.Question{Text: qKag})
	must(err)
	fmt.Printf("resposta: %s\n", ra.Text)

	demo.Title("3. kag.Engine (Verify) percorre o grafo")
	e := &kag.Engine{Model: m, Graph: g, Verify: true}
	ans, err := e.Answer(ctx, knowledge.Question{Text: qKag})
	must(err)
	fmt.Printf("resposta: %s\n", ans.Text)
	fmt.Println("trace:")
	for _, s := range ans.Trace {
		fmt.Printf("  %s: %s\n", s.Name, s.Detail)
	}
	fmt.Println("triplas usadas (fontes):")
	for _, s := range ans.Sources {
		fmt.Printf("  - %s (doc %v)\n", s.Content, s.Metadata["source"])
	}

	demo.Title("4. Grafo com ciclo: a travessia termina")
	cyc := kag.NewMemory()
	must(cyc.UpsertEntities(ctx, []kag.Entity{{ID: "alpha", Name: "Alpha"}, {ID: "beta", Name: "Beta"}, {ID: "gamma", Name: "Gamma"}}))
	must(cyc.UpsertRelations(ctx, []kag.Relation{
		{From: "alpha", Type: "LIGA", To: "beta", Source: "ciclo"},
		{From: "beta", Type: "LIGA", To: "gamma", Source: "ciclo"},
		{From: "gamma", Type: "LIGA", To: "alpha", Source: "ciclo"},
	}))
	qCycle := "Quem está ligado a Alpha em um ciclo?"
	ca, err := (&kag.Engine{Model: m, Graph: cyc, MaxHops: 10}).Answer(ctx, knowledge.Question{Text: qCycle})
	must(err)
	fmt.Printf("resposta: %s\n", ca.Text)
	for _, s := range ca.Trace {
		fmt.Printf("  %s: %s\n", s.Name, s.Detail)
	}
	fmt.Printf("arestas visitadas: %d (o ciclo não se repete)\n", len(ca.Sources))
}
