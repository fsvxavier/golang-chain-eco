package vectorstore

import (
	"container/heap"
	"context"
	"encoding/gob"
	"math"
	"math/rand"
	"os"
	"sort"
	"sync"
)

// Flat is an exact, in-process vector index — the Go counterpart of FAISS
// IndexFlat (cosine). It is pure Go (no cgo, no FAISS binding), keeps
// everything in memory and can be saved to / loaded from a file.
type Flat struct {
	mu   sync.RWMutex
	recs map[string]Record
}

var _ Backend = (*Flat)(nil)

// NewFlat returns an empty index.
func NewFlat() *Flat { return &Flat{recs: map[string]Record{}} }

func (f *Flat) Upsert(_ context.Context, recs []Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range recs {
		f.recs[r.ID] = r
	}
	return nil
}

func (f *Flat) Delete(_ context.Context, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		delete(f.recs, id)
	}
	return nil
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := 0; i < len(a) && i < len(b); i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func topK(recs []Record, q []float32, k int) []Hit {
	hits := make([]Hit, len(recs))
	for i, r := range recs {
		hits[i] = Hit{r, cosine(q, r.Vector)}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

func (f *Flat) Query(_ context.Context, q []float32, k int) ([]Hit, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	recs := make([]Record, 0, len(f.recs))
	for _, r := range f.recs {
		recs = append(recs, r)
	}
	return topK(recs, q, k), nil
}

// Save writes the index to path (gob).
func (f *Flat) Save(path string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	return gob.NewEncoder(fh).Encode(f.recs)
}

// LoadFlat reads an index written by Save.
func LoadFlat(path string) (*Flat, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	f := NewFlat()
	return f, gob.NewDecoder(fh).Decode(&f.recs)
}

func init() { gob.Register(map[string]any{}); gob.Register([]any{}) }

// Annoy is an approximate index in the style of Spotify's Annoy: a forest of
// random-projection trees searched best-first, then exact re-ranking of the
// candidates. Pure Go, in-memory; the forest is rebuilt lazily after writes.
// Trees trades build time and memory for recall (default 10); SearchK is the
// number of candidates inspected (default k*Trees).
type Annoy struct {
	Trees   int
	SearchK int
	Seed    int64

	mu     sync.Mutex
	recs   map[string]Record
	items  []Record // snapshot the forest was built from
	forest []*annoyNode
	dirty  bool
}

var _ Backend = (*Annoy)(nil)

type annoyNode struct {
	normal      []float32
	offset      float32
	left, right *annoyNode
	leaf        []int // indexes into Annoy.items
}

const annoyLeaf = 16

func (a *Annoy) Upsert(_ context.Context, recs []Record) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.recs == nil {
		a.recs = map[string]Record{}
	}
	for _, r := range recs {
		a.recs[r.ID] = r
	}
	a.dirty = true
	return nil
}

func (a *Annoy) Delete(_ context.Context, ids []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		delete(a.recs, id)
	}
	a.dirty = true
	return nil
}

func unit(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	out := make([]float32, len(v))
	if n == 0 {
		return out
	}
	s := float32(1 / math.Sqrt(n))
	for i, x := range v {
		out[i] = x * s
	}
	return out
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func (a *Annoy) build() {
	ids := make([]string, 0, len(a.recs))
	for id := range a.recs {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic given Seed
	a.items = make([]Record, len(ids))
	unitVecs := make([][]float32, len(ids))
	idx := make([]int, len(ids))
	for i, id := range ids {
		a.items[i], unitVecs[i], idx[i] = a.recs[id], unit(a.recs[id].Vector), i
	}
	trees := a.Trees
	if trees <= 0 {
		trees = 10
	}
	rng := rand.New(rand.NewSource(a.Seed))
	a.forest = a.forest[:0]
	for range trees {
		a.forest = append(a.forest, buildNode(unitVecs, append([]int(nil), idx...), rng))
	}
	a.dirty = false
}

func buildNode(vecs [][]float32, idx []int, rng *rand.Rand) *annoyNode {
	if len(idx) <= annoyLeaf {
		return &annoyNode{leaf: idx}
	}
	for try := 0; try < 3; try++ {
		p, q := vecs[idx[rng.Intn(len(idx))]], vecs[idx[rng.Intn(len(idx))]]
		normal := make([]float32, len(p))
		var mid float32
		for i := range p {
			normal[i] = p[i] - q[i]
			mid += normal[i] * (p[i] + q[i]) / 2
		}
		n := &annoyNode{normal: normal, offset: -mid}
		var l, r []int
		for _, i := range idx {
			if dot(normal, vecs[i])+n.offset > 0 {
				l = append(l, i)
			} else {
				r = append(r, i)
			}
		}
		if len(l) > 0 && len(r) > 0 {
			n.left, n.right = buildNode(vecs, l, rng), buildNode(vecs, r, rng)
			return n
		}
	}
	// degenerate (duplicates): split arbitrarily so recursion terminates
	h := len(idx) / 2
	return &annoyNode{normal: make([]float32, len(vecs[idx[0]])),
		left: buildNode(vecs, idx[:h], rng), right: buildNode(vecs, idx[h:], rng)}
}

type frame struct {
	n      *annoyNode
	margin float32 // higher = more promising
}
type frames []frame

func (f frames) Len() int           { return len(f) }
func (f frames) Less(i, j int) bool { return f[i].margin > f[j].margin }
func (f frames) Swap(i, j int)      { f[i], f[j] = f[j], f[i] }
func (f *frames) Push(x any)        { *f = append(*f, x.(frame)) }
func (f *frames) Pop() any {
	old := *f
	x := old[len(old)-1]
	*f = old[:len(old)-1]
	return x
}

func (a *Annoy) Query(_ context.Context, q []float32, k int) ([]Hit, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dirty || a.forest == nil {
		a.build()
	}
	if len(a.items) == 0 {
		return nil, nil
	}
	searchK := a.SearchK
	if searchK <= 0 {
		searchK = k * len(a.forest)
	}
	uq := unit(q)
	pq := &frames{}
	for _, t := range a.forest {
		heap.Push(pq, frame{t, float32(math.Inf(1))})
	}
	seen := map[int]bool{}
	var cands []Record
	for pq.Len() > 0 && len(cands) < searchK {
		f := heap.Pop(pq).(frame)
		if f.n.leaf != nil {
			for _, i := range f.n.leaf {
				if !seen[i] {
					seen[i] = true
					cands = append(cands, a.items[i])
				}
			}
			continue
		}
		m := dot(f.n.normal, uq) + f.n.offset
		heap.Push(pq, frame{f.n.left, min(f.margin, m)})
		heap.Push(pq, frame{f.n.right, min(f.margin, -m)})
	}
	return topK(cands, q, k), nil
}
