package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocPath(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "project", "front-end"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "project", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "x.md"), filepath.Join(ws, "project", "evil.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DocsConfig{WorkspaceRoot: ws}.withDefaults()
	for _, ok := range []string{"project/front-end/plan.md", "project/new/dir/next_steps.md", "project/PLAN.MD"} {
		if _, err := docPath(cfg, ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "src/main.go", "src/x.md", "project/a.txt", "../x.md", "project/../src/x.md",
		"project/link/x.md", "/etc/passwd.md", filepath.Join(outside, "x.md"), "project/evil.md", "other/x.md"} {
		if _, err := docPath(cfg, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDocToolWritesOnlyForPO(t *testing.T) {
	ws := t.TempDir()
	rt := DocTools(DocsConfig{WorkspaceRoot: ws})
	if len(rt("junior")) != 0 || len(rt("qa")) != 0 || len(rt("leader")) != 0 {
		t.Fatal("only the po may write docs")
	}
	if len(rt("po")) != 1 {
		t.Fatal("po must get project_doc_write")
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
