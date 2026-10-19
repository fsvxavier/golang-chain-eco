package kag_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/kag"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
)

// engFake answers the planner (tool "Plan"), the verifier (tool "Verdict") and
// the synthesis (no tools) with scripted replies.
func engFake(plan, verdict, answer string) *models.Fake {
	return &models.Fake{Func: func(_ []messages.Message, o models.Options) (messages.Message, error) {
		if len(o.Tools) == 0 {
			return messages.AI(answer), nil
		}
		body := plan
		if o.Tools[0].Name == "Verdict" {
			body = verdict
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(body), &args); err != nil {
			return messages.AI(body), nil // no tool call: structured output must fail
		}
		return messages.AI("", messages.ToolCall{ID: "t1", Name: o.Tools[0].Name, Args: args}), nil
	}}
}

// engGraph builds Dr.Silva -PRESCREVEU-> X -INTERAGE_COM-> Y -EM-> Paciente.
func engGraph(t *testing.T, triples ...[3]string) *kag.Memory {
	t.Helper()
	g := kag.NewMemory()
	ctx := context.Background()
	ents := []kag.Entity{
		{ID: "dr_silva", Type: "Medico", Name: "Dr. Silva"},
		{ID: "x", Type: "Droga", Name: "Droga X"},
		{ID: "y", Type: "Droga", Name: "Droga Y"},
		{ID: "paciente", Type: "Pessoa", Name: "Paciente"},
	}
	if err := g.UpsertEntities(ctx, ents); err != nil {
		t.Fatal(err)
	}
	var rels []kag.Relation
	for _, tr := range triples {
		rels = append(rels, kag.Relation{From: tr[0], Type: tr[1], To: tr[2], Source: "doc-1"})
	}
	if err := g.UpsertRelations(ctx, rels); err != nil {
		t.Fatal(err)
	}
	return g
}

var engChain = [][3]string{
	{"dr_silva", "PRESCREVEU", "x"},
	{"x", "INTERAGE_COM", "y"},
	{"y", "EM", "paciente"},
}

const engThreeStepPlan = `{"steps":[
 {"id":"s1","goal":"o que o médico prescreveu","entity":"Dr. Silva","relations":["PRESCREVEU"],"dir":"out"},
 {"id":"s2","goal":"com quem interage","entity":"$s1","relations":["INTERAGE_COM"],"dir":"out"},
 {"id":"s3","goal":"em qual paciente","entity":"$s2","relations":["EM"],"dir":"out"}]}`

type engChunks struct {
	calls int
	ans   knowledge.Answer
}

func (c *engChunks) Answer(context.Context, knowledge.Question) (knowledge.Answer, error) {
	c.calls++
	return c.ans, nil
}

