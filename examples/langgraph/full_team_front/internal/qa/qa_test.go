package qa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

func scopeOf(s team.Scope, err error) func(context.Context) (team.Scope, error) {
	return func(context.Context) (team.Scope, error) { return s, err }
}

func call(t *testing.T, tl tools.Tool) string {
	t.Helper()
	out, err := tl.Call(context.Background(), map[string]any{"spec": "AC1: coupon shows discount"})
	if err != nil {
		t.Fatalf("must be a normal result, got error: %v", err)
	}
	return fmt.Sprint(out)
}

func TestBrowserTestNotRunWithoutCommand(t *testing.T) {
	tl := Tool(scopeOf(team.Scope{AppURL: "http://localhost:3000"}, nil), CommandRunner(""))
	if out := call(t, tl); !strings.HasPrefix(out, "NOT RUN:") {
		t.Fatalf("got %q", out)
	}
}

func TestBrowserTestNotRunWithoutAppURL(t *testing.T) {
	called := false
	run := func(context.Context, string, string) (string, error) { called = true; return "x", nil }
	for _, sc := range []func(context.Context) (team.Scope, error){
		scopeOf(team.Scope{Roots: []string{"/x"}}, nil),
		scopeOf(team.Scope{}, team.ErrNoScope),
	} {
		if out := call(t, Tool(sc, run)); !strings.HasPrefix(out, "NOT RUN:") {
			t.Fatalf("got %q", out)
		}
	}
	if called {
		t.Fatal("runner must not run without an app url")
	}
}

func TestBrowserTestRejectsNonHTTPAppURL(t *testing.T) {
	called := false
	run := func(context.Context, string, string) (string, error) { called = true; return "x", nil }
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "http://", "localhost:3000", "ftp://h/x"} {
		if out := call(t, Tool(scopeOf(team.Scope{AppURL: u}, nil), run)); !strings.HasPrefix(out, "NOT RUN:") {
			t.Fatalf("%s: got %q", u, out)
		}
	}
	if called {
		t.Fatal("runner must not run for a non-http app url")
	}
}

func TestBrowserTestRunsRunnerWithScopeURL(t *testing.T) {
	var gotURL, gotSpec string
	run := func(_ context.Context, u, s string) (string, error) { gotURL, gotSpec = u, s; return "PASS", nil }
	tl := Tool(scopeOf(team.Scope{AppURL: "https://app.test/x"}, nil), run)
	if out := call(t, tl); out != "PASS" || gotURL != "https://app.test/x" || !strings.Contains(gotSpec, "AC1") {
		t.Fatalf("out %q url %q spec %q", out, gotURL, gotSpec)
	}
	big := strings.Repeat("a", MaxSpec+1)
	if _, err := tl.Call(context.Background(), map[string]any{"spec": big}); err == nil {
		t.Fatal("oversized spec must be refused")
	}
}

// Helper process: the re-exec'd test binary prints what it was given.
func TestMain(m *testing.M) {
	if os.Getenv("QA_HELPER") == "1" {
		b, _ := os.ReadFile(os.Getenv("SPEC_FILE"))
		wd, _ := os.Getwd()
		fmt.Printf("args=%q url=%s spec=%s dir=%s file=%s", os.Args[1:], os.Getenv("APP_URL"), b, wd, os.Getenv("SPEC_FILE"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCommandRunnerUsesNoShell(t *testing.T) {
	t.Setenv("QA_HELPER", "1")
	work := t.TempDir()
	x := filepath.Join(work, "x")
	if err := os.WriteFile(x, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	// If a shell parsed this, "rm x" would run in the child's directory; the
	// literal ';' and the words arrive as argv instead.
	run := CommandRunner(os.Args[0] + " echo ok; rm " + x)
	out, err := run(context.Background(), "http://app.test", "SPEC; $(touch "+x+".pwn)")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `args=["echo" "ok;" "rm"`) || !strings.Contains(out, "url=http://app.test") || !strings.Contains(out, "spec=SPEC; $(touch") {
		t.Fatalf("unexpected output: %s", out)
	}
	if b, err := os.ReadFile(x); err != nil || string(b) != "keep" {
		t.Fatalf("x was touched: %v %q", err, b)
	}
	if _, err := os.Stat(x + ".pwn"); err == nil {
		t.Fatal("spec text was interpolated")
	}
	// The spec dir is gone afterwards.
	i := strings.Index(out, "file=")
	if _, err := os.Stat(out[i+5:]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp spec file left behind: %v", err)
	}
}

func TestCommandRunnerCapsOutput(t *testing.T) {
	t.Setenv("QA_HELPER", "1")
	r := commandRunner(os.Args[0], 10, 0)
	out, err := r(context.Background(), "http://a.test", "spec")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 10+64 || !strings.Contains(out, "truncated") {
		t.Fatalf("not capped: %q", out)
	}
}

func TestToolNotGivenToOtherRoles(t *testing.T) {
	rt := RoleTools(func() *team.HubClient { return nil }, "x")
	for _, r := range []string{"po", "engineer", "leader", "specialist", "manager", ""} {
		if len(rt(r)) != 0 {
			t.Fatalf("role %q got the tool", r)
		}
	}
	if ts := rt("qa"); len(ts) != 1 || ts[0].Name() != "browser_test" {
		t.Fatalf("qa tools: %v", ts)
	}
	// No hub: no scope, so nothing runs.
	if out := call(t, rt("qa")[0]); !strings.HasPrefix(out, "NOT RUN:") {
		t.Fatalf("got %q", out)
	}
}
