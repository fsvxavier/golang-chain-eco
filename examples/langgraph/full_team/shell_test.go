package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

func testShell(t *testing.T) (tools.Tool, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	dec, _ := decider()
	return shellTool(dir, compress.NewRTK(dec)), dir
}

func runShell(t *testing.T, sh tools.Tool, cmd string) (string, error) {
	t.Helper()
	out, err := sh.Call(context.Background(), map[string]any{"command": cmd})
	return fmt.Sprint(out), err
}

func TestShellRejectsPathsOutsideWorkspace(t *testing.T) {
	sh, dir := testShell(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("topsecret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{
		"grep -r x /etc", "ls /", "grep topsecret " + secret, "ls ..", `ls "/etc"`, "ls ~", "ls ~/x",
		"ls link", "grep topsecret link/secret.txt", "ls link/..", "ls /et*", "echo hi; ls /etc", "echo hi | grep x /etc/passwd",
	} {
		_, err := runShell(t, sh, cmd)
		if err == nil || !strings.Contains(err.Error(), "shell: paths outside the workspace are not allowed") {
			t.Errorf("%q: err = %v, want a path rejection", cmd, err)
		}
	}
}

func TestShellAllowsWorkspacePaths(t *testing.T) {
	sh, _ := testShell(t)
	for _, cmd := range []string{"ls .", "ls sub", "ls ./sub", "echo a/b", "grep -n foo /api/coupon", "ls -la"} {
		if _, err := runShell(t, sh, cmd); err != nil {
			t.Errorf("%q: %v", cmd, err)
		}
	}
}

func TestShellRejectsExpansionAndRedirection(t *testing.T) {
	sh, _ := testShell(t)
	for _, cmd := range []string{"echo $ANTHROPIC_API_KEY", "echo x > f", "echo `id`", "grep x < f"} {
		if _, err := runShell(t, sh, cmd); err == nil {
			t.Errorf("%q was accepted", cmd)
		}
	}
}

func TestShellEnvHasNoSecrets(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fake-a")
	t.Setenv("OPENAI_API_KEY", "fake-o")
	dir := t.TempDir()
	env := shellEnv(dir)
	for _, kv := range env {
		if strings.Contains(kv, "API_KEY") || strings.Contains(kv, "fake-") {
			t.Errorf("secret in child env: %s", kv)
		}
	}
	if !slices.Contains(env, "HOME="+dir) {
		t.Errorf("HOME is not the scratch dir: %v", env)
	}
}

func TestAllowedProgramsWithLiveModel(t *testing.T) {
	t.Setenv("AGENT_MODEL", "anthropic:claude-haiku-5-5")
	got := allowedPrograms(func(string) string { return "" })
	if want := []string{"ls", "grep", "echo"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Each of these reached a host file before: through a flag value, an escape or
// a brace the tokeniser reads differently from the shell.
func TestShellRejectsBypasses(t *testing.T) {
	sh, dir := testShell(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("topsecret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pats"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{
		"grep x --file=" + secret, "grep x -f" + secret, "grep x -f pats", "grep x --file pats", "grep -nf pats .",
		"grep -r x --exclude-from=" + secret, "grep -r x --include-from=pats", "grep x --regexp=" + secret + " -e" + secret,
		"grep x -e" + secret, "grep x -- " + secret, "grep x -- ..", "grep -R x .", "grep --dereference-recursive x .",
		`ls \/etc`, "ls {/etc,/tmp}", "ls /{etc,tmp}", "echo \\$HOME",
		"git -C / status", "git --git-dir=" + outside + " status", "git --work-tree=/ status", "go -C / env",
	} {
		out, err := runShell(t, sh, cmd)
		if err == nil {
			t.Errorf("%q accepted, output %.80q", cmd, out)
		}
	}
}

func TestShellAllowsPlainFlags(t *testing.T) {
	sh, _ := testShell(t)
	for _, cmd := range []string{"grep -rn x .", "grep -r --include=*.go x .", "grep -n -C 2 foo sub", "grep -e foo -- sub", "ls -la sub"} {
		if _, err := runShell(t, sh, cmd); err != nil {
			t.Errorf("%q: %v", cmd, err)
		}
	}
}

func TestShellRejectsGlobsAndQuotedSeparators(t *testing.T) {
	sh, _ := testShell(t)
	for _, cmd := range []string{
		"grep root /etc/passw[d]", "ls /et?", "echo /etc/*", "ls -d /*", "grep -rn x /e*/host*",
		`grep -E "^| grep " /etc/passwd`, `ls "a; ls " /etc`, `ls "a & ls " /etc`, `echo "unterminated /etc`,
		"grep --deref x .", "grep --dereference-r x .", "grep -r --exclude-f=pats x .", "grep -r --include-f pats x .", "grep x --fil pats",
		"X=1 grep -R x .", "git -C . status",
	} {
		out, err := runShell(t, sh, cmd)
		if err == nil {
			t.Errorf("%q accepted, output %.80q", cmd, out)
		}
	}
}

func TestShellAllowsNarrowedFlags(t *testing.T) {
	sh, _ := testShell(t)
	for _, cmd := range []string{"grep -r --include=*.go x .", "grep --color=never x sub", "grep -r --exclude-dir=sub x .", "grep -r --exclude=*.md x .", "git diff -M", "X=1 grep -r x ."} {
		if _, err := runShell(t, sh, cmd); err != nil && strings.Contains(err.Error(), "shell:") {
			t.Errorf("%q: %v", cmd, err)
		}
	}
}
