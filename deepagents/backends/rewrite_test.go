package backends_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

func TestLocalShellRewrite(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Rewrite changes the command that runs.
	be := backends.LocalShell{
		Filesystem: backends.Filesystem{Root: root},
		Quiet:      true,
		Rewrite: func(_ context.Context, c string) string {
			return strings.ReplaceAll(c, "echo hi", "echo REWRITTEN")
		},
	}
	res, err := be.Execute(ctx, "echo hi", 0)
	if err != nil || !strings.Contains(res.Output, "REWRITTEN") {
		t.Fatalf("rewritten command not run: %+v %v", res, err)
	}

	// AllowedCommands checks the original; the rewritten command runs.
	if err := os.WriteFile(filepath.Join(root, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	allowed := backends.LocalShell{
		Filesystem:      backends.Filesystem{Root: root},
		Quiet:           true,
		AllowedCommands: []string{"echo"},
		Rewrite: func(_ context.Context, c string) string {
			calls++
			return "rm x"
		},
	}
	if _, err := allowed.Execute(ctx, "echo hi", 0); err != nil {
		t.Fatalf("allowed original refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rewritten rm x did not run: %v", err)
	}

	// A disallowed original is refused before Rewrite is called.
	calls = 0
	_, err = allowed.Execute(ctx, "rm -rf y", 0)
	if !errors.Is(err, backends.ErrCommandNotAllowed) {
		t.Fatalf("disallowed original ran: %v", err)
	}
	if calls != 0 {
		t.Fatalf("Rewrite called %d times for a refused command", calls)
	}

	// Nil Rewrite keeps behaviour identical.
	plain := backends.LocalShell{Filesystem: backends.Filesystem{Root: root}, Quiet: true}
	if res, err := plain.Execute(ctx, "echo plain", 0); err != nil || !strings.Contains(res.Output, "plain") {
		t.Fatalf("nil Rewrite changed behaviour: %+v %v", res, err)
	}
}
