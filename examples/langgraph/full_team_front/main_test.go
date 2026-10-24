package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

func clearModelEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"AGENT_MODEL", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(k, "")
	}
	for _, r := range team.Roles {
		t.Setenv("AGENT_MODEL_"+strings.ToUpper(r), "")
	}
}

// A live model chosen with AGENT_MODEL or AGENT_MODEL_<ROLE> and no provider
// key in this process (a proxy, a keyless local model) still loses go and git.
func TestAllowedProgramsWithLiveModelButNoKey(t *testing.T) {
	for _, k := range []string{"AGENT_MODEL", "AGENT_MODEL_QA"} {
		clearModelEnv(t)
		t.Setenv(k, "openai:x")
		got := allowedPrograms(func(string) string { return "" })
		if want := []string{"ls", "grep", "echo"}; !reflect.DeepEqual(got, want) {
			t.Errorf("with %s = %v, want %v", k, got, want)
		}
	}
}

func TestAllowedProgramsWithProviderKey(t *testing.T) {
	clearModelEnv(t)
	env := func(keys map[string]string) func(string) string {
		return func(k string) string { return keys[k] }
	}
	offline := allowedPrograms(env(nil))
	if want := []string{"go", "git", "ls", "grep", "echo"}; !reflect.DeepEqual(offline, want) {
		t.Errorf("offline = %v, want %v", offline, want)
	}
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		got := allowedPrograms(env(map[string]string{k: "x"}))
		if want := []string{"ls", "grep", "echo"}; !reflect.DeepEqual(got, want) {
			t.Errorf("with %s = %v, want %v", k, got, want)
		}
	}
}

// The scripted decider takes its ponytail answer from the rule: a conversation
// without code blocks gets no note, one with a real share of code does.
func TestScriptedPonytailFollowsRule(t *testing.T) {
	q := compress.Question{ID: compress.QPonytail, Kind: compress.KindNoul}
	for _, tc := range []struct {
		share float64
		want  bool
	}{{0, false}, {0.5, true}} {
		ans, err := scripted(context.Background(), compress.State{"code_share": tc.share}, []compress.Question{q})
		if err != nil {
			t.Fatal(err)
		}
		if len(ans) != 1 || ans[0].True != tc.want {
			t.Errorf("code_share %v: answer %+v, want True=%v", tc.share, ans, tc.want)
		}
	}
}

