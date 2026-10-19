package retrievers

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
)

func fixed(ids ...string) Retriever {
	return Func(func(context.Context, string) ([]documents.Document, error) {
		out := make([]documents.Document, len(ids))
		for i, id := range ids {
			out[i] = documents.Document{ID: id, PageContent: id}
		}
		return out, nil
	})
}

// Two retrievers that each rank a different document first tie on fused score;
// the tie must resolve by first-seen order on every run.
func TestEnsembleTiesAreDeterministic(t *testing.T) {
	for run := 0; run < 50; run++ {
		got, err := Ensemble([]Retriever{fixed("a", "b"), fixed("b", "a")}, nil).Invoke(context.Background(), "q")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
			t.Fatalf("run %d: order = %v, want [a b]", run, []string{got[0].ID, got[1].ID})
		}
	}
}

func TestEnsembleWeightsRankFirst(t *testing.T) {
	got, _ := Ensemble([]Retriever{fixed("a", "b"), fixed("b", "a")}, []float64{1, 3}).Invoke(context.Background(), "q")
	if got[0].ID != "b" {
		t.Fatalf("heavier retriever should win, got %s first", got[0].ID)
	}
}
