package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
	"github.com/fsvxavier/golang-chain-eco/integrations/codegraph"
)

type ran struct {
	name string
	args []string
}

func TestBackendGraph(t *testing.T) {
	ws, root := workspace(t)
	if err := os.MkdirAll(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	var runs []ran
	cfg := Config{WorkspaceRoot: ws, Run: func(_ context.Context, name string, args []string) (string, error) {
		runs = append(runs, ran{name, args})
		return "symbols", nil
	}}
	ts := Tools(fixed(team.Scope{Roots: []string{root}}), cfg)

	out, err := call(t, ts, "backend_graph", map[string]any{"query": "coupon handler"})
	if err != nil || out != "symbols" {
		t.Fatalf("%q %v", out, err)
	}
	if r := runs[0]; r.name != "codegraph" || r.args[len(r.args)-1] != "coupon handler" || r.args[len(r.args)-2] != "--" {
		t.Fatalf("command: %+v", r)
	}

	for name, args := range map[string]map[string]any{
		"empty query":        {"query": "  "},
		"option as query":    {"query": "--help"},
		"too long":           {"query": strings.Repeat("a", codegraph.MaxQuery+1)},
		"root outside":       {"query": "x", "root": t.TempDir()},
		"graphify not there": {"query": "x", "engine": "graphify"},
		"unknown engine":     {"query": "x", "engine": "rm"},
		"no index below":     {"query": "x", "root": "sub"},
	} {
		n := len(runs)
		if _, err := call(t, ts, "backend_graph", args); err == nil {
			t.Errorf("%s accepted", name)
		}
		if len(runs) != n {
			t.Errorf("%s ran a command", name)
		}
	}
}

// The workspace itself can be asked too, for any demand: it is the project the
// operator pointed the team at. An index above a root never counts.
func TestBackendGraphWorkspaceAndOwnIndex(t *testing.T) {
	ws, root := workspace(t)
	if err := os.MkdirAll(filepath.Join(ws, ".codegraph-wsl"), 0o755); err != nil {
		t.Fatal(err)
	}
	var dirs []string
	cfg := Config{WorkspaceRoot: ws, Run: func(_ context.Context, _ string, args []string) (string, error) {
		dirs = append(dirs, args[2])
		return "ok", nil
	}}
	// The demand names a backend root without an index; the workspace has one.
	ts := Tools(fixed(team.Scope{Roots: []string{root}}), cfg)
	if _, err := call(t, ts, "backend_graph", map[string]any{"query": "x", "root": ws}); err != nil {
		t.Fatalf("the workspace must be searchable: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("dirs %v", dirs)
	}
	if _, err := call(t, ts, "backend_graph", map[string]any{"query": "x"}); err == nil {
		t.Fatal("the default root (the demand's first) has no index of its own: the workspace index above it must not be used")
	}
	// A demand that named no backend still lets the workspace be searched.
	if _, err := call(t, Tools(fixed(team.Scope{}), cfg), "backend_graph", map[string]any{"query": "x", "root": ws}); err != nil {
		t.Fatal(err)
	}
}

func TestBackendGraphForEveryRole(t *testing.T) {
	rt := RoleTools(func() *team.HubClient { return nil }, Config{WorkspaceRoot: t.TempDir()})
	for _, role := range []string{"po", "leader", "junior", "mid", "senior", "specialist", "qa"} {
		var has bool
		for _, tl := range rt(role) {
			has = has || tl.Name() == "backend_graph"
		}
		if !has {
			t.Errorf("%s lacks backend_graph", role)
		}
	}
	if len(rt("junior")) != 1 {
		t.Error("only the PO gets the read tools besides backend_graph")
	}
}

// With the real CodeGraph CLI and a project that has an index, the answer must
// come back and stay under the cap. Skipped unless GRAPH_TEST_ROOT is set.
func TestRealCodeGraphIfAvailable(t *testing.T) {
	dir := os.Getenv("GRAPH_TEST_ROOT")
	if dir == "" {
		t.Skip("set GRAPH_TEST_ROOT to a project with a CodeGraph index")
	}
	out, err := codegraph.Ask(context.Background(), nil, dir, "", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || len(out) > codegraph.MaxBytes+len(codegraph.Truncated) {
		t.Fatalf("answer of %d bytes", len(out))
	}
}
