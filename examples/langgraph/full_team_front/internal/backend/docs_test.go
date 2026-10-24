package backend

import (
	"context"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
	"os"
	"path/filepath"
	"testing"
)

func TestDocPath(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	for _, d := range []string{"project/front-end", "docs", "src"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(ws, "project", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "x.md"), filepath.Join(ws, "project", "evil.md")); err != nil {
		t.Fatal(err)
	}
	roots := []string{"project", "docs"} // named by the demand
	for _, ok := range []string{"front-end/plan.md", "new/dir/next_steps.md", "PLAN.MD", filepath.Join(ws, "docs", "adr.md")} {
		if _, err := docPath(ws, roots, ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a.txt", "../x.md", "front-end/../../x.md", "link/x.md", "/etc/passwd.md",
		filepath.Join(outside, "x.md"), "evil.md", filepath.Join(ws, "src", "x.md")} {
		if _, err := docPath(ws, roots, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	// The location comes from the demand: another root allows another place, none allows nothing.
	if _, err := docPath(ws, []string{"src"}, "x.md"); err != nil {
		t.Errorf("a root named by the demand must work: %v", err)
	}
	if _, err := docPath(ws, nil, "x.md"); err == nil {
		t.Error("no doc roots must refuse")
	}
	if _, err := docPath(ws, []string{outside}, "x.md"); err == nil {
		t.Error("a root outside the workspace must refuse")
	}
}

func TestDocToolWritesOnlyForPO(t *testing.T) {
	ws := t.TempDir()
	rt := DocTools(func() *team.HubClient { return nil }, DocsConfig{WorkspaceRoot: ws})
	if len(rt("junior")) != 0 || len(rt("qa")) != 0 || len(rt("leader")) != 0 {
		t.Fatal("only the po may write docs")
	}
	if len(rt("po")) != 1 {
		t.Fatal("po must get project_doc_write")
	}
	// Without a hub the tool has no scope: it must refuse, not guess a location.
	if _, err := rt("po")[0].Call(context.Background(), map[string]any{"path": "plan.md", "content": "x"}); err == nil {
		t.Fatal("no scope must refuse the write")
	}
	p := filepath.Join(ws, "project", "plan.md")
	if err := writeDoc(p, "a\n", false); err != nil {
		t.Fatal(err)
	}
	if err := writeDoc(p, "b\n", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a\nb\n" {
		t.Fatalf("got %q", b)
	}
	if err := writeDoc(p, "c\n", false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "c\n" {
		t.Fatalf("replace got %q", b)
	}
}
