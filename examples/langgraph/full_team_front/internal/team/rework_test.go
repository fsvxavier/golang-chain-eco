package team

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func TestPartsNeedTwoOrMore(t *testing.T) {
	out := "DECISION: SPLIT\nSUBTASK|Telas|Listar e criar\nsubtask | Testes | Cobrir os estados\nSUBTASK||vazio\n"
	ps := parts(out)
	if len(ps) != 2 || ps[0].Title != "Telas" || ps[1].Desc != "Cobrir os estados" {
		t.Fatalf("%+v", ps)
	}
	if parts("SUBTASK|Só uma|parte") != nil {
		t.Fatal("one part is no split")
	}
}

func TestExhaustedTaskShowsEveryRejection(t *testing.T) {
	w := &work{Feedback: "last", Rejections: []string{"round 1: a", "round 2: b", "round 3: last"}}
	if got := rejectionsOf(w, false); got != "last" {
		t.Fatalf("%q", got)
	}
	if got := rejectionsOf(w, true); !strings.Contains(got, "round 1: a") || !strings.Contains(got, "round 3: last") {
		t.Fatalf("%q", got)
	}
}

func TestReopenBudgetIsPerRequest(t *testing.T) {
	m := &manager{}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("req-a"))
	for i := 0; i < MaxReopens; i++ {
		if !m.reopen(ctx) {
			t.Fatalf("reopen %d refused", i)
		}
	}
	if m.reopen(ctx) {
		t.Fatal("budget not enforced")
	}
	if !m.reopen(runnables.WithConfig(context.Background(), graph.Thread("req-b"))) {
		t.Fatal("another request shares the budget")
	}
}
