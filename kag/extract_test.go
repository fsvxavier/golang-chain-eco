package kag_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/kag"
)

// fakeLLM answers each chunk (matched by its text) with a forced tool call.
// Chunks mapped to nil get a malformed payload, which fails structured decoding.
func fakeLLM(replies map[string]map[string]any) *models.Fake {
	return &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		last, _ := messages.Last(msgs)
		args, ok := replies[last.Content]
		if !ok {
			args = map[string]any{"entities": "not a list"}
		}
		return messages.AI("", messages.ToolCall{Name: "extraction", Args: args}), nil
	}}
}

func ents(pairs ...string) []any {
	var out []any
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, map[string]any{"name": pairs[i], "type": pairs[i+1]})
	}
	return out
}

func rels(triples ...string) []any {
	var out []any
	for i := 0; i < len(triples); i += 3 {
		out = append(out, map[string]any{"from": triples[i], "type": triples[i+1], "to": triples[i+2]})
	}
	return out
}

func extractor(m *kag.Memory, llm models.ChatModel, s kag.Schema) *kag.Extractor {
	return &kag.Extractor{Model: llm, Embedder: embeddings.Fake{}, Schema: s, Graph: m}
}

func TestExtractBuildsGraph(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	llm := fakeLLM(map[string]map[string]any{
		"Ana Silva works with Bruno.": {"entities": ents("Ana Silva", "Person", "Bruno", "Person"),
			"relations": rels("Ana Silva", "works with", "Bruno")},
		"Bruno lives in Lisboa.": {"entities": ents(), "relations": rels("Bruno", "lives_in", "Lisboa")},
	})
	docs := []documents.Document{
		{ID: "d1", PageContent: "Ana Silva works with Bruno."},
		{PageContent: "Bruno lives in Lisboa.", Metadata: map[string]any{"source": "f.md"}},
	}

	st, err := extractor(m, llm, kag.Schema{}).Extract(ctx, docs)
	if err != nil {
		t.Fatal(err)
	}
	if st != (kag.ExtractStats{Entities: 3, Relations: 2, Dropped: 0}) {
		t.Fatalf("stats = %+v", st)
	}
	ana, ok, _ := m.Entity(ctx, "ana_silva")
	if !ok || ana.Name != "Ana Silva" || ana.Type != "Person" || len(ana.Vector) == 0 {
		t.Fatalf("entity ana_silva = %+v ok=%v", ana, ok)
	}
	out, _ := m.Neighbors(ctx, "ana_silva", nil, kag.Out)
	if len(out) != 1 || out[0].To != "bruno" || out[0].Type != "WORKS_WITH" || out[0].Source != "d1" {
		t.Fatalf("out edges = %+v", out)
	}
	lisboa, _ := m.Neighbors(ctx, "bruno", []string{"LIVES_IN"}, kag.Out)
	if len(lisboa) != 1 || lisboa[0].Source != "f.md" {
		t.Fatalf("lives_in edge = %+v", lisboa)
	}
	if _, ok, _ := m.Entity(ctx, "lisboa"); !ok {
		t.Fatal("endpoint entity lisboa not created")
	}
}

func TestExtractNormalizesRelationType(t *testing.T) {
	hostile := []string{"trata com; DROP", "`MATCH (n) DETACH DELETE n`", "1st", " ;; "}
	safe := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	want := map[string]string{
		"trata com; DROP":             "TRATA_COM_DROP",
		"`MATCH (n) DETACH DELETE n`": "MATCH_N_DETACH_DELETE_N",
		"1st":                         "",
		" ;; ":                        "",
	}
	for _, h := range hostile {
		m := kag.NewMemory()
		llm := fakeLLM(map[string]map[string]any{
			"x": {"entities": ents(), "relations": rels("A", h, "B")},
		})
		st, err := extractor(m, llm, kag.Schema{}).Extract(context.Background(), []documents.Document{{PageContent: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		got := want[h]
		if got == "" {
			if st.Relations != 0 || st.Dropped != 1 {
				t.Fatalf("%q: stats = %+v, want dropped", h, st)
			}
			continue
		}
		if !safe.MatchString(got) {
			t.Fatalf("test expectation %q is not safe", got)
		}
		edges, _ := m.Neighbors(context.Background(), "a", nil, kag.Out)
		if len(edges) != 1 || edges[0].Type != got {
			t.Fatalf("%q: edges = %+v, want type %q", h, edges, got)
		}
	}
}

func TestExtractDropsDuplicatesAndEmpty(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	llm := fakeLLM(map[string]map[string]any{
		"x": {
			"entities":  ents("Ana", "Person", "ana", "Person", "  ", "Person"),
			"relations": rels("Ana", "knows", "Bruno", "Ana", "knows", "Bruno", "", "knows", "Bruno"),
		},
	})
	st, err := extractor(m, llm, kag.Schema{}).Extract(ctx, []documents.Document{{PageContent: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	// Entities: ana and bruno (implicit from the relation). Dropped: duplicate
	// "ana", blank entity, duplicate relation, relation without a source.
	if st != (kag.ExtractStats{Entities: 2, Relations: 1, Dropped: 4}) {
		t.Fatalf("stats = %+v", st)
	}
}

func TestExtractSchemaFilters(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	llm := fakeLLM(map[string]map[string]any{
		"x": {
			"entities":  ents("Ana", "Person"),
			"relations": rels("Ana", "works with", "Bruno", "Ana", "lives in", "Lisboa"),
		},
	})
	x := extractor(m, llm, kag.Schema{RelationTypes: []string{"works_with"}})
	st, err := x.Extract(ctx, []documents.Document{{PageContent: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if st != (kag.ExtractStats{Entities: 2, Relations: 1, Dropped: 1}) {
		t.Fatalf("stats = %+v", st)
	}
	if _, ok, _ := m.Entity(ctx, "lisboa"); ok {
		t.Fatal("entity from a rejected relation must not be created")
	}
}

func TestExtractBadJSONContinues(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	llm := fakeLLM(map[string]map[string]any{
		"good": {"entities": ents("Ana", "Person"), "relations": rels("Ana", "knows", "Bruno")},
	})
	docs := []documents.Document{
		{ID: "bad", PageContent: "bad chunk"},
		{ID: "good", PageContent: "good"},
	}
	st, err := extractor(m, llm, kag.Schema{}).Extract(ctx, docs)
	if err == nil || !strings.Contains(err.Error(), "chunk 0") {
		t.Fatalf("err = %v, want error naming chunk 0", err)
	}
	if st.Entities != 2 || st.Relations != 1 {
		t.Fatalf("stats = %+v, want the valid chunk stored", st)
	}
	if _, ok, _ := m.Entity(ctx, "ana"); !ok {
		t.Fatal("valid chunk not stored")
	}
}
