package conformance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/kag"
)

// Graph exercises a kag.GraphStore: upsert, neighbours by direction and type,
// shortest paths, a cycle A->B->C->A, name lookup and delete. IDs carry a
// per-run suffix so the suite can run against a shared database.
func Graph(t *testing.T, g kag.GraphStore) {
	ctx := context.Background()
	run := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	a, b, c, d := "a"+run, "b"+run, "c"+run, "d"+run
	ents := []kag.Entity{
		{ID: a, Type: "Person", Name: "Alice " + run},
		{ID: b, Type: "Person", Name: "Bob " + run},
		{ID: c, Type: "Person", Name: "Carol " + run},
		{ID: d, Type: "Person", Name: "Dave " + run},
	}
	if err := g.UpsertEntities(ctx, ents); err != nil {
		t.Fatal(err)
	}
	if err := g.UpsertRelations(ctx, []kag.Relation{
		{From: a, Type: "KNOWS", To: b, Source: "doc1"},
		{From: b, Type: "KNOWS", To: c, Source: "doc1"},
		{From: c, Type: "WORKS_WITH", To: d, Source: "doc2"},
	}); err != nil {
		t.Fatal(err)
	}
	// Re-upserting the same triple must not duplicate it.
	if err := g.UpsertRelations(ctx, []kag.Relation{{From: a, Type: "KNOWS", To: b, Source: "doc3"}}); err != nil {
		t.Fatal(err)
	}

	if e, ok, err := g.Entity(ctx, b); err != nil || !ok || e.Name != "Bob "+run || e.Type != "Person" {
		t.Fatalf("entity: %+v %v %v", e, ok, err)
	}
	if _, ok, _ := g.Entity(ctx, "missing"+run); ok {
		t.Fatal("missing entity found")
	}

	if rels, err := g.Neighbors(ctx, b, nil, kag.Out); err != nil || len(rels) != 1 || rels[0].To != c {
		t.Fatalf("out: %+v %v", rels, err)
	}
	if rels, err := g.Neighbors(ctx, b, nil, kag.In); err != nil || len(rels) != 1 || rels[0].From != a || rels[0].Source != "doc3" {
		t.Fatalf("in (upsert must replace source): %+v %v", rels, err)
	}
	if rels, err := g.Neighbors(ctx, b, nil, kag.Both); err != nil || len(rels) != 2 {
		t.Fatalf("both: %+v %v", rels, err)
	}
	if rels, err := g.Neighbors(ctx, c, []string{"WORKS_WITH"}, kag.Out); err != nil || len(rels) != 1 || rels[0].To != d {
		t.Fatalf("type filter: %+v %v", rels, err)
	}
	if rels, err := g.Neighbors(ctx, c, []string{"KNOWS"}, kag.Both); err != nil || len(rels) != 1 {
		t.Fatalf("type filter both: %+v %v", rels, err)
	}

	paths, err := g.Path(ctx, a, d, 3)
	if err != nil || len(paths) != 1 || len(paths[0]) != 3 || paths[0][0].From != a || paths[0][2].To != d {
		t.Fatalf("path a->d: %+v %v", paths, err)
	}
	if paths, _ = g.Path(ctx, a, d, 2); len(paths) != 0 {
		t.Fatalf("maxHops must cap the path: %+v", paths)
	}

	// Close the cycle A->B->C->A and check that searches still terminate.
	if err := g.UpsertRelations(ctx, []kag.Relation{{From: c, Type: "KNOWS", To: a}}); err != nil {
		t.Fatal(err)
	}
	if paths, err := g.Path(ctx, b, a, 5); err != nil || len(paths) != 1 || len(paths[0]) != 2 {
		t.Fatalf("cycle path b->a: %+v %v", paths, err)
	}
	if paths, err := g.Path(ctx, a, "missing"+run, 5); err != nil || len(paths) != 0 {
		t.Fatalf("unreachable on cycle: %+v %v", paths, err)
	}

	if ents, err := g.FindEntities(ctx, "bob "+run, nil, 5); err != nil || len(ents) == 0 || ents[0].ID != b {
		t.Fatalf("find by name: %+v %v", ents, err)
	}

	if err := g.Delete(ctx, []string{c}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := g.Entity(ctx, c); ok {
		t.Fatal("deleted entity still present")
	}
	if rels, _ := g.Neighbors(ctx, b, nil, kag.Out); len(rels) != 0 {
		t.Fatalf("relations of deleted entity remain: %+v", rels)
	}
	if paths, _ := g.Path(ctx, a, d, 5); len(paths) != 0 {
		t.Fatalf("path through deleted entity: %+v", paths)
	}
	if err := g.Delete(ctx, []string{a, b, d}); err != nil {
		t.Fatal(err)
	}
}
