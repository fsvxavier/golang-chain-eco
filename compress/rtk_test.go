package compress

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

func fakeRTKPath(string) (string, error) { return "/usr/bin/rtk", nil }

func noRTKPath(string) (string, error) { return "", errors.New("not found") }

func routeDecider(yes bool) Decider {
	return FuncDecider(func(_ context.Context, _ State, qs []Question) ([]Answer, error) {
		out := make([]Answer, len(qs))
		for i, q := range qs {
			out[i] = Answer{ID: q.ID, Kind: q.Kind, True: yes, P: map[bool]float64{true: 1, false: 0}[yes]}
		}
		return out, nil
	})
}

func TestRTKRewrite(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		command string
		look    func(string) (string, error)
		yes     bool
		want    string
	}{
		{"simple routed", "go test ./...", fakeRTKPath, true, "rtk go test ./..."},
		{"rtk missing", "go test ./...", noRTKPath, true, "go test ./..."},
		{"empty", "", fakeRTKPath, true, ""},
		{"whitespace", "   ", fakeRTKPath, true, "   "},
		{"pipe", "a | b", fakeRTKPath, true, "a | b"},
		{"and", "a && b", fakeRTKPath, true, "a && b"},
		{"semicolon", "a; b", fakeRTKPath, true, "a; b"},
		{"redirect", "a > f", fakeRTKPath, true, "a > f"},
		{"input redirect", "a < f", fakeRTKPath, true, "a < f"},
		{"command substitution", "echo $(x)", fakeRTKPath, true, "echo $(x)"},
		{"backtick", "echo `x`", fakeRTKPath, true, "echo `x`"},
		{"leading assignment", "FOO=1 go test", fakeRTKPath, true, "FOO=1 go test"},
		{"decider says no", "go test ./...", fakeRTKPath, false, "go test ./..."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &RTK{Decider: routeDecider(c.yes), LookPath: c.look}
			if got := r.Rewrite(ctx, c.command); got != c.want {
				t.Fatalf("Rewrite(%q) = %q, want %q", c.command, got, c.want)
			}
		})
	}
}

// sizedExec returns outputs of the given sizes, one per Execute call.
type sizedExec struct {
	sizes []int
	i     int
}

func (s *sizedExec) Execute(_ context.Context, _ string, _ time.Duration) (backends.ExecResult, error) {
	n := s.sizes[s.i]
	s.i++
	return backends.ExecResult{Output: strings.Repeat("x", n)}, nil
}

func TestRTKExecutorTracksMax(t *testing.T) {
	ctx := context.Background()
	var seen []int
	d := FuncDecider(func(_ context.Context, st State, qs []Question) ([]Answer, error) {
		seen = append(seen, st["prev_output_bytes"].(int))
		return []Answer{{ID: QRouteRTK, Kind: KindNoul, True: true, P: 1}}, nil
	})
	r := &RTK{Decider: d, LookPath: fakeRTKPath}
	ex := r.Executor(&sizedExec{sizes: []int{10, 50, 20, 5}})

	for i := 0; i < 4; i++ {
		cmd := r.Rewrite(ctx, "go test ./...")
		if _, err := ex.Execute(ctx, cmd, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Before any run the size is unknown; after runs of 10, 50, 20 the
	// recorded value is the largest seen (50), not the last one (20).
	want := []int{-1, 10, 50, 50}
	if len(seen) != len(want) {
		t.Fatalf("decider calls = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("prev_output_bytes = %v, want %v", seen, want)
		}
	}
}

// Real rtk on PATH: error lines of a failing command must survive the rewrite.
func TestRTKRealBinary(t *testing.T) {
	if _, err := exec.LookPath("rtk"); err != nil {
		t.Skip("rtk not on PATH")
	}
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module tiny\n\ngo 1.21\n"), 0o600))
	must(os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {\n\tvar x int = \"not an int\"\n\t_ = x\n}\n"), 0o600))

	r := NewRTK(RuleDecider{})
	const cmd = "go vet ./..."
	if got := r.Rewrite(context.Background(), cmd); got != "rtk "+cmd {
		t.Fatalf("Rewrite(%q) = %q, want rtk routing", cmd, got)
	}
	be := backends.LocalShell{Filesystem: backends.Filesystem{Root: root}, Quiet: true, Rewrite: r.Rewrite}
	res, err := be.Execute(context.Background(), cmd, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("go vet on broken code exited 0: %s", res.Output)
	}
	if !strings.Contains(res.Output, "not an int") || !strings.Contains(res.Output, "main.go") {
		t.Fatalf("compile error lost through rtk: %s", res.Output)
	}
}
