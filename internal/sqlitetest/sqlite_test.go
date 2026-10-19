// Integration test for sqlsaver against a real SQLite driver. It lives in its
// own module so the main module stays dependency-free.
package sqlitetest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/internal/conformance"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store/sqlstore"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

func open(t *testing.T) *sqlsaver.Saver {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := sqlsaver.New(context.Background(), db, sqlsaver.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSaverCRUD(t *testing.T) {
	s, ctx := open(t), context.Background()
	id1, _ := s.Put(ctx, checkpoint.Checkpoint{ThreadID: "t", Step: 1, Channels: map[string]any{"n": 1}})
	id2, _ := s.Put(ctx, checkpoint.Checkpoint{ThreadID: "t", Step: 2, ParentID: id1})
	cp, ok, err := s.Get(ctx, checkpoint.Config{ThreadID: "t"})
	if err != nil || !ok || cp.ID != id2 {
		t.Fatal(cp, ok, err)
	}
	cp, _, _ = s.Get(ctx, checkpoint.Config{ThreadID: "t", CheckpointID: id1})
	if cp.Channels["n"] != 1 {
		t.Fatal(cp)
	}
	if l, _ := s.List(ctx, "t", 1); len(l) != 1 || l[0].ID != id2 {
		t.Fatal(l)
	}
	_ = s.DeleteThread(ctx, "t")
	if _, ok, _ := s.Get(ctx, checkpoint.Config{ThreadID: "t"}); ok {
		t.Fatal("deleted")
	}
}

type wireArgs struct {
	Amount int `json:"amount"`
}

// HITL pause/resume across a "restart": a second agent instance and a second
// connection share only the database file.
func TestHITLAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cp.db")
	mk := func(script ...messages.Message) (*graph.Compiled, func()) {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		s, err := sqlsaver.New(context.Background(), db, sqlsaver.SQLite)
		if err != nil {
			t.Fatal(err)
		}
		wire := tools.New("wire", "send", func(_ context.Context, a wireArgs) (any, error) { return "sent", nil })
		ag, err := agents.Create(agents.Options{
			Model: models.NewFake(script...), Tools: []tools.Tool{wire},
			Middleware: []agents.Middleware{middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"wire": {}}}},
			Compile:    []graph.CompileOption{graph.WithCheckpointer(s)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return ag, func() { db.Close() }
	}
	ctx := runnables.WithConfig(context.Background(), graph.Thread("t"))

	a1, close1 := mk(messages.AI("", messages.ToolCall{ID: "w", Name: "wire", Args: map[string]any{"amount": 5}}))
	out, err := a1.Invoke(ctx, graph.State{"messages": "pay"})
	if err != nil || len(graph.Interrupts(out)) != 1 {
		t.Fatalf("%v %v", out, err)
	}
	close1()

	a2, close2 := mk(messages.AI("done"))
	defer close2()
	out, err = a2.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: []middleware.HITLDecision{{Type: middleware.Approve}}}})
	if err != nil {
		t.Fatal(err)
	}
	ms := out["messages"].([]messages.Message)
	if ms[len(ms)-1].Content != "done" || ms[2].Content != "sent" {
		t.Fatalf("%+v", ms)
	}
}

