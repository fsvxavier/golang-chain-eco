package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

const (
	maxGraphBytes = 24 << 10 // a graph answer is meant to replace several reads, so it is capped well below MaxBytes
	graphTimeout  = 30 * time.Second
	maxQueryLen   = 500
)

type graphArgs struct {
	Query  string `json:"query" jsonschema:"what to find, in words or symbol names"`
	Root   string `json:"root,omitempty" jsonschema:"a named root, or a directory inside one, that has its own CodeGraph or graphify index; default the first root"`
	Engine string `json:"engine,omitempty" jsonschema:"codegraph or graphify; default the first one the root has"`
}

// engineFor returns the engines whose index sits directly in dir, CodeGraph
// first: it answers with the relevant symbols' source and their call paths.
// The index must be in dir itself, not above it: an index higher up could cover
// directories the demand did not name.
func engineFor(dir string) []string {
	var out []string
	for _, n := range []string{".codegraph", ".codegraph-wsl"} {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && fi.IsDir() {
			out = append(out, "codegraph")
			break
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "graphify-out", "graph.json")); err == nil && fi.Mode().IsRegular() {
		out = append(out, "graphify")
	}
	return out
}

// graphCmd is the command line of engine for a question about dir. The query is
// one argv element (no shell) and cannot start with "-", so it is never an option.
func graphCmd(engine, dir, query string) (string, []string) {
	if engine == "graphify" {
		return "graphify", []string{"query", query, "--graph", filepath.Join(dir, "graphify-out", "graph.json"), "--budget", "1500"}
	}
	return "codegraph", []string{"explore", "-p", dir, "--max-files", "4", "--", query}
}

// runGraph runs the engine without a shell, with a minimal environment, a
// timeout and a cap on the output.
func runGraph(ctx context.Context, name string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, graphTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "NO_COLOR=1", "LANG=C.UTF-8"}
	var out bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, max: maxGraphBytes}
	var errb bytes.Buffer
	cmd.Stderr = &limitWriter{w: &errb, max: 2 << 10}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s timed out after %s", name, graphTimeout)
		}
		return "", fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(errb.String()))
	}
	s := out.String()
	if lw, ok := cmd.Stdout.(*limitWriter); ok && lw.cut {
		s += truncated
	}
	return s, nil
}

// limitWriter keeps the first max bytes and drops the rest without failing the writer.
type limitWriter struct {
	w   *bytes.Buffer
	max int
	cut bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if room := l.max - l.w.Len(); room < len(p) {
		l.cut = true
		if room > 0 {
			l.w.Write(p[:room])
		}
		return len(p), nil
	}
	return l.w.Write(p)
}

// graphTool is backend_graph: a question answered from the root's code graph.
func graphTool(cfg Config, scope func(context.Context, string) (string, team.Scope, error), count func(context.Context, string)) tools.Tool {
	run := cfg.Run
	if run == nil {
		run = runGraph
	}
	return tools.New("backend_graph", "Ask a backend root that has a CodeGraph or graphify index (a .codegraph or graphify-out folder in the root) what it contains: you get the relevant symbols' source and their call paths in one answer, usually far cheaper than several backend_grep and backend_read calls. Try it first; if the root has no index it says so, and then use backend_grep and backend_read.",
		func(ctx context.Context, a graphArgs) (any, error) {
			id, s, err := scope(ctx, "backend_graph")
			if err != nil {
				return nil, err
			}
			q := strings.TrimSpace(a.Query)
			if q == "" || len(q) > maxQueryLen || strings.ContainsRune(q, 0) || strings.HasPrefix(q, "-") {
				return nil, tools.Errorf("backend_graph: query must be 1 to %d characters and not start with '-'", maxQueryLen)
			}
			dir, err := resolvePath(cfg.WorkspaceRoot, s.Roots, a.Root)
			if err != nil {
				return nil, tools.Errorf("backend_graph: %v", err)
			}
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				return nil, tools.Errorf("backend_graph: %q is not a directory", a.Root)
			}
			avail := engineFor(dir)
			engine := a.Engine
			switch {
			case len(avail) == 0:
				return nil, tools.Errorf("backend_graph: %s has no CodeGraph or graphify index of its own: use backend_grep and backend_read", dir)
			case engine == "":
				engine = avail[0]
			case engine != "codegraph" && engine != "graphify" || !contains(avail, engine):
				return nil, tools.Errorf("backend_graph: engine %q is not available for this root (has: %s)", engine, strings.Join(avail, ", "))
			}
			name, args := graphCmd(engine, dir, q)
			out, err := run(ctx, name, args)
			if err != nil {
				return nil, tools.Errorf("backend_graph: %v: use backend_grep and backend_read", err)
			}
			count(ctx, id)
			return out, nil
		})
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
