package kag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
)

// Memory is an in-process GraphStore. ponytail: single RWMutex and linear scans
// for FindEntities; swap for an index or a real graph database when the graph
// outgrows RAM or query latency matters.
type Memory struct {
	mu   sync.RWMutex
	ents map[string]Entity
	out  map[string]map[string]Relation // from -> relKey -> relation
	in   map[string]map[string]Relation // to   -> relKey -> relation
}

// NewMemory returns an empty graph.
func NewMemory() *Memory {
	return &Memory{ents: map[string]Entity{}, out: map[string]map[string]Relation{}, in: map[string]map[string]Relation{}}
}

func relKey(r Relation) string { return r.From + "\x00" + r.Type + "\x00" + r.To }

func cloneEntity(e Entity) Entity {
	e.Props = maps.Clone(e.Props)
	e.Vector = slices.Clone(e.Vector)
	return e
}

func cloneRelation(r Relation) Relation {
	r.Props = maps.Clone(r.Props)
	return r
}

func sortRelations(rels []Relation) {
	slices.SortFunc(rels, func(a, b Relation) int {
		return strings.Compare(relKey(a), relKey(b))
	})
}

func (m *Memory) UpsertEntities(ctx context.Context, ents []Entity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, e := range ents {
		if e.ID == "" {
			return errors.New("kag: entity without ID")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range ents {
		m.ents[e.ID] = cloneEntity(e)
	}
	return nil
}

func (m *Memory) UpsertRelations(ctx context.Context, rels []Relation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, r := range rels {
		if r.From == "" || r.To == "" || r.Type == "" {
			return fmt.Errorf("kag: relation needs From, Type and To: %+v", r)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range rels {
		r = cloneRelation(r)
		k := relKey(r)
		if m.out[r.From] == nil {
			m.out[r.From] = map[string]Relation{}
		}
		if m.in[r.To] == nil {
			m.in[r.To] = map[string]Relation{}
		}
		m.out[r.From][k] = r
		m.in[r.To][k] = r
	}
	return nil
}

func (m *Memory) Neighbors(ctx context.Context, id string, relTypes []string, dir Direction) ([]Relation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string]bool{}
	var res []Relation
	add := func(edges map[string]Relation) {
		for k, r := range edges {
			if seen[k] || (len(relTypes) > 0 && !slices.Contains(relTypes, r.Type)) {
				continue
			}
			seen[k] = true
			res = append(res, cloneRelation(r))
		}
	}
	if dir == Out || dir == Both {
		add(m.out[id])
	}
	if dir == In || dir == Both {
		add(m.in[id])
	}
	sortRelations(res)
	return res, nil
}

func (m *Memory) Entity(ctx context.Context, id string) (Entity, bool, error) {
	if err := ctx.Err(); err != nil {
		return Entity{}, false, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.ents[id]
	return cloneEntity(e), ok, nil
}

func (m *Memory) FindEntities(ctx context.Context, text string, vec []float32, k int) ([]Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := tokens(text)
	m.mu.RLock()
	defer m.mu.RUnlock()
	type scored struct {
		e Entity
		s float64
	}
	var hits []scored
	for _, e := range m.ents {
		var s float64
		if len(vec) > 0 && len(e.Vector) > 0 {
			s = embeddings.Cosine(vec, e.Vector)
		} else {
			s = nameScore(q, e)
		}
		if s > 0 {
			hits = append(hits, scored{cloneEntity(e), s})
		}
	}
	slices.SortFunc(hits, func(a, b scored) int {
		if a.s != b.s {
			if a.s > b.s {
				return -1
			}
			return 1
		}
		return strings.Compare(a.e.ID, b.e.ID)
	})
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	res := make([]Entity, len(hits))
	for i, h := range hits {
		res[i] = h.e
	}
	return res, nil
}

// Path runs a layered BFS over outgoing edges, so it terminates on cycles, and
// reconstructs every path that reaches `to` at the shortest depth.
func (m *Memory) Path(ctx context.Context, from, to string, maxHops int) ([][]Relation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxHops <= 0 || from == to {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	type hop struct {
		prev string
		rel  Relation
	}
	dist := map[string]int{from: 0}
	parents := map[string][]hop{}
	frontier := []string{from}
	for d := 1; d <= maxHops && len(frontier) > 0; d++ {
		var next []string
		for _, u := range frontier {
			edges := make([]Relation, 0, len(m.out[u]))
			for _, r := range m.out[u] {
				edges = append(edges, r)
			}
			sortRelations(edges)
			for _, r := range edges {
				if dd, ok := dist[r.To]; ok && dd < d {
					continue
				}
				if _, ok := dist[r.To]; !ok {
					dist[r.To] = d
					next = append(next, r.To)
				}
				parents[r.To] = append(parents[r.To], hop{u, r})
			}
		}
		if _, ok := dist[to]; ok {
			break
		}
		frontier = next
	}
	if _, ok := dist[to]; !ok {
		return nil, nil
	}

	var build func(node string) [][]Relation
	build = func(node string) [][]Relation {
		if node == from {
			return [][]Relation{{}}
		}
		var paths [][]Relation
		for _, h := range parents[node] {
			for _, p := range build(h.prev) {
				paths = append(paths, append(slices.Clone(p), cloneRelation(h.rel)))
			}
		}
		return paths
	}
	return build(to), nil
}

func (m *Memory) Delete(ctx context.Context, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.ents, id)
		for k, r := range m.out[id] {
			delete(m.in[r.To], k)
		}
		delete(m.out, id)
		for k, r := range m.in[id] {
			delete(m.out[r.From], k)
		}
		delete(m.in, id)
	}
	return nil
}

type snapshot struct {
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
}

// Save writes the whole graph as JSON. Props values come back as JSON types
// (numbers become float64) after LoadMemory.
func (m *Memory) Save(path string) error {
	m.mu.RLock()
	snap := snapshot{}
	for _, e := range m.ents {
		snap.Entities = append(snap.Entities, e)
	}
	for _, edges := range m.out {
		for _, r := range edges {
			snap.Relations = append(snap.Relations, r)
		}
	}
	m.mu.RUnlock()
	slices.SortFunc(snap.Entities, func(a, b Entity) int { return strings.Compare(a.ID, b.ID) })
	sortRelations(snap.Relations)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadMemory reads a graph written by Save.
func LoadMemory(path string) (*Memory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("kag: load %s: %w", path, err)
	}
	m := NewMemory()
	ctx := context.Background()
	if err := m.UpsertEntities(ctx, snap.Entities); err != nil {
		return nil, err
	}
	if err := m.UpsertRelations(ctx, snap.Relations); err != nil {
		return nil, err
	}
	return m, nil
}

// tokens lowercases, strips basic Latin accents and splits on non-alphanumerics.
func tokens(s string) []string {
	return strings.FieldsFunc(accentFolder.Replace(strings.ToLower(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// nameScore is the share of query tokens found in the entity name or ID.
func nameScore(q []string, e Entity) float64 {
	if len(q) == 0 {
		return 0
	}
	have := map[string]bool{}
	for _, t := range append(tokens(e.Name), tokens(e.ID)...) {
		have[t] = true
	}
	hits := 0
	for _, t := range q {
		if have[t] {
			hits++
		}
	}
	return float64(hits) / float64(len(q))
}
