package ciam_test

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/ciam"
	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

// With an HNSW index the cosine ranking must give the same answer as the scan
// on the existing fixture (BM25 is unchanged, so the fused order matches too).
func TestVectorIndexMatchesScan(t *testing.T) {
	ctx, root := context.Background(), djangoProject(t)
	scan := newManager()
	idx := newManager()
	idx.IndexFactory = func() langmem.VectorIndex { return vectorstore.NewHNSW() }
	for _, m := range []*ciam.Manager{scan, idx} {
		if _, err := m.Index(ctx, root, ciam.IndexOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	reqs := []ciam.SearchRequest{
		{Project: "shop", Query: "stripe customer id", Limit: 3},
		{Project: "shop", Query: "invoice total cents", Limit: 3},
		{Project: "shop", Query: "send receipt email", Limit: 3},
		{Project: "shop", Query: "invoice", Types: []string{"task"}, Limit: 2},
		{Project: "shop", Query: "invoice viewset list", Types: []string{"model", "view"}, Limit: 1},
	}
	for _, req := range reqs {
		want, err := scan.Search(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		got, err := idx.Search(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) || len(want) == 0 {
			t.Fatalf("%q: index %d results, scan %d", req.Query, len(got), len(want))
		}
		for i := range want {
			if got[i].File != want[i].File || got[i].Index != want[i].Index {
				t.Fatalf("%q #%d: index %s#%d, scan %s#%d", req.Query, i, got[i].File, got[i].Index, want[i].File, want[i].Index)
			}
		}
	}
}
