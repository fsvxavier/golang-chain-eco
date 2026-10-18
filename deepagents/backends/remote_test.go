package backends_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

// localRunner pretends bash on this machine is a remote sandbox.
func localRunner(ctx context.Context, command string, _ time.Duration) (backends.ExecResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	res := backends.ExecResult{Output: buf.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode, err = ee.ExitCode(), nil
	}
	return res, err
}

func TestRemote(t *testing.T) {
	ctx := context.Background()
	r := backends.Remote{Run: localRunner, Root: t.TempDir()}

	if _, err := r.Write(ctx, "/src/a.txt", "hello 'world'\nsecond line $HOME\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write(ctx, "/src/a.txt", "again"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite must fail: %v", err)
	}
	got, err := r.Read(ctx, "/src/a.txt")
	if err != nil || got != "hello 'world'\nsecond line $HOME\n" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := r.Read(ctx, "/nope"); !errors.Is(err, backends.ErrNotFound) {
		t.Fatal(err)
	}
	if _, n, err := r.Edit(ctx, "/src/a.txt", "second", "third", false); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if got, _ = r.Read(ctx, "/src/a.txt"); !strings.Contains(got, "third line") {
		t.Fatal(got)
	}
	_, _ = r.Write(ctx, "/src/deep/b.go", "package b\nvar third = 1\n")

	ls, err := r.Ls(ctx, "/src")
	if err != nil || len(ls) != 2 || ls[0].Path != "/src/a.txt" || ls[1].Path != "/src/deep/" || !ls[1].IsDir {
		t.Fatalf("%+v %v", ls, err)
	}
	gl, err := r.Glob(ctx, "**/*.go", "/src")
	if err != nil || len(gl) != 1 || gl[0].Path != "/src/deep/b.go" {
		t.Fatalf("%+v %v", gl, err)
	}
	gr, err := r.Grep(ctx, "third", "/src", "")
	if err != nil || len(gr) != 2 || gr[0].Path != "/src/a.txt" || gr[0].Line != 2 || gr[1].Path != "/src/deep/b.go" {
		t.Fatalf("%+v %v", gr, err)
	}
	if gr, _ = r.Grep(ctx, "third", "/src", "*.go"); len(gr) != 1 {
		t.Fatalf("glob filter: %+v", gr)
	}
	if gr, _ = r.Grep(ctx, "absent", "/src", ""); len(gr) != 0 {
		t.Fatal(gr)
	}
	res, err := r.Execute(ctx, "pwd && echo ok", 0)
	if err != nil || res.ExitCode != 0 || !strings.HasSuffix(strings.TrimSpace(res.Output), "ok") {
		t.Fatalf("%+v %v", res, err)
	}
	if _, ok := backends.AsExecutor(r); !ok {
		t.Fatal("Remote must be an Executor")
	}
}
