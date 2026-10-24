// Package codegraph lets an agent search a project through its code graph
// instead of grepping and reading files: CodeGraph (a .codegraph or
// .codegraph-wsl index) and graphify (graphify-out/graph.json) answer a
// question with the relevant symbols' source and call paths in one call, which
// costs far fewer tokens than several grep and read calls.
//
// The engines are external programs (codegraph, graphify). They run without a
// shell, with a minimal environment, a timeout and a cap on the output. The
// index must sit in the searched directory itself: an index higher up could
// cover directories the caller did not mean to expose.
package codegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Engine is a code graph program.
type Engine string

// The supported engines. CodeGraph is preferred: it answers with the symbols'
// source and their call paths.
const (
	CodeGraph Engine = "codegraph"
	Graphify  Engine = "graphify"
)

const (
	// MaxBytes caps an answer; it is meant to replace several reads, so it is small.
	MaxBytes = 24 << 10
	// Timeout bounds one engine run.
	Timeout = 30 * time.Second
	// MaxQuery is the longest accepted question, in bytes.
	MaxQuery = 500
	// Truncated marks a cut answer.
	Truncated = "\n[truncated]"
)

// Runner runs an engine command and returns its output. Run is the default.
type Runner func(ctx context.Context, name string, args []string) (string, error)

// Detect returns the engines whose index sits directly in dir, CodeGraph first.
// graphify counts when graphify-out has a graph with nodes (see GraphifyGraph).
func Detect(dir string) []Engine {
	var out []Engine
	for _, n := range []string{".codegraph", ".codegraph-wsl"} {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && fi.IsDir() {
			out = append(out, CodeGraph)
			break
		}
	}
	if GraphifyGraph(dir) != "" {
		out = append(out, Graphify)
	}
	return out
}

// GraphifyGraph is the graph graphify should be asked about: a graph.json with
// at least one node anywhere under dir/graphify-out (the cache and hidden
// folders aside), the most recently written one. graphify keeps dated snapshots
// next to the current graph (graphify-out/2026-10-13/graph.json), and the
// current one can be empty after an update that indexed nothing, so the top-level
// file alone is not enough. It returns "" when there is none.
func GraphifyGraph(dir string) string {
	root := filepath.Join(dir, "graphify-out")
	var best string
	var bestTime time.Time
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable entry is skipped, not fatal
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			if p != root && (d.Name() == "cache" || strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator)) >= maxGraphDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "graph.json" || !d.Type().IsRegular() || !graphHasNodes(p) {
			return nil
		}
		if fi, err := d.Info(); err == nil && (best == "" || fi.ModTime().After(bestTime)) {
			best, bestTime = p, fi.ModTime()
		}
		return nil
	})
	return best
}

// maxGraphDepth bounds how deep under graphify-out snapshots are looked for.
const maxGraphDepth = 3

