package vectorstore_test

// Fixtures in testdata/real_* were written by the official libraries
// (annoy 1.17.3, faiss-cpu 1.15.1) with testdata/gen_real.py; the expected
// neighbours come from those libraries' own search.

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
)

type realCase struct {
	Dim    int       `json:"dim"`
	Query  []float32 `json:"query"`
	Top    []int64   `json:"top"`
	Scores []float64 `json:"scores"`
	Item3  []float32 `json:"item3"`
}

func realExpected(t *testing.T) map[string]realCase {
	b, err := os.ReadFile("testdata/real_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]realCase
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func checkTop(t *testing.T, name string, hits []vectorstore.Hit, c realCase, score func(float64) float64) {
	t.Helper()
	if len(hits) != len(c.Top) {
		t.Fatalf("%s: %d hits, want %d", name, len(hits), len(c.Top))
	}
	for i, h := range hits {
		if h.ID != strconv.FormatInt(c.Top[i], 10) {
			t.Fatalf("%s: hit %d = %s, want %d (all %v)", name, i, h.ID, c.Top[i], c.Top)
		}
		if c.Scores != nil && math.Abs(score(h.Score)-c.Scores[i]) > 1e-4 {
			t.Fatalf("%s: score %d = %v, want %v", name, i, score(h.Score), c.Scores[i])
		}
	}
}

func TestRealAnnoyFiles(t *testing.T) {
	exp := realExpected(t)
	for _, metric := range []string{"angular", "euclidean"} {
		c := exp["annoy_"+metric]
		f, err := vectorstore.LoadAnnoyFile("testdata/real_"+metric+".ann", c.Dim, metric)
		if err != nil {
			t.Fatalf("%s: %v", metric, err)
		}
		if f.Len() != 40 {
			t.Fatalf("%s: %d items, want 40", metric, f.Len())
		}
		hits, _ := f.Query(context.Background(), c.Query, len(c.Top))
		checkTop(t, metric, hits, c, nil)
		got, _ := f.Query(context.Background(), c.Item3, 1)
		if got[0].ID != "3" {
			t.Fatalf("%s: item 3 vector maps to %s", metric, got[0].ID)
		}
	}
}

func TestRealFAISSFiles(t *testing.T) {
	exp := realExpected(t)
	for file, key := range map[string]string{"real_flat_l2": "faiss_l2", "real_flat_ip": "faiss_ip", "real_idmap_l2": "faiss_idmap"} {
		c := exp[key]
		f, err := vectorstore.LoadFAISSFlat("testdata/" + file + ".faiss")
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		hits, _ := f.Query(context.Background(), c.Query, len(c.Top))
		score := func(s float64) float64 { return -s } // L2: we negate the squared distance
		if f.Metric == "IP" {
			score = func(s float64) float64 { return s }
		}
		checkTop(t, file, hits, c, score)
	}
}
