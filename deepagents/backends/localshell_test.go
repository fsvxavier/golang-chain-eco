package backends_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

func TestLocalShellAllowedCommands(t *testing.T) {
	be := backends.LocalShell{
		Filesystem:      backends.Filesystem{Root: t.TempDir()},
		AllowedCommands: []string{"echo", "pwd", "ls"},
		Quiet:           true,
	}
	ctx := context.Background()
	for _, c := range []string{"echo hi", "FOO=1 BAR=2 echo hi", "echo a && ls", "echo hi | pwd", "echo hi\npwd"} {
		if _, err := be.Execute(ctx, c, 0); err != nil {
			t.Errorf("must allow %q: %v", c, err)
		}
	}
	for _, c := range []string{
		"rm -rf x", "echo hi; rm x", "echo ok && curl example.com", "echo $(rm x)", "echo `id`",
		"ls\nwhoami", "bash -c 'rm x'", "echo <(id)", "/bin/rm x", "(rm x)",
	} {
		if _, err := be.Execute(ctx, c, 0); !errors.Is(err, backends.ErrCommandNotAllowed) {
			t.Errorf("must deny %q, got %v", c, err)
		}
	}
	// without an allowlist every command runs
	open := backends.LocalShell{Filesystem: be.Filesystem, Quiet: true}
	if res, err := open.Execute(ctx, "echo open", 0); err != nil || !strings.Contains(res.Output, "open") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestLocalShellFixedWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	be := backends.LocalShell{Filesystem: backends.Filesystem{Root: root}, Quiet: true}
	// the command starts in Root, whatever the caller's cwd is
	res, err := be.Execute(context.Background(), "pwd -P", 0)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	if strings.TrimSpace(res.Output) != want {
		t.Fatalf("cwd %q, want %q", res.Output, want)
	}
}

func TestLocalShellHostWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	root := t.TempDir()
	quiet := backends.LocalShell{Filesystem: backends.Filesystem{Root: root}, Quiet: true}
	_, _ = quiet.Execute(context.Background(), "true", 0)
	if buf.Len() != 0 {
		t.Fatalf("Quiet must not warn: %s", buf.String())
	}
	loud := backends.LocalShell{Filesystem: backends.Filesystem{Root: root}}
	_, _ = loud.Execute(context.Background(), "true", 0)
	_, _ = loud.Execute(context.Background(), "true", 0)
	if n := strings.Count(buf.String(), "runs commands directly on the host"); n != 1 {
		t.Fatalf("want exactly one warning, got %d: %s", n, buf.String())
	}
}
