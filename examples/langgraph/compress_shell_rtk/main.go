// compress_shell_rtk: routes a shell command through rtk when a decider says so.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

func main() {
	dir, err := os.MkdirTemp("", "compress_shell_rtk")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// The scripted decider says yes to every question: route the command.
	yes := compress.FuncDecider(func(_ context.Context, _ compress.State, qs []compress.Question) ([]compress.Answer, error) {
		out := make([]compress.Answer, len(qs))
		for i, q := range qs {
			out[i] = compress.Answer{ID: q.ID, Kind: q.Kind, True: true, P: 0.9}
		}
		return out, nil
	})
	rtk := compress.NewRTK(yes)

	if _, err := exec.LookPath("rtk"); err != nil {
		fmt.Println("rtk not on PATH, passthrough")
	}

	shell := backends.LocalShell{
		Filesystem:      backends.Filesystem{Root: dir},
		AllowedCommands: []string{"echo", "go"},
		Rewrite: func(ctx context.Context, command string) string {
			executed := rtk.Rewrite(ctx, command)
			fmt.Printf("requested: %s\nexecuted:  %s\n", command, executed)
			return executed
		},
		Quiet: true,
	}
	run := rtk.Executor(shell) // records output sizes for the next decision

	ctx := context.Background()
	res, err := run.Execute(ctx, "go version", 30*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("exit %d, output: %s", res.ExitCode, res.Output)

	// The allowlist still applies to the command the caller asked for.
	_, err = run.Execute(ctx, "curl example.com", 5*time.Second)
	if errors.Is(err, backends.ErrCommandNotAllowed) {
		fmt.Println("curl refused by AllowedCommands")
	}
}
