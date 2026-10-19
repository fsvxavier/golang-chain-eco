package kag_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
	"github.com/fsvxavier/golang-chain-eco/kag"
)

func TestGraphConformance(t *testing.T) {
	conformance.Graph(t, kag.NewMemory())
}

func TestPathTerminatesOnCycle(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	if err := m.UpsertEntities(ctx, []kag.Entity{{ID: "a"}, {ID: "b"}, {ID: "c"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.UpsertRelations(ctx, []kag.Relation{
		{From: "a", Type: "R", To: "b"}, {From: "b", Type: "R", To: "a"}, {From: "b", Type: "R", To: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	paths, err := m.Path(ctx, "a", "c", 50)
	if err != nil || len(paths) != 0 {
		t.Fatalf("unreachable target: %v %v", paths, err)
	}
}

func TestPathReturnsAllShortest(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	// diamond a->b->d and a->c->d, plus a longer a->d detour that must not appear.
	if err := m.UpsertRelations(ctx, []kag.Relation{
		{From: "a", Type: "R", To: "b"}, {From: "b", Type: "R", To: "d"},
		{From: "a", Type: "R", To: "c"}, {From: "c", Type: "R", To: "d"},
		{From: "c", Type: "R", To: "e"}, {From: "e", Type: "R", To: "d"},
	}); err != nil {
		t.Fatal(err)
	}
	paths, err := m.Path(ctx, "a", "d", 3)
	if err != nil || len(paths) != 2 {
		t.Fatalf("want 2 shortest paths, got %d: %v", len(paths), err)
	}
	for _, p := range paths {
		if len(p) != 2 || p[0].From != "a" || p[1].To != "d" {
			t.Fatalf("bad path: %+v", p)
		}
	}
	if paths, _ = m.Path(ctx, "a", "d", 1); len(paths) != 0 {
		t.Fatalf("maxHops ignored: %v", paths)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	if err := m.UpsertEntities(ctx, []kag.Entity{
		{ID: "dr_silva", Type: "Person", Name: "Dr. Silva", Props: map[string]any{"crm": "123"}, Vector: []float32{1, 0}},
		{ID: "x", Type: "Drug", Name: "X"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.UpsertRelations(ctx, []kag.Relation{{From: "dr_silva", Type: "PRESCREVEU", To: "x", Source: "doc1"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := kag.LoadMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	e, ok, err := loaded.Entity(ctx, "dr_silva")
	if err != nil || !ok || e.Name != "Dr. Silva" || e.Type != "Person" || len(e.Vector) != 2 || e.Props["crm"] != "123" {
		t.Fatalf("entity: %+v %v %v", e, ok, err)
	}
	rels, err := loaded.Neighbors(ctx, "dr_silva", nil, kag.Out)
	if err != nil || len(rels) != 1 || rels[0].Type != "PRESCREVEU" || rels[0].Source != "doc1" {
		t.Fatalf("relations: %+v %v", rels, err)
	}
	if _, err := kag.LoadMemory(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file loaded")
	}
}

func TestFindEntitiesByName(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	if err := m.UpsertEntities(ctx, []kag.Entity{
		{ID: "maria", Name: "Maria"},
		{ID: "joao_silva", Name: "João Silva"},
		{ID: "joao_souza", Name: "João Souza"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := m.FindEntities(ctx, "JOAO silva", nil, 5)
	if err != nil || len(got) == 0 || got[0].ID != "joao_silva" {
		t.Fatalf("accent-insensitive match: %+v %v", got, err)
	}
	for _, e := range got {
		if e.ID == "maria" {
			t.Fatalf("unrelated entity returned: %+v", got)
		}
	}
	if got, _ = m.FindEntities(ctx, "joao", nil, 1); len(got) != 1 || got[0].ID != "joao_silva" {
		t.Fatalf("ties must break by ID, k=1: %+v", got)
	}
}

func TestFindEntitiesByVector(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	if err := m.UpsertEntities(ctx, []kag.Entity{
		{ID: "x", Name: "X", Vector: []float32{1, 0, 0}},
		{ID: "y", Name: "Y", Vector: []float32{0, 1, 0}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := m.FindEntities(ctx, "", []float32{0.9, 0.1, 0}, 2)
	if err != nil || len(got) != 2 || got[0].ID != "x" {
		t.Fatalf("cosine ranking: %+v %v", got, err)
	}
}

func TestNormalizeID(t *testing.T) {
	if got := kag.NormalizeID("  Dr.  Silva Jr "); got != "dr._silva_jr" {
		t.Fatalf("got %q", got)
	}
}

func TestConcurrentUpsertRead(t *testing.T) {
	ctx := context.Background()
	m := kag.NewMemory()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := range 50 {
				id := fmt.Sprintf("e%d_%d", i, j)
				_ = m.UpsertEntities(ctx, []kag.Entity{{ID: id, Name: id}})
				_ = m.UpsertRelations(ctx, []kag.Relation{{From: id, Type: "R", To: "hub"}})
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				_, _ = m.Neighbors(ctx, "hub", nil, kag.In)
				_, _ = m.FindEntities(ctx, "e1", nil, 3)
				_, _ = m.Path(ctx, "e1_0", "hub", 2)
			}
		}()
	}
	wg.Wait()
	if rels, _ := m.Neighbors(ctx, "hub", nil, kag.In); len(rels) != 400 {
		t.Fatalf("want 400 incoming, got %d", len(rels))
	}
}
