// vector_indexes: the two in-process indexes (the Go counterparts of FAISS
// IndexFlat and Annoy): exact search vs an approximate forest, with recall
// measured against the exact answer, plus saving and loading the exact index.
// Runs offline.
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
)

func randVec(rng *rand.Rand, n int) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = float32(rng.NormFloat64())
	}
	return v
}

func main() {
	ctx, rng := context.Background(), rand.New(rand.NewSource(7))
	flat, ann := vectorstore.NewFlat(), &vectorstore.Annoy{Trees: 20, Seed: 7}
	var recs []vectorstore.Record
	for i := range 5000 {
		recs = append(recs, vectorstore.Record{ID: fmt.Sprint(i), Vector: randVec(rng, 32), Text: fmt.Sprintf("item %d", i)})
	}
	_ = flat.Upsert(ctx, recs)
	_ = ann.Upsert(ctx, recs)

	queries := make([][]float32, 100)
	for i := range queries {
		queries[i] = randVec(rng, 32)
	}
	recall := func(searchK int) float64 {
		ann.SearchK = searchK
		hit, total := 0, 0
		for _, q := range queries {
			want, _ := flat.Query(ctx, q, 10)
			got, _ := ann.Query(ctx, q, 10)
			have := map[string]bool{}
			for _, h := range got {
				have[h.ID] = true
			}
			for _, h := range want {
				total++
				if have[h.ID] {
					hit++
				}
			}
		}
		return 100 * float64(hit) / float64(total)
	}
	// SearchK = candidates inspected: more candidates, better recall, slower queries.
	// (Random 32-d Gaussian vectors are a hard case for any approximate index.)
	fmt.Printf("forest, default SearchK : recall@10 = %.0f%%\n", recall(0))
	fmt.Printf("forest, SearchK = 1500  : recall@10 = %.0f%%\n", recall(1500))

	path := filepath.Join(os.TempDir(), "flat-index.gob")
	defer os.Remove(path)
	if err := flat.Save(path); err != nil {
		log.Fatal(err)
	}
	loaded, err := vectorstore.LoadFlat(path)
	if err != nil {
		log.Fatal(err)
	}
	q := randVec(rng, 32)
	a, _ := flat.Query(ctx, q, 1)
	b, _ := loaded.Query(ctx, q, 1)
	fmt.Printf("saved and reloaded: same nearest neighbour = %v (%s)\n", a[0].ID == b[0].ID, b[0].Text)
}
