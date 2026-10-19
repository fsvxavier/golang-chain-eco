package vectorstore

import (
	"context"
	"encoding/gob"
	"math"
	"math/rand"
	"os"
	"slices"
	"sort"
	"sync"
)

// HNSW is an approximate nearest-neighbour index in the style of Malkov &
// Yashunin's hierarchical navigable small-world graphs: cosine distance,
// pure Go, in memory. EfSearch trades speed for recall. Deleting or replacing
// a record leaves a tombstone that is traversed but never returned; the graph
// is rebuilt once tombstones outnumber live records.
type HNSW struct {
	// M is the maximum number of links per node and layer (layer 0 allows 2M).
	// Default 16. Set before the first Upsert.
	M int
	// EfConstruction is the candidate list size used while inserting (default 200).
	EfConstruction int
	// Seed drives the level generator: the same inserts give the same graph.
	Seed int64

	mu       sync.RWMutex
	efSearch int
	ids      map[string]int32 // live record id -> node index
	nodes    []*hnswNode
	entry    int32
	maxLevel int
	dead     int
	rng      *rand.Rand
}

type hnswNode struct {
	rec     Record
	unit    []float32 // normalised vector: cosine is a dot product
	friends [][]int32 // friends[l] for each layer the node lives in
	dead    bool
}

var _ Backend = (*HNSW)(nil)

// NewHNSW returns an empty index with M=16, EfConstruction=200, EfSearch=64.
func NewHNSW() *HNSW { return &HNSW{M: 16, EfConstruction: 200, efSearch: 64} }

// SetEfSearch sets the candidate list size used by Query.
func (h *HNSW) SetEfSearch(ef int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.efSearch = ef
}

// EfSearch returns the candidate list size used by Query.
func (h *HNSW) EfSearch() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ef()
}

func (h *HNSW) m() int {
	if h.M >= 2 {
		return h.M
	}
	return 16
}

func (h *HNSW) efc() int {
	if h.EfConstruction > 0 {
		return h.EfConstruction
	}
	return 200
}

func (h *HNSW) ef() int {
	if h.efSearch > 0 {
		return h.efSearch
	}
	return 64
}

func (h *HNSW) maxLinks(l int) int {
	if l == 0 {
		return 2 * h.m()
	}
	return h.m()
}

func (h *HNSW) reset() {
	h.ids = map[string]int32{}
	h.nodes, h.entry, h.maxLevel, h.dead = nil, -1, 0, 0
	h.rng = rand.New(rand.NewSource(h.Seed))
}

func (h *HNSW) randomLevel() int {
	return int(-math.Log(1-h.rng.Float64()) / math.Log(float64(h.m())))
}

// Upsert inserts or replaces records; a replaced record's old node becomes a tombstone.
func (h *HNSW) Upsert(_ context.Context, recs []Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ids == nil {
		h.reset()
	}
	for _, r := range recs {
		h.remove(r.ID)
		h.insert(r)
	}
	h.maybeCompact()
	return nil
}

// Delete tombstones the records with the given ids.
func (h *HNSW) Delete(_ context.Context, ids []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range ids {
		h.remove(id)
	}
	h.maybeCompact()
	return nil
}

func (h *HNSW) remove(id string) {
	i, ok := h.ids[id]
	if !ok {
		return
	}
	h.nodes[i].dead = true
	h.nodes[i].rec = Record{}
	delete(h.ids, id)
	h.dead++
}

// ponytail: tombstones are reclaimed by a full rebuild once they outnumber live
// records; an incremental repair would avoid the O(n log n) spike.
func (h *HNSW) maybeCompact() {
	if h.dead < 64 || h.dead < len(h.ids) {
		return
	}
	var recs []Record
	for _, n := range h.nodes {
		if !n.dead {
			recs = append(recs, n.rec)
		}
	}
	h.reset()
	for _, r := range recs {
		h.insert(r)
	}
}

// d is the cosine distance between q and node i (both unit length).
func (h *HNSW) d(q []float32, i int32) float32 { return 1 - dot(q, h.nodes[i].unit) }

func (h *HNSW) insert(r Record) {
	id := int32(len(h.nodes))
	lvl := h.randomLevel()
	n := &hnswNode{rec: r, unit: unit(r.Vector), friends: make([][]int32, lvl+1)}
	h.nodes = append(h.nodes, n)
	h.ids[r.ID] = id
	if h.entry < 0 {
		h.entry, h.maxLevel = id, lvl
		return
	}
	ep := []cand{{h.d(n.unit, h.entry), h.entry}}
	for l := h.maxLevel; l > lvl; l-- {
		ep = h.search(n.unit, ep, 1, l, true)
	}
	for l := min(lvl, h.maxLevel); l >= 0; l-- {
		w := h.search(n.unit, ep, h.efc(), l, true)
		for _, c := range h.selectNeighbors(w, h.maxLinks(l)) {
			n.friends[l] = append(n.friends[l], c.i)
			e := h.nodes[c.i]
			e.friends[l] = append(e.friends[l], id)
			if len(e.friends[l]) > h.maxLinks(l) {
				e.friends[l] = h.shrink(e.friends[l], c.i, l)
			}
		}
		ep = w
	}
	if lvl > h.maxLevel {
		h.entry, h.maxLevel = id, lvl
	}
}

