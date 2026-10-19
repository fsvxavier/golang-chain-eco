package graphstore

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/memgraph"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
	"github.com/fsvxavier/golang-chain-eco/kag"
)

// unreachable makes any request fail with a dial error, so a test can tell a
// validation error (raised before any request) from a network one.
func unreachable() *memgraph.Client { return &memgraph.Client{Addr: "127.0.0.1:1"} }

func TestMemgraphRelationTypeValidated(t *testing.T) {
	m := &Memgraph{Client: unreachable()}
	bad := []string{"X`) DETACH DELETE n //", "knows", "KNOWS-X", ""}
	for _, typ := range bad {
		err := m.UpsertRelations(context.Background(), []kag.Relation{
			{From: "a", Type: "KNOWS", To: "b"},
			{From: "a", Type: typ, To: "b"},
		})
		if err == nil {
			t.Fatalf("type %q accepted", typ)
		}
		if strings.Contains(err.Error(), "memgraph:") || strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "connect") {
			t.Fatalf("type %q reached the network: %v", typ, err)
		}
	}
}

func TestMemgraphLabelValidated(t *testing.T) {
	m := &Memgraph{Client: unreachable(), Label: "Entity) DETACH DELETE n //"}
	ctx := context.Background()
	calls := map[string]error{
		"UpsertEntities":  m.UpsertEntities(ctx, []kag.Entity{{ID: "x", Name: "x"}}),
		"UpsertRelations": m.UpsertRelations(ctx, []kag.Relation{{From: "a", Type: "KNOWS", To: "b"}}),
		"Neighbors":       errOf(m.Neighbors(ctx, "a", nil, kag.Both)),
		"Entity":          errOf2(m.Entity(ctx, "a")),
		"FindEntities":    errOf(m.FindEntities(ctx, "a", nil, 1)),
		"Path":            errOf(m.Path(ctx, "a", "b", 2)),
		"Delete":          m.Delete(ctx, []string{"a"}),
	}
	for name, err := range calls {
		if err == nil || !strings.Contains(err.Error(), "invalid identifier") {
			t.Fatalf("%s: want label validation error, got %v", name, err)
		}
	}
}

func TestMemgraphDefaultLabelIsEntity(t *testing.T) {
	m := &Memgraph{}
	if l, err := m.label(); err != nil || l != "Entity" {
		t.Fatalf("label %q %v", l, err)
	}
}

func TestMemgraphPathQueryUsesAllShortest(t *testing.T) {
	q := memgraphPathQuery("Entity", 3)
	if !strings.Contains(q, "[*ALLSHORTEST ..3 (r, n | 1) w]") {
		t.Fatalf("query: %s", q)
	}
	if strings.Contains(q, "allShortestPaths") || strings.Contains(q, "BFS") {
		t.Fatalf("query uses a syntax that returns one path or does not parse: %s", q)
	}
}

func TestMemgraphRowsToEntities(t *testing.T) {
	rows := [][]any{
		{"a", "Alice", "Person", `{"k":1}`, []any{float64(1), int64(2)}},
		{"b", nil, nil, nil, nil},
	}
	ents, err := rowsToEntities(rows)
	if err != nil {
		t.Fatal(err)
	}
	want := []kag.Entity{
		{ID: "a", Name: "Alice", Type: "Person", Props: map[string]any{"k": float64(1)}, Vector: []float32{1, 2}},
		{ID: "b"},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("got %+v want %+v", ents, want)
	}
	if _, err := rowsToEntities([][]any{{int64(7), "x", "t", nil, nil}}); err == nil {
		t.Fatal("numeric id accepted")
	}
	if _, err := rowsToEntities([][]any{{"a", "x", "t", map[string]any{}, nil}}); err == nil {
		t.Fatal("map props accepted; props are stored as a JSON string")
	}
	if _, err := rowsToEntities([][]any{{"a", "x", "t", nil, []any{"no"}}}); err == nil {
		t.Fatal("non-numeric vector accepted")
	}
}

func TestMemgraphRowsToRelations(t *testing.T) {
	rows := [][]any{
		{"a", "KNOWS", "b", `{"w":1}`, "doc1"},
		{"c", "KNOWS", "a", nil, nil},
	}
	rels, err := rowsToRelations(rows)
	if err != nil {
		t.Fatal(err)
	}
	want := []kag.Relation{
		{From: "a", Type: "KNOWS", To: "b", Props: map[string]any{"w": float64(1)}, Source: "doc1"},
		{From: "c", Type: "KNOWS", To: "a"},
	}
	if !reflect.DeepEqual(rels, want) {
		t.Fatalf("got %+v want %+v", rels, want)
	}
	if _, err := rowsToRelations([][]any{{"a", "KNOWS", "b"}}); err == nil {
		t.Fatal("short row accepted")
	}
}

func TestMemgraphPathFromValue(t *testing.T) {
	v := []any{
		map[string]any{"from": "a", "type": "KNOWS", "to": "b", "props": `{"w":2}`, "source": "doc1"},
		map[string]any{"from": "b", "type": "WORKS_WITH", "to": "c", "props": nil, "source": nil},
	}
	path, err := pathFromValue(v)
	if err != nil {
		t.Fatal(err)
	}
	want := []kag.Relation{
		{From: "a", Type: "KNOWS", To: "b", Props: map[string]any{"w": float64(2)}, Source: "doc1"},
		{From: "b", Type: "WORKS_WITH", To: "c"},
	}
	if !reflect.DeepEqual(path, want) {
		t.Fatalf("got %+v want %+v", path, want)
	}
	if _, err := pathFromValue("not a list"); err == nil {
		t.Fatal("scalar path accepted")
	}
	if _, err := pathFromValue([]any{int64(1)}); err == nil {
		t.Fatal("scalar hop accepted")
	}
}

// TestMemgraphReal runs the shared graph suite against a live Memgraph.
// MEMGRAPH_ADDR=127.0.0.1:7687 (no auth; set MEMGRAPH_USER/MEMGRAPH_PASS if needed).
func TestMemgraphReal(t *testing.T) {
	addr := os.Getenv("MEMGRAPH_ADDR")
	if addr == "" {
		t.Skip("MEMGRAPH_ADDR not set")
	}
	c := &memgraph.Client{Addr: addr, Username: os.Getenv("MEMGRAPH_USER"), Password: os.Getenv("MEMGRAPH_PASS")}
	label := fmt.Sprintf("G%d", time.Now().UnixNano())
	m := &Memgraph{Client: c, Label: label}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = m.run(ctx, fmt.Sprintf("MATCH (n:%s) DETACH DELETE n", label), nil)
		_, _ = m.run(ctx, fmt.Sprintf("DROP CONSTRAINT ON (n:%s) ASSERT n.id IS UNIQUE", label), nil)
		_ = c.Close()
	})
	conformance.Graph(t, m)
}

// errOf and errOf2 drop the value so a call's error can sit in a table.
func errOf[T any](_ T, err error) error { return err }

func errOf2[A, B any](_ A, _ B, err error) error { return err }