// graphHasNodes reports whether path is a graphify graph with at least one
// node. graphify writes a valid, empty graph when it indexed no files (an ignore
// file that excludes everything does that), and asking it only returns "No
// matching nodes found" after a wasted call.
// shortcut: a file over 4 KB is taken as populated without parsing it (an empty
// graph is about 240 bytes); parse it too if graphify ever pads empty graphs.
func graphHasNodes(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	if fi.Size() > 4<<10 {
		return true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var g struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	return json.Unmarshal(b, &g) == nil && len(g.Nodes) > 0
}

// Command is the command line of e for a question about dir. The query is one
// argv element (no shell); Ask refuses a query that starts with "-".
func Command(e Engine, dir, query string) (string, []string) {
	if e == Graphify {
		return "graphify", []string{"query", query, "--graph", GraphifyGraph(dir), "--budget", "1500"}
	}
	return "codegraph", []string{"explore", "-p", dir, "--max-files", "4", "--", query}
}

// ValidQuery reports whether q can be passed to an engine.
func ValidQuery(q string) bool {
	return q != "" && len(q) <= MaxQuery && !strings.ContainsRune(q, 0) && !strings.HasPrefix(q, "-")
}

// Ask answers a question about dir with engine ("" picks the first one dir
// has). run nil means Run. It fails, without running anything, when the query
// is not valid or dir has no index of the engine.
func Ask(ctx context.Context, run Runner, dir string, engine Engine, query string) (string, error) {
	query = strings.TrimSpace(query)
	if !ValidQuery(query) {
		return "", fmt.Errorf("query must be 1 to %d characters and not start with '-'", MaxQuery)
	}
	have := Detect(dir)
	switch {
	case len(have) == 0:
		return "", fmt.Errorf("%s has no CodeGraph or graphify index of its own", dir)
	case engine == "":
		engine = have[0]
	default:
		ok := false
		for _, h := range have {
			ok = ok || h == engine
		}
		if !ok {
			return "", fmt.Errorf("engine %q is not available for %s (has: %v)", engine, dir, have)
		}
	}
	if run == nil {
		run = Run
	}
	name, args := Command(engine, dir, query)
	// graphify only finds nodes when it runs in the project directory; Run reads
	// the directory from the context so the Runner signature stays small.
	return run(context.WithValue(ctx, dirKey{}, dir), name, args)
}

type dirKey struct{}

// Run runs the command without a shell, with a minimal environment, a timeout
// and a cap on the output.
func Run(ctx context.Context, name string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if d, ok := ctx.Value(dirKey{}).(string); ok {
		cmd.Dir = d
	}
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "NO_COLOR=1", "LANG=C.UTF-8"}
	var out, errb bytes.Buffer
	so := &limitWriter{w: &out, max: MaxBytes}
	cmd.Stdout = so
	cmd.Stderr = &limitWriter{w: &errb, max: 2 << 10}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s timed out after %s", name, Timeout)
		}
		return "", fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(errb.String()))
	}
	s := out.String()
	if so.cut {
		s += Truncated
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

// Args are the arguments of the tool built by Tool.
type Args struct {
	Query  string `json:"query" jsonschema:"what to find, in words or symbol names"`
	Root   string `json:"root,omitempty" jsonschema:"a directory that has its own CodeGraph or graphify index; default the first one offered"`
	Engine string `json:"engine,omitempty" jsonschema:"codegraph or graphify; default the first one the root has"`
}

// Description is the standard tool description.
const Description = "Ask a project directory that has a CodeGraph or graphify index (a .codegraph or graphify-out folder in it) what it contains: you get the relevant symbols' source and their call paths in one answer, usually far cheaper than several grep and read calls. Try it first; if the directory has no index it says so, and then use the search and read tools."

// Tool builds a tool called name. resolve maps the call's root argument ("" for
// the default) to the directory to search and decides what the caller may reach;
// it must refuse anything outside what the agent is allowed to read. run nil
// means Run. done, when set, is told after every answer (for call counting).
func Tool(name string, resolve func(ctx context.Context, root string) (string, error), run Runner, done func(context.Context)) tools.Tool {
	return tools.New(name, Description, func(ctx context.Context, a Args) (any, error) {
		dir, err := resolve(ctx, a.Root)
		if err != nil {
			return nil, tools.Errorf("%s: %v", name, err)
		}
		out, err := Ask(ctx, run, dir, Engine(a.Engine), a.Query)
		if err != nil {
			return nil, tools.Errorf("%s: %v: use the search and read tools instead", name, err)
		}
		if done != nil {
			done(ctx)
		}
		return out, nil
	})
}

// ProjectTool is Tool for one fixed directory (root). It is what an example
// with a plain working directory adds to its agent; it is nil when root has no
// index, so the agent carries no tool it cannot use.
func ProjectTool(root string) tools.Tool {
	abs, err := filepath.Abs(root)
	if err != nil || len(Detect(abs)) == 0 {
		return nil
	}
	return Tool("code_graph", func(_ context.Context, r string) (string, error) {
		if r != "" && filepath.Clean(r) != "." && filepath.Clean(r) != abs {
			return "", fmt.Errorf("only %s can be searched", abs)
		}
		return abs, nil
	}, nil, nil)
}
