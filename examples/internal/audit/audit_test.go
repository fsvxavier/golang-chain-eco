// Package audit checks every example against the project's token rules, so a new
// example cannot skip them.
//
// An example is a directory with a main.go; its tree belongs to it unless a
// subdirectory has a main.go of its own. The rules:
//
//   - Compaction (caveman, ponytail, TOON): an example that builds agents with
//     agents.Create must use agents/middleware Compaction, NewCompression or
//     Compression. deepagents.Create compacts by itself.
//   - Code graph (CodeGraph, graphify): an example that builds agents with
//     agents.Create and gives them a local directory (backends.Filesystem or
//     backends.LocalShell) must use integrations/codegraph. deepagents.Create adds
//     code_graph by itself when the directory has an index.
//
// An example that cannot follow a rule says why with a comment in any of its
// files: "audit:exempt compaction: <reason>" or "audit:exempt codegraph: <reason>".
package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const mod = "github.com/fsvxavier/golang-chain-eco/"

type facts struct {
	agentsCreate bool
	compaction   bool
	localDir     bool
	codegraph    bool
	exempt       map[string]bool
}

// scan reads the example whose root is dir, leaving out the trees of nested examples.
func scan(t *testing.T, dir string) facts {
	t.Helper()
	f := facts{exempt: map[string]bool{}}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir {
				if _, err := os.Stat(filepath.Join(p, "main.go")); err == nil {
					return filepath.SkipDir // a nested example checks itself
				}
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		full, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		_ = file
		names := map[string]string{} // local name -> import path
		for _, im := range full.Imports {
			path := strings.Trim(im.Path.Value, `"`)
			name := filepath.Base(path)
			if im.Name != nil {
				name = im.Name.Name
			}
			names[name] = path
		}
		for _, cg := range full.Comments {
			for _, c := range cg.List {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(strings.TrimPrefix(c.Text, "//")), "audit:exempt "); ok {
					rule, reason, _ := strings.Cut(rest, ":")
					if strings.TrimSpace(reason) != "" {
						f.exempt[strings.TrimSpace(rule)] = true
					}
				}
			}
		}
		ast.Inspect(full, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch path := names[x.Name]; {
			case path == mod+"agents" && sel.Sel.Name == "Create":
				f.agentsCreate = true
			case path == mod+"agents/middleware" && (sel.Sel.Name == "Compaction" || sel.Sel.Name == "NewCompression" || sel.Sel.Name == "Compression"):
				f.compaction = true
			case path == mod+"deepagents/backends" && (sel.Sel.Name == "Filesystem" || sel.Sel.Name == "LocalShell"):
				f.localDir = true
			case path == mod+"integrations/codegraph":
				f.codegraph = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// exampleRoots lists the directories under examples/ that hold a main.go.
func exampleRoots(t *testing.T) []string {
	t.Helper()
	var roots []string
	err := filepath.WalkDir("../..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && filepath.Base(p) == "internal" && filepath.Base(filepath.Dir(p)) == "examples" {
			return filepath.SkipDir // examples/internal holds helpers, not examples
		}
		if !d.IsDir() && d.Name() == "main.go" {
			roots = append(roots, filepath.Dir(p))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(roots)
	if len(roots) < 20 {
		t.Fatalf("found only %d examples: the scan is broken", len(roots))
	}
	return roots
}

func TestEveryExampleCompactsAndCanUseTheCodeGraph(t *testing.T) {
	for _, dir := range exampleRoots(t) {
		f := scan(t, dir)
		name := strings.TrimPrefix(dir, "../../")
		if f.agentsCreate && !f.compaction && !f.exempt["compaction"] {
			t.Errorf("%s builds agents with agents.Create but uses no compaction: add middleware.Compaction() (caveman, ponytail, TOON) to the agent, or say why not with `// audit:exempt compaction: <reason>`", name)
		}
		if f.agentsCreate && f.localDir && !f.codegraph && !f.exempt["codegraph"] {
			t.Errorf("%s gives agents a local directory but no code graph: add the tool from integrations/codegraph (ProjectTool), or say why not with `// audit:exempt codegraph: <reason>`", name)
		}
	}
}

// The checker itself: it must flag what it claims to flag, or the test above
// proves nothing.
func TestScanFindsTheRules(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", `package main
import (
	"github.com/fsvxavier/golang-chain-eco/agents"
	mw "github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)
// audit:exempt codegraph: the directory is a scratch one
var _ = agents.Create
var _ = mw.Compaction
var _ = backends.LocalShell{}
`)
	write("nested/main.go", "package main\nimport \"github.com/fsvxavier/golang-chain-eco/agents\"\nvar _ = agents.Create\n")
	f := scan(t, dir)
	if !f.agentsCreate || !f.compaction || !f.localDir || f.codegraph || !f.exempt["codegraph"] {
		t.Fatalf("%+v", f)
	}
	n := scan(t, filepath.Join(dir, "nested"))
	if !n.agentsCreate || n.compaction {
		t.Fatalf("nested example: %+v", n)
	}
}
