package codegraph_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/codegraph"
)

func indexed(t *testing.T, dirs ...string) string {
	t.Helper()
	d := t.TempDir()
	for _, n := range dirs {
		if err := os.MkdirAll(filepath.Join(d, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if len(dirs) > 0 && dirs[len(dirs)-1] == "graphify-out" {
		if err := os.WriteFile(filepath.Join(d, "graphify-out", "graph.json"), []byte(`{"nodes":[{"id":"a"}],"links":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestDetectPrefersCodeGraph(t *testing.T) {
	if got := codegraph.Detect(t.TempDir()); len(got) != 0 {
		t.Fatalf("no index: %v", got)
	}
	d := indexed(t, ".codegraph-wsl", "graphify-out")
	if got := codegraph.Detect(d); len(got) != 2 || got[0] != codegraph.CodeGraph || got[1] != codegraph.Graphify {
		t.Fatalf("got %v", got)
	}
	if got := codegraph.Detect(indexed(t, "graphify-out")); len(got) != 1 || got[0] != codegraph.Graphify {
		t.Fatalf("graphify only: %v", got)
	}
}

func TestAskRunsTheRightCommand(t *testing.T) {
	d := indexed(t, ".codegraph", "graphify-out")
	var name string
	var args []string
	run := func(_ context.Context, n string, a []string) (string, error) { name, args = n, a; return "ok", nil }

	if out, err := codegraph.Ask(context.Background(), run, d, "", " find auth "); err != nil || out != "ok" {
		t.Fatal(out, err)
	}
	if name != "codegraph" || args[len(args)-2] != "--" || args[len(args)-1] != "find auth" {
		t.Fatalf("%s %v", name, args)
	}
	if _, err := codegraph.Ask(context.Background(), run, d, codegraph.Graphify, "find auth"); err != nil {
		t.Fatal(err)
	}
	if name != "graphify" || args[0] != "query" || args[1] != "find auth" {
		t.Fatalf("%s %v", name, args)
	}
}

func TestAskRefusesWithoutRunning(t *testing.T) {
	d := indexed(t, ".codegraph")
	run := func(context.Context, string, []string) (string, error) {
		t.Fatal("must not run")
		return "", nil
	}
	for name, c := range map[string]struct {
		dir, q string
		e      codegraph.Engine
	}{
		"empty":        {d, "  ", ""},
		"option":       {d, "--help", ""},
		"too long":     {d, strings.Repeat("a", codegraph.MaxQuery+1), ""},
		"nul":          {d, "a\x00b", ""},
		"no index":     {t.TempDir(), "x", ""},
		"other engine": {d, "x", codegraph.Graphify},
		"unknown":      {d, "x", "rm"},
		"index above":  {filepath.Join(d, "sub"), "x", ""},
	} {
		if _, err := codegraph.Ask(context.Background(), run, c.dir, c.e, c.q); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestToolResolvesAndCounts(t *testing.T) {
	d := indexed(t, ".codegraph")
	var done int
	tl := codegraph.Tool("t", func(_ context.Context, r string) (string, error) {
		if r == "bad" {
			return "", os.ErrPermission
		}
		return d, nil
	}, func(context.Context, string, []string) (string, error) { return "answer", nil }, func(context.Context) { done++ })
	if out, err := tl.Call(context.Background(), map[string]any{"query": "x"}); err != nil || out != "answer" || done != 1 {
		t.Fatal(out, err, done)
	}
	if _, err := tl.Call(context.Background(), map[string]any{"query": "x", "root": "bad"}); err == nil || done != 1 {
		t.Fatalf("a refused root must not run or count: %v done=%d", err, done)
	}
}

func TestProjectToolOnlyWhenIndexed(t *testing.T) {
	if codegraph.ProjectTool(t.TempDir()) != nil {
		t.Fatal("no index, no tool")
	}
	d := indexed(t, ".codegraph")
	tl := codegraph.ProjectTool(d)
	if tl == nil || tl.Name() != "code_graph" {
		t.Fatal("indexed project must get code_graph")
	}
	if _, err := tl.Call(context.Background(), map[string]any{"query": "x", "root": "/etc"}); err == nil {
		t.Fatal("a root other than the project must be refused")
	}
}

// graphify writes a valid but empty graph when it indexed nothing; that is no index.
func TestDetectIgnoresAnEmptyGraphifyGraph(t *testing.T) {
	d := indexed(t, "graphify-out")
	if err := os.WriteFile(filepath.Join(d, "graphify-out", "graph.json"), []byte(`{"directed":false,"nodes":[],"links":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegraph.Detect(d); len(got) != 0 {
		t.Fatalf("an empty graph is not an index: %v", got)
	}
	if err := os.WriteFile(filepath.Join(d, "graphify-out", "graph.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegraph.Detect(d); len(got) != 0 {
		t.Fatalf("a broken graph is not an index: %v", got)
	}
}

// graphify keeps dated snapshots under graphify-out; when the top-level graph is
// empty (an update that indexed nothing), the newest snapshot with nodes is used.
func TestGraphifyGraphScansSnapshots(t *testing.T) {
	d := t.TempDir()
	write := func(rel, body string, age int) string {
		p := filepath.Join(d, "graphify-out", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-time.Duration(age) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}
	full := `{"nodes":[{"id":"a"}],"links":[]}`
	write("graph.json", `{"nodes":[],"links":[]}`, 0)
	if codegraph.GraphifyGraph(d) != "" {
		t.Fatal("only an empty graph: no index")
	}
	old := write("2026-10-01/graph.json", full, 48)
	newer := write("2026-10-13/graph.json", full, 24)
	write("cache/x/graph.json", full, 1) // the cache is not a graph to ask
	write(".hidden/graph.json", full, 1)
	if got := codegraph.GraphifyGraph(d); got != newer {
		t.Fatalf("want the newest snapshot with nodes %s (not %s), got %s", newer, old, got)
	}
	top := write("graph.json", full, 0)
	if got := codegraph.GraphifyGraph(d); got != top {
		t.Fatalf("a populated top-level graph is the newest: %s", got)
	}
	if got := codegraph.Detect(d); len(got) != 1 || got[0] != codegraph.Graphify {
		t.Fatalf("%v", got)
	}
	if _, args := codegraph.Command(codegraph.Graphify, d, "q"); args[3] != top {
		t.Fatalf("the command must use the chosen graph: %v", args)
	}
}
