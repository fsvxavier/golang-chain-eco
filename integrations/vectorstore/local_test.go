package vectorstore_test

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
)

func TestFlat(t *testing.T)  { conformance.Vector(t, vectorstore.NewFlat()) }
func TestAnnoy(t *testing.T) { conformance.Vector(t, &vectorstore.Annoy{}) }

func TestFlatSaveLoad(t *testing.T) {
	f, ctx := vectorstore.NewFlat(), context.Background()
	_ = f.Upsert(ctx, []vectorstore.Record{{ID: "a", Vector: []float32{1, 0}, Text: "A", Metadata: map[string]any{"k": "v"}}})
	p := filepath.Join(t.TempDir(), "idx.gob")
	if err := f.Save(p); err != nil {
		t.Fatal(err)
	}
	g, err := vectorstore.LoadFlat(p)
	if err != nil {
		t.Fatal(err)
	}
	hits, _ := g.Query(ctx, []float32{1, 0}, 1)
	if len(hits) != 1 || hits[0].Text != "A" || hits[0].Metadata["k"] != "v" {
		t.Fatalf("%+v", hits)
	}
}

// Annoy must find most true neighbours on random data (recall check).
func TestAnnoyRecall(t *testing.T) {
	rng, ctx := rand.New(rand.NewSource(1)), context.Background()
	flat, ann := vectorstore.NewFlat(), &vectorstore.Annoy{Trees: 20, Seed: 1}
	var recs []vectorstore.Record
	for i := range 2000 {
		v := make([]float32, 16)
		for j := range v {
			v[j] = float32(rng.NormFloat64())
		}
		recs = append(recs, vectorstore.Record{ID: fmt.Sprint(i), Vector: v})
	}
	_ = flat.Upsert(ctx, recs)
	_ = ann.Upsert(ctx, recs)
	found, total := 0, 0
	for range 50 {
		q := make([]float32, 16)
		for j := range q {
			q[j] = float32(rng.NormFloat64())
		}
		want, _ := flat.Query(ctx, q, 10)
		got, _ := ann.Query(ctx, q, 10)
		in := map[string]bool{}
		for _, h := range got {
			in[h.ID] = true
		}
		for _, h := range want {
			total++
			if in[h.ID] {
				found++
			}
		}
	}
	if r := float64(found) / float64(total); r < 0.7 {
		t.Fatalf("recall %.2f", r)
	} else {
		t.Logf("recall %.2f", r)
	}
}
