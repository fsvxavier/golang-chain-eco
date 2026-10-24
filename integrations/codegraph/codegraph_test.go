package codegraph_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		if err := os.WriteFile(filepath.Join(d, "graphify-out", "graph.json"), []byte("{}"), 0o644); err != nil {
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