// shrink keeps the maxLinks(l) links of node owner that are nearest to it. The
// paper re-runs the neighbour heuristic here; the plain nearest cut is about
// 30x cheaper to build and costs little recall (see TestHNSWRecallVsFlat).
// ponytail: plain nearest-M shrink; use selectNeighbors here if recall on
// clustered data drops below the target.
func (h *HNSW) shrink(links []int32, owner int32, l int) []int32 {
	e := h.nodes[owner]
	cs := make([]cand, len(links))
	for j, f := range links {
		cs[j] = cand{h.d(e.unit, f), f}
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].d < cs[b].d })
	out := make([]int32, h.maxLinks(l))
	for j := range out {
		out[j] = cs[j].i
	}
	return out
}

// selectNeighbors keeps up to m candidates (sorted by distance to the node
// being linked) that are closer to it than to every neighbour already kept,
// then fills the rest with the nearest pruned ones (the paper's heuristic).
func (h *HNSW) selectNeighbors(cands []cand, m int) []cand {
	out := make([]cand, 0, m)
	for _, c := range cands {
		if len(out) == m {
			break
		}
		keep := true
		for _, r := range out {
			if h.d(h.nodes[c.i].unit, r.i) < c.d {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, c)
		}
	}
	for _, c := range cands {
		if len(out) == m {
			break
		}
		if !slices.ContainsFunc(out, func(x cand) bool { return x.i == c.i }) {
			out = append(out, c)
		}
	}
	return out
}

type cand struct {
	d float32
	i int32
}

// candHeap is a binary min-heap on d. A max-heap is a candHeap of negated
// distances. Typed (no container/heap) so pushes do not box.
type candHeap []cand

func (h *candHeap) push(c cand) {
	*h = append(*h, c)
	s := *h
	for i := len(s) - 1; i > 0; {
		p := (i - 1) / 2
		if s[p].d <= s[i].d {
			break
		}
		s[p], s[i] = s[i], s[p]
		i = p
	}
}

// pop removes and returns the smallest element; the heap must not be empty.
func (h *candHeap) pop() cand {
	s := *h
	top, last := s[0], len(s)-1
	s[0] = s[last]
	s = s[:last]
	for i := 0; ; {
		m, l, r := i, 2*i+1, 2*i+2
		if l < len(s) && s[l].d < s[m].d {
			m = l
		}
		if r < len(s) && s[r].d < s[m].d {
			m = r
		}
		if m == i {
			break
		}
		s[i], s[m] = s[m], s[i]
		i = m
	}
	*h = s
	return top
}

// search returns up to ef nodes of layer l nearest to q, sorted by distance.
// Tombstones are traversed; they are returned only when dead is true.
func (h *HNSW) search(q []float32, eps []cand, ef, l int, dead bool) []cand {
	visited := make([]bool, len(h.nodes))
	var cands, w candHeap // w is a max-heap: keys are negated distances
	for _, c := range eps {
		visited[c.i] = true
		cands.push(c)
		if dead || !h.nodes[c.i].dead {
			w.push(cand{-c.d, c.i})
		}
	}
	for len(w) > ef {
		w.pop()
	}
	for len(cands) > 0 {
		c := cands.pop()
		if len(w) >= ef && c.d > -w[0].d {
			break
		}
		for _, f := range h.nodes[c.i].friends[l] {
			if visited[f] {
				continue
			}
			visited[f] = true
			d := h.d(q, f)
			if len(w) < ef || d < -w[0].d {
				cands.push(cand{d, f})
				if dead || !h.nodes[f].dead {
					w.push(cand{-d, f})
					if len(w) > ef {
						w.pop()
					}
				}
			}
		}
	}
	out := make([]cand, len(w))
	for i := len(out) - 1; i >= 0; i-- {
		c := w.pop()
		out[i] = cand{-c.d, c.i}
	}
	return out
}

// Query returns the k nearest live records to q, best first.
func (h *HNSW) Query(_ context.Context, q []float32, k int) ([]Hit, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if k <= 0 || len(h.ids) == 0 {
		return nil, nil
	}
	uq := unit(q)
	ep := []cand{{h.d(uq, h.entry), h.entry}}
	for l := h.maxLevel; l > 0; l-- {
		ep = h.search(uq, ep, 1, l, true)
	}
	res := h.search(uq, ep, max(h.ef(), k), 0, false)
	if len(res) > k {
		res = res[:k]
	}
	hits := make([]Hit, len(res))
	for j, c := range res {
		hits[j] = Hit{h.nodes[c.i].rec, float64(1 - c.d)}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
	return hits, nil
}

type hnswFile struct {
	M, EfConstruction, EfSearch int
	Seed                        int64
	Recs                        []Record
}

// Save writes the parameters and live records to path (gob). The graph is not
// stored: LoadHNSW rebuilds it, which keeps the file a plain record list like Flat's.
func (h *HNSW) Save(path string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	f := hnswFile{M: h.m(), EfConstruction: h.efc(), EfSearch: h.ef(), Seed: h.Seed}
	for _, n := range h.nodes {
		if !n.dead {
			f.Recs = append(f.Recs, n.rec)
		}
	}
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	return gob.NewEncoder(fh).Encode(f)
}

// LoadHNSW reads an index written by Save and rebuilds its graph.
func LoadHNSW(path string) (*HNSW, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var f hnswFile
	if err := gob.NewDecoder(fh).Decode(&f); err != nil {
		return nil, err
	}
	h := &HNSW{M: f.M, EfConstruction: f.EfConstruction, efSearch: f.EfSearch, Seed: f.Seed}
	h.reset()
	for _, r := range f.Recs {
		h.insert(r)
	}
	return h, nil
}