func engSourceIDs(a knowledge.Answer) []string {
	var ids []string
	for _, s := range a.Sources {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestEngineMultiHopAnswers(t *testing.T) {
	m := engFake(engThreeStepPlan, "", "Dr. Silva prescreveu X, que interage com Y, em Paciente. [1][2][3]")
	e := &kag.Engine{Model: m, Graph: engGraph(t, engChain...)}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "O que o Dr. Silva prescreveu chega ao paciente?"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Strategy != "kag" {
		t.Fatalf("strategy %q", ans.Strategy)
	}
	if len(ans.Trace) != 3 {
		t.Fatalf("trace %+v", ans.Trace)
	}
	if len(ans.Sources) != 3 {
		t.Fatalf("sources %v", engSourceIDs(ans))
	}
	for _, id := range []string{"dr_silva|PRESCREVEU|x", "x|INTERAGE_COM|y", "y|EM|paciente"} {
		if !strings.Contains(strings.Join(engSourceIDs(ans), " "), id) {
			t.Errorf("missing triple %s in %v", id, engSourceIDs(ans))
		}
	}
	if ans.Sources[0].Metadata["source"] != "doc-1" {
		t.Errorf("provenance lost: %+v", ans.Sources[0].Metadata)
	}
	if !strings.Contains(ans.Text, "Paciente") {
		t.Errorf("text %q", ans.Text)
	}
}

func TestEngineCycleNoRepeatedTriples(t *testing.T) {
	g := kag.NewMemory()
	ctx := context.Background()
	_ = g.UpsertEntities(ctx, []kag.Entity{{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta"}})
	_ = g.UpsertRelations(ctx, []kag.Relation{
		{From: "a", Type: "R", To: "b"}, {From: "b", Type: "R", To: "a"}, {From: "a", Type: "R", To: "a"},
	})
	plan := `{"steps":[{"id":"s1","entity":"Alpha","dir":"both"}]}`
	e := &kag.Engine{Model: engFake(plan, "", "ok"), Graph: g, MaxHops: 10}
	ans, err := e.Answer(ctx, knowledge.Question{Text: "ciclo?"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range engSourceIDs(ans) {
		if seen[id] {
			t.Fatalf("triple repeated: %s in %v", id, engSourceIDs(ans))
		}
		seen[id] = true
	}
	if len(seen) != 3 {
		t.Fatalf("want 3 distinct triples, got %v", engSourceIDs(ans))
	}
}

func TestEngineMaxHopsLimits(t *testing.T) {
	g := kag.NewMemory()
	ctx := context.Background()
	_ = g.UpsertEntities(ctx, []kag.Entity{{ID: "a", Name: "Alpha"}})
	_ = g.UpsertRelations(ctx, []kag.Relation{
		{From: "a", Type: "R", To: "b"}, {From: "b", Type: "R", To: "c"}, {From: "c", Type: "R", To: "d"},
	})
	plan := `{"steps":[{"id":"s1","entity":"Alpha","dir":"out"}]}`
	e := &kag.Engine{Model: engFake(plan, "", "ok"), Graph: g, MaxHops: 2}
	ans, err := e.Answer(ctx, knowledge.Question{Text: "saltos"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Sources) != 2 {
		t.Fatalf("MaxHops=2 should give 2 triples, got %v", engSourceIDs(ans))
	}
}

func TestEngineFallsBackToChunks(t *testing.T) {
	plan := `{"steps":[{"id":"s1","goal":"detalhe do protocolo","entity":"Protocolo Z"}]}`
	chunks := &engChunks{ans: knowledge.Answer{Sources: []knowledge.Source{{ID: "doc-7", Content: "Protocolo Z: ..."}}}}
	e := &kag.Engine{Model: engFake(plan, "", "segundo o trecho [1]"), Graph: kag.NewMemory(), Chunks: chunks}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "protocolo Z?"})
	if err != nil {
		t.Fatal(err)
	}
	if chunks.calls != 1 {
		t.Fatalf("chunks called %d times", chunks.calls)
	}
	if len(ans.Sources) != 1 || ans.Sources[0].ID != "doc-7" {
		t.Fatalf("sources %v", engSourceIDs(ans))
	}
}

func TestEngineVerifyFlagsUnsupported(t *testing.T) {
	plan := `{"steps":[{"id":"s1","entity":"Dr. Silva","dir":"out"}]}`
	verdict := `{"supported":false,"unsupported":["X mata Paciente"]}`
	e := &kag.Engine{
		Model:  engFake(plan, verdict, "X mata o paciente"),
		Graph:  engGraph(t, engChain...),
		Verify: true,
	}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "X mata?"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ans.Text, "(não verificado)") {
		t.Fatalf("text not flagged: %q", ans.Text)
	}
	last := ans.Trace[len(ans.Trace)-1]
	if last.Name != "unverified" || !strings.Contains(last.Detail, "X mata Paciente") {
		t.Fatalf("trace %+v", ans.Trace)
	}
}

func TestEngineVerifyPassesSupported(t *testing.T) {
	plan := `{"steps":[{"id":"s1","entity":"Dr. Silva","dir":"out"}]}`
	e := &kag.Engine{
		Model:  engFake(plan, `{"supported":true}`, "Dr. Silva prescreveu X"),
		Graph:  engGraph(t, engChain...),
		Verify: true,
	}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "?"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ans.Text, "não verificado") {
		t.Fatalf("supported answer flagged: %q", ans.Text)
	}
}

func TestEngineMaxStepsCaps(t *testing.T) {
	var steps []string
	for i := 0; i < 10; i++ {
		steps = append(steps, `{"id":"s`+string(rune('a'+i))+`","entity":"Dr. Silva","dir":"out"}`)
	}
	plan := `{"steps":[` + strings.Join(steps, ",") + `]}`
	e := &kag.Engine{Model: engFake(plan, "", "ok"), Graph: engGraph(t, engChain...)}
	ans, err := e.Answer(context.Background(), knowledge.Question{Text: "?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Trace) != 4 {
		t.Fatalf("default MaxSteps 4, got %d steps", len(ans.Trace))
	}
}

func TestEngineEmptyQuestion(t *testing.T) {
	e := &kag.Engine{Model: &models.Fake{}, Graph: kag.NewMemory()}
	_, err := e.Answer(context.Background(), knowledge.Question{Text: "   "})
	if !errors.Is(err, knowledge.ErrEmptyQuestion) {
		t.Fatalf("got %v", err)
	}
}

func TestEngineBadPlanJSON(t *testing.T) {
	e := &kag.Engine{Model: engFake("isto não é json", "", "ok"), Graph: kag.NewMemory()}
	_, err := e.Answer(context.Background(), knowledge.Question{Text: "?"})
	if err == nil || !strings.Contains(err.Error(), "kag: plan") {
		t.Fatalf("want plan error, got %v", err)
	}
}
