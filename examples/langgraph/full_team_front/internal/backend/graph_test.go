package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
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
	r := runs[0]
	if r.name != "codegraph" || r.args[len(r.args)-1] != "coupon handler" || r.args[len(r.args)-2] != "--" {
		t.Fatalf("command: %+v", r)
	}
	p := false
	for i, a := range r.args {
		if a == "-p" && r.args[i+1] != "" {
			p = true
		}
	}
	if !p {
		t.Fatalf("project path missing: %v", r.args)
	}

	for name, args := range map[string]map[string]any{
		"empty query":        {"query": "  "},
		"option as query":    {"query": "--help"},
		"too long":           {"query": strings.Repeat("a", maxQueryLen+1)},
		"root outside":       {"query": "x", "root": ws},
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

func TestBackendGraphNeedsScopeAndOwnIndex(t *testing.T) {
	ws, root := workspace(t)
	if err := os.MkdirAll(filepath.Join(ws, ".codegraph"), 0o755); err != nil { // an index ABOVE the root
		t.Fatal(err)
	}
	cfg := Config{WorkspaceRoot: ws, Run: func(context.Context, string, []string) (string, error) {
		t.Fatal("must not run")
		return "", nil
	}}
	if _, err := call(t, Tools(fixed(team.Scope{Roots: []string{root}}), cfg), "backend_graph", map[string]any{"query": "x"}); err == nil {
		t.Fatal("an index above the root must not be used: it may cover directories the demand did not name")
	}
	if _, err := call(t, Tools(fixed(team.Scope{}), cfg), "backend_graph", map[string]any{"query": "x"}); err == nil {
		t.Fatal("no scope, no access")
	}
}

func TestBackendGraphPrefersCodeGraphThenGraphify(t *testing.T) {
	_, root := workspace(t)
	if got := engineFor(root); len(got) != 0 {
		t.Fatalf("no index: %v", got)
	}
	_ = os.MkdirAll(filepath.Join(root, "graphify-out"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "graphify-out", "graph.json"), []byte("{}"), 0o644)
	if got := engineFor(root); len(got) != 1 || got[0] != "graphify" {
		t.Fatalf("graphify only: %v", got)
	}
	_ = os.MkdirAll(filepath.Join(root, ".codegraph-wsl"), 0o755)
	if got := engineFor(root); len(got) != 2 || got[0] != "codegraph" {
		t.Fatalf("codegraph must come first: %v", got)
	}
	name, args := graphCmd("graphify", root, "q")
	if name != "graphify" || args[0] != "query" || args[1] != "q" {
		t.Fatalf("%s %v", name, args)
	}
}

// With the real CodeGraph CLI and a project that has an index, the answer must
// come back and stay under the cap. Skipped when either is missing.
func TestRealCodeGraphIfAvailable(t *testing.T) {
	dir := os.Getenv("GRAPH_TEST_ROOT")
	if _, err := exec.LookPath("codegraph"); err != nil || dir == "" {
		t.Skip("set GRAPH_TEST_ROOT to a project with a CodeGraph index and install codegraph")
	}
	out, err := runGraph(context.Background(), "codegraph", []string{"explore", "-p", dir, "--max-files", "4", "--", "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || len(out) > maxGraphBytes+len(truncated) {
		t.Fatalf("answer of %d bytes", len(out))
	}
}