func TestSQLStore(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	s, err := sqlstore.New(ctx, db, sqlstore.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Put(ctx, []string{"users", "1"}, "prefs", map[string]any{"lang": "go", "n": 3})
	_ = s.Put(ctx, []string{"users", "2"}, "prefs", map[string]any{"lang": "py"})
	_ = s.Put(ctx, []string{"users_other"}, "x", map[string]any{"lang": "go"})
	_ = s.Put(ctx, []string{"users", "1"}, "prefs", map[string]any{"lang": "rust"}) // upsert
	it, _ := s.Get(ctx, []string{"users", "1"}, "prefs")
	if it == nil || it.Value["lang"] != "rust" {
		t.Fatalf("%+v", it)
	}
	got, _ := s.Search(ctx, []string{"users"}, map[string]any{"lang": "py"}, 0)
	if len(got) != 1 || got[0].Namespace[1] != "2" {
		t.Fatalf("%+v", got)
	}
	all, _ := s.Search(ctx, []string{"users"}, nil, 0)
	if len(all) != 2 { // "users_other" must not match prefix "users"
		t.Fatalf("prefix leak: %+v", all)
	}
	ns, _ := s.ListNamespaces(ctx, []string{"users"})
	if len(ns) != 2 {
		t.Fatal(ns)
	}
	_ = s.Delete(ctx, []string{"users", "1"}, "prefs")
	if it, _ := s.Get(ctx, []string{"users", "1"}, "prefs"); it != nil {
		t.Fatal("deleted")
	}
}

// langmem's IndexedStore keeps vectors inside the item value; they must
// survive the JSON round trip of a real SQL backend.
func TestLangmemIndexedStoreOverSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	base, err := sqlstore.New(ctx, db, sqlstore.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	st := langmem.NewIndexedStore(base, embeddings.Fake{})
	ns := []string{"memories", "u1"}
	_ = st.Put(ctx, ns, "a", map[string]any{"content": "the user loves sushi and japanese food"})
	_ = st.Put(ctx, ns, "b", map[string]any{"content": "the user works on kubernetes clusters"})
	hits, err := langmem.Query(ctx, st, ns, "japanese food", nil, 5)
	if err != nil || len(hits) != 2 || hits[0].Key != "a" || hits[0].Score <= hits[1].Score {
		t.Fatalf("%+v %v", hits, err)
	}
	if _, leaked := hits[0].Value["_embedding"]; leaked {
		t.Fatal("embedding leaked")
	}
}

func TestSaverNamespaces(t *testing.T) {
	conformance.Namespaces(t, open(t))
}

// A checkpoints_go table written before the checkpoint_ns column existed must
// still be readable, and the migration must be repeatable.
func TestSaverMigratesOldTable(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TABLE checkpoints_go (
		thread_id TEXT NOT NULL, checkpoint_id TEXT NOT NULL, seq BIGINT NOT NULL, data TEXT NOT NULL,
		PRIMARY KEY (thread_id, checkpoint_id))`); err != nil {
		t.Fatal(err)
	}
	b, err := checkpoint.Marshal(checkpoint.Checkpoint{ID: "old", ThreadID: "t", Step: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO checkpoints_go (thread_id, checkpoint_id, seq, data) VALUES ('t', 'old', 1, ?)`, string(b)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s, err := sqlsaver.New(ctx, db, sqlsaver.SQLite)
		if err != nil {
			t.Fatal(err)
		}
		cp, ok, err := s.Get(ctx, checkpoint.Config{ThreadID: "t"})
		if err != nil || !ok || cp.ID != "old" || cp.Step != 3 {
			t.Fatalf("old row: %+v %v %v", cp, ok, err)
		}
	}
}

// Scalars that plain JSON would widen (int64, time.Time, []byte) must come back
// from a real database with their exact Go types.
func TestSaverKeepsScalarTypes(t *testing.T) {
	s, ctx := open(t), context.Background()
	when := time.Date(2026, 10, 19, 12, 30, 0, 500, time.UTC)
	id, err := s.Put(ctx, checkpoint.Checkpoint{ThreadID: "t", Step: 1, Channels: map[string]any{
		"big": int64(1<<62 + 3), "when": when, "raw": []byte("ok"), "n": 7,
	}})
	if err != nil {
		t.Fatal(err)
	}
	cp, ok, err := s.Get(ctx, checkpoint.Config{ThreadID: "t", CheckpointID: id})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if cp.Channels["big"] != int64(1<<62+3) || cp.Channels["n"] != 7 || string(cp.Channels["raw"].([]byte)) != "ok" {
		t.Fatalf("%#v", cp.Channels)
	}
	if got, ok := cp.Channels["when"].(time.Time); !ok || !got.Equal(when) {
		t.Fatalf("when: %#v", cp.Channels["when"])
	}
}