// The offline run must show at least one JSON tool result rewritten to TOON
// with bytes saved. Ponytail notes are not required: the offline run has no
// code blocks, so the rule may give zero notes.
func TestOfflineRunReportsTOON(t *testing.T) {
	if os.Getenv("LAYA_URL") != "" {
		t.Skip("offline run only: LAYA_URL is set")
	}
	for _, r := range team.Roles {
		if team.ModelSpec(r) != "" {
			t.Skipf("offline run only: role %s has a model spec", r)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, err := run(ctx, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m.toonResults < 1 || m.toonSaved < 1 {
		t.Errorf("TOON rewrites = %d saving %d bytes, want >= 1 rewrite with bytes saved", m.toonResults, m.toonSaved)
	}
}

// The PO is read-only: it gets the backend tools and never the shell, which
// every other role keeps.
func TestPOGetsNoShell(t *testing.T) {
	shell := tools.New("shell", "test shell", func(context.Context, struct{}) (any, error) { return "", nil })
	names := func(role string) map[string]bool {
		m := map[string]bool{}
		for _, tl := range workerTools(shell, func() *team.HubClient { return nil }, t.TempDir())(role) {
			m[tl.Name()] = true
		}
		return m
	}
	po := names("po")
	if po["shell"] || !po["backend_read"] || !po["backend_grep"] || !po["backend_get"] {
		t.Errorf("po tools: %v", po)
	}
	for _, r := range []string{"junior", "mid", "senior", "leader", "specialist", "qa"} {
		if !names(r)["shell"] {
			t.Errorf("%s lost the shell", r)
		}
	}
}

// Serve mode gives no role a shell: developer output is text.
func TestServeStackHasNoShell(t *testing.T) {
	st := newStack(t.TempDir(), "", false)
	for _, r := range []string{"po", "junior", "mid", "senior", "leader", "specialist", "qa"} {
		for _, tl := range st.opts.RoleTools(r) {
			if tl.Name() == "shell" {
				t.Errorf("serve gave %s a shell", r)
			}
		}
	}
	if len(workerTools(nil, func() *team.HubClient { return nil }, t.TempDir())("po")) == 0 {
		t.Error("nil shell must only drop the shell, not the other tools")
	}
}

// The demo shell refuses redirections, expansions and backticks before bash sees them.
func TestDemoShellRejectsExpansionAndRedirects(t *testing.T) {
	clearModelEnv(t)
	dir := t.TempDir()
	sh := shellTool(dir, compress.NewRTK(compress.FuncDecider(scripted)))
	for _, cmd := range []string{"echo $ANTHROPIC_API_KEY", "echo x > f", "echo x >> f", "cat < f", "echo `id`"} {
		if _, err := sh.Call(context.Background(), map[string]any{"command": cmd}); err == nil {
			t.Errorf("%q was accepted", cmd)
		}
	}
	if _, err := os.Stat(dir + "/f"); err == nil {
		t.Error("a redirect wrote a file")
	}
	if out, err := sh.Call(context.Background(), map[string]any{"command": "echo hi"}); err != nil || !strings.Contains(fmt.Sprint(out), "hi") {
		t.Errorf("echo hi: %v, %v", out, err)
	}
}

// The shell's child process gets PATH and HOME only: no provider keys.
func TestShellEnvHasNoSecrets(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-fake")
	t.Setenv("OPENAI_API_KEY", "sk-fake2")
	dir := t.TempDir()
	env := shellEnv(dir)
	for _, e := range env {
		if strings.Contains(e, "API_KEY") || strings.Contains(e, "sk-fake") {
			t.Errorf("env leaks %q", e)
		}
	}
	if !slices.Contains(env, "HOME="+dir) {
		t.Errorf("env = %v, want HOME=%s", env, dir)
	}
}

// The demo backlog tool exists only when every role is offline: a live model
// never gets a hard-coded backlog, and with no tool the scripted call is off too.
func TestOpenTasksOnlyOffline(t *testing.T) {
	has := func(st *stack) bool {
		for _, tl := range st.opts.Tools {
			if tl.Name() == tasksToolName {
				return true
			}
		}
		return false
	}
	clearModelEnv(t)
	if st := newStack(t.TempDir(), "", true); !has(st) || st.opts.Offline == nil {
		t.Error("offline stack must register open_tasks and its scripted call")
	}
	t.Setenv("AGENT_MODEL_PO", "openai:x")
	if st := newStack(t.TempDir(), "", true); has(st) || st.opts.Offline != nil {
		t.Error("a live role must switch open_tasks off")
	}
}

// Path-like arguments must stay inside the shell's workspace.
func TestDemoShellConfinesPaths(t *testing.T) {
	clearModelEnv(t)
	dir, outside := t.TempDir(), t.TempDir()
	secret := outside + "/secret.txt"
	if err := os.WriteFile(secret, []byte("topsecret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir+"/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir+"/link"); err != nil {
		t.Fatal(err)
	}
	sh := shellTool(dir, compress.NewRTK(compress.FuncDecider(scripted)))
	call := func(cmd string) error {
		_, err := sh.Call(context.Background(), map[string]any{"command": cmd})
		return err
	}
	for _, cmd := range []string{
		"grep -r x /etc", "ls /", "grep x " + secret, "ls ..", `ls "/etc"`, "ls '/etc'",
		"ls link", "grep -r x link/secret.txt", "ls ~", "ls sub/../..", "echo ok; ls /etc", "echo ok | grep x /etc",
	} {
		err := call(cmd)
		if err == nil || !strings.Contains(err.Error(), "paths outside the workspace are not allowed") {
			t.Errorf("%q: err = %v, want a confinement error", cmd, err)
		}
	}
	for _, cmd := range []string{"ls .", "ls sub", "echo a/b", "grep -n foo /api/coupon", "ls ./sub", "ls sub/.."} {
		if err := call(cmd); err != nil {
			t.Errorf("%q rejected: %v", cmd, err)
		}
	}
}

// Flag-attached paths, file-reading flags, backslashes and braces: what bash
// would expand or read but a whitespace tokenizer does not see as a path.
func TestDemoShellRejectsFlagAndExpansionBypasses(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := t.TempDir()
	sh := shellTool(dir, compress.NewRTK(compress.FuncDecider(scripted)))
	call := func(cmd string) error {
		_, err := sh.Call(context.Background(), map[string]any{"command": cmd})
		return err
	}
	for _, cmd := range []string{
		"grep x --file=/etc/passwd", "grep x -f/etc/shadow", "grep x -f /etc/passwd", "grep --file /etc/passwd x",
		"grep x --exclude-from=/etc/x", "grep x --include-from=f", "grep -R x .", "grep --dereference-recursive x .",
		"grep -nf f x", "grep x -- /etc/passwd", "grep x --include=/etc/passwd",
		`ls \/etc`, "ls {/etc,/tmp}", "ls /{etc,tmp}",
		"go -C /etc list", "git -C /etc status", "git --git-dir=/etc status", "git --work-tree=/etc status",
	} {
		if err := call(cmd); err == nil {
			t.Errorf("%q was accepted", cmd)
		}
	}
	for _, cmd := range []string{"grep -rn --include=*.go foo .", "grep -n foo ."} {
		if err := call(cmd); err != nil {
			t.Errorf("%q rejected: %v", cmd, err)
		}
	}
}

// Real bash runs these through the tool: each reached a host file before the
// guard checked glob matches and split segments outside quotes only.
func TestDemoShellRejectsGlobAndQuoteBypasses(t *testing.T) {
	clearModelEnv(t)
	dir, outside := t.TempDir(), t.TempDir()
	secret := outside + "/secretfile.txt"
	if err := os.WriteFile(secret, []byte("topsecret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir+"/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/sub/a.go", []byte("foo|/etc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sh := shellTool(dir, compress.NewRTK(compress.FuncDecider(scripted)))
	run := func(cmd string) (string, error) {
		out, err := sh.Call(context.Background(), map[string]any{"command": cmd})
		return fmt.Sprint(out), err
	}
	for _, cmd := range []string{
		"grep top " + outside + "/secretf*", "grep top " + outside + "/secretfile.tx[t]", "grep top " + outside + "/secretfile.tx?",
		"grep -rn top " + outside[:len(outside)-3] + "*/secretf*", "ls " + outside + "/*", "echo " + outside + "/*", "ls -d /*",
		"grep --include=" + outside + "/secr* x .",
		`grep -E "^| grep " ` + secret, `ls "a; ls " ` + outside, `ls ";" /etc`, `grep top "x|y" ` + secret,
		"grep -R x .", "grep --deref x .", "grep --dereference-r x .", "grep x --exclude-f=pats", "grep x --include-f=pats", "grep x --fil=pats",
		"X=1 grep -R x .", "X=" + secret + " ls",
	} {
		out, err := run(cmd)
		if err == nil || strings.Contains(out, "topsecret") {
			t.Errorf("%q accepted (err %v), output %.80q", cmd, err, out)
		}
	}
	for _, cmd := range []string{`grep -rE "foo" .`, "grep -rn --include=*.go foo sub", "grep top /nonexistent/zz*", "ls sub/*.go"} {
		if _, err := run(cmd); err != nil {
			t.Errorf("%q rejected: %v", cmd, err)
		}
	}
	// git diff -C is copy detection, not "change directory".
	if deniedFlag("git", "-C", true) || !deniedFlag("git", "-C", false) || !deniedFlag("go", "-C", false) {
		t.Error("-C must be denied only before the subcommand")
	}
}
