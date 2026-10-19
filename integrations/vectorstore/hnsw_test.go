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

func TestHNSW(t *testing.T) { conformance.Vector(t, vectorstore.NewHNSW()) }

func randRecs(rng *rand.Rand, n, dim int) []vectorstore.Record {
	recs := make([]vectorstore.Record, n)
	for i := range recs {
		v := make([]float32, dim)
		for j := range v {
			v[j] = float32(rng.NormFloat64())
		}
		recs[i] = vectorstore.Record{ID: fmt.Sprint(i), Vector: v}
	}
	return recs
}

func randVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for j := range v {
		v[j] = float32(rng.NormFloat64())
	}
	return v
}

// recallAt10 is the fraction of Flat's top-10 that the HNSW index also returns.
func recallAt10(t testing.TB, flat, h vectorstore.Backend, rng *rand.Rand, dim, queries int) float64 {
	ctx := context.Background()
	found, total := 0, 0
	for range queries {
		q := randVec(rng, dim)
		want, _ := flat.Query(ctx, q, 10)
		got, _ := h.Query(ctx, q, 10)
		in := map[string]bool{}
		for _, g := range got {
			in[g.ID] = true
		}
		for _, w := range want {
			total++
			if in[w.ID] {
				found++
			}
		}
	}
	return float64(found) / float64(total)
}

func TestHNSWRecallVsFlat(t *testing.T) {
	const n, dim = 5000, 32
	if testing.Short() {
		t.Skip("short")
	}
	rng, ctx := rand.New(rand.NewSource(1)), context.Background()
	recs := randRecs(rng, n, dim)
	flat := vectorstore.NewFlat()
	_ = flat.Upsert(ctx, recs)
	h := &vectorstore.HNSW{M: 16, EfConstruction: 64, Seed: 1}
	if err := h.Upsert(ctx, recs); err != nil {
		t.Fatal(err)
	}
	r := recallAt10(t, flat, h, rng, dim, 30)
	t.Logf("recall@10 (efSearch=%d, n=%d): %.3f", h.EfSearch(), n, r)
	if r < 0.95 {
		t.Fatalf("recall@10 %.3f < 0.95", r)
	}
}

func TestHNSWDeleteAndReupsert(t *testing.T) {
	ctx := context.Background()
	h := vectorstore.NewHNSW()
	_ = h.Upsert(ctx, []vectorstore.Record{
		{ID: "a", Vector: []float32{1, 0}, Text: "A"},
		{ID: "b", Vector: []float32{0, 1}, Text: "B"},
		{ID: "c", Vector: []float32{0.7, 0.7}, Text: "C"},
	})
	_ = h.Delete(ctx, []string{"a"})
	hits, _ := h.Query(ctx, []float32{1, 0}, 3)
	for _, x := range hits {
		if x.ID == "a" {
			t.Fatalf("deleted record returned: %+v", hits)
		}
	}
	// re-upsert moves "b" onto the query direction: the old vector must not match
	_ = h.Upsert(ctx, []vectorstore.Record{{ID: "b", Vector: []float32{1, 0}, Text: "B2"}})
	hits, _ = h.Query(ctx, []float32{1, 0}, 2)
	if len(hits) != 2 || hits[0].ID != "b" || hits[0].Text != "B2" || hits[0].Score < 0.999 {
		t.Fatalf("%+v", hits)
	}
	for _, x := range hits {
		if x.Text == "B" {
			t.Fatal("stale version of re-upserted record returned")
		}
	}
	_ = h.Delete(ctx, []string{"b", "c", "missing"})
	if hits, _ := h.Query(ctx, []float32{1, 0}, 5); len(hits) != 0 {
		t.Fatalf("empty index returned %+v", hits)
	}
}

func TestHNSWChurnKeepsResults(t *testing.T) {
	ctx, rng := context.Background(), rand.New(rand.NewSource(3))
	h := &vectorstore.HNSW{Seed: 3}
	live := map[string]bool{}
	recs := randRecs(rng, 400, 8)
	_ = h.Upsert(ctx, recs)
	for _, r := range recs {
		live[r.ID] = true
	}
	// delete most records so tombstones trigger a rebuild, then check only live ids come back
	for i := 0; i < 300; i++ {
		_ = h.Delete(ctx, []string{recs[i].ID})
		delete(live, recs[i].ID)
	}
	for range 20 {
		hits, _ := h.Query(ctx, randVec(rng, 8), 10)
		if len(hits) != 10 {
			t.Fatalf("want 10 hits, got %d", len(hits))
		}
		for _, x := range hits {
			if !live[x.ID] {
				t.Fatalf("dead record %s returned", x.ID)
			}
		}
	}
}

func TestHNSWSaveLoad(t *testing.T) {
	ctx := context.Background()
	h := &vectorstore.HNSW{Seed: 5}
	_ = h.Upsert(ctx, []vectorstore.Record{{ID: "a", Vector: []float32{1, 0}, Text: "A", Metadata: map[string]any{"k": "v"}}, {ID: "b", Vector: []float32{0, 1}}})
	_ = h.Delete(ctx, []string{"b"})
	p := filepath.Join(t.TempDir(), "idx.gob")
	if err := h.Save(p); err != nil {
		t.Fatal(err)
	}
	g, err := vectorstore.LoadHNSW(p)
	if err != nil {
		t.Fatal(err)
	}
	hits, _ := g.Query(ctx, []float32{1, 0}, 5)
	if len(hits) != 1 || hits[0].Text != "A" || hits[0].Metadata["k"] != "v" {
		t.Fatalf("%+v", hits)
	}
}

func BenchmarkQuery(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		rng, ctx := rand.New(rand.NewSource(1)), context.Background()
		recs := randRecs(rng, n, 32)
		flat := vectorstore.NewFlat()
		_ = flat.Upsert(ctx, recs)
		h := &vectorstore.HNSW{Seed: 1}
		_ = h.Upsert(ctx, recs)
		qs := make([][]float32, 64)
		for i := range qs {
			qs[i] = randVec(rng, 32)
		}
		for _, bk := range []struct {
			name string
			be   vectorstore.Backend
		}{{"Flat", flat}, {"HNSW", h}} {
			b.Run(fmt.Sprintf("%s/n=%d", bk.name, n), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_, _ = bk.be.Query(ctx, qs[i%len(qs)], 10)
				}
			})
		}
	}
}
