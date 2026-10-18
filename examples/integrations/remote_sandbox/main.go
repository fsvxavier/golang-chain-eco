// remote_sandbox: any "run a command somewhere" function becomes a full
// deepagents backend (files + shell) through backends.Remote. Here the
// "somewhere" is a throwaway directory on this machine, driven by bash, so
// the example runs anywhere — it EXECUTES COMMANDS ON THE HOST, so do not
// point an untrusted agent at it. In production swap runLocal for
// kubernetes.Sandbox.Run (a pod) or any other isolated runner.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

func runLocal(ctx context.Context, command string, _ time.Duration) (backends.ExecResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res := backends.ExecResult{Output: out.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode, err = ee.ExitCode(), nil
	}
	return res, err
}

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "sandbox-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	b := backends.Remote{Run: runLocal, Root: dir}
	if _, err := b.Write(ctx, "/app/main.go", "package main\n\n// TODO: handle errors\nfunc main() {}\n"); err != nil {
		log.Fatal(err)
	}
	src, _ := b.Read(ctx, "/app/main.go")
	fmt.Printf("read back %d bytes\n", len(src))

	if _, n, err := b.Edit(ctx, "/app/main.go", "handle errors", "return errors", false); err == nil {
		fmt.Printf("edited %d occurrence(s)\n", n)
	}
	matches, _ := b.Grep(ctx, "TODO", "/app", "*.go")
	for _, m := range matches {
		fmt.Printf("grep %s:%d %s\n", m.Path, m.Line, m.Text)
	}
	ls, _ := b.Ls(ctx, "/app")
	for _, f := range ls {
		fmt.Printf("ls   %s (%d bytes)\n", f.Path, f.Size)
	}
	res, _ := b.Execute(ctx, "wc -l app/main.go", 10*time.Second)
	fmt.Printf("exec exit=%d output=%s", res.ExitCode, res.Output)
	// Wire it to an agent with deepagents.Create(model, deepagents.WithBackend(b)).
}
