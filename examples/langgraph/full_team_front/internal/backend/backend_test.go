package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

func fixed(s team.Scope) func(context.Context) (team.Scope, error) {
	return func(context.Context) (team.Scope, error) { return s, nil }
}

func find(t *testing.T, ts []tools.Tool, name string) tools.Tool {
	t.Helper()
	for _, tl := range ts {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("no tool %s", name)
	return nil
}

func call(t *testing.T, ts []tools.Tool, name string, args map[string]any) (string, error) {
	t.Helper()
	out, err := find(t, ts, name).Call(context.Background(), args)
	return fmt.Sprint(out), err
}

// workspace: ws/root/a.txt (inside), ws/secret.txt (workspace, outside root),
// root/link -> ../secret.txt (symlink escape).
func workspace(t *testing.T) (ws, root string) {
	t.Helper()
	ws = t.TempDir()
	root = filepath.Join(ws, "root")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{
		filepath.Join(root, "a.txt"):       "coupon handler body",
		filepath.Join(root, "sub", "b.ts"): "export const coupon = 1\nother line\n",
		filepath.Join(ws, "secret.txt"):    "coupon SECRET",
	} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(ws, "secret.txt"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return ws, root
}

func TestReadRefusesOutsideRoots(t *testing.T) {
	ws, root := workspace(t)
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "x.txt"), []byte("x"), 0o644)
	ts := Tools(fixed(team.Scope{Roots: []string{root}}), Config{WorkspaceRoot: ws})
	for _, p := range []string{
		"../secret.txt",
		"sub/../../secret.txt",
		filepath.Join(ws, "secret.txt"),
		filepath.Join(other, "x.txt"),
		"/etc/passwd",
		"link",
	} {
		if out, err := call(t, ts, "backend_read", map[string]any{"path": p}); err == nil || strings.Contains(out, "SECRET") {
			t.Errorf("read %q: %q, %v; want refusal", p, out, err)
		}
	}
	// A root outside the workspace is refused even though the scope names it.
	ts = Tools(fixed(team.Scope{Roots: []string{other}}), Config{WorkspaceRoot: ws})
	if _, err := call(t, ts, "backend_read", map[string]any{"path": filepath.Join(other, "x.txt")}); err == nil {
		t.Error("root outside the workspace must be refused")
	}
	// A sibling sharing the root's prefix is not inside it.
	os.MkdirAll(root+"2", 0o755)
	os.WriteFile(filepath.Join(root+"2", "c.txt"), []byte("c"), 0o644)
	ts = Tools(fixed(team.Scope{Roots: []string{root}}), Config{WorkspaceRoot: ws})
	if _, err := call(t, ts, "backend_read", map[string]any{"path": filepath.Join(root+"2", "c.txt")}); err == nil {
		t.Error("prefix sibling must be refused")
	}
}

func TestReadWithinRoot(t *testing.T) {
	ws, root := workspace(t)
	ts := Tools(fixed(team.Scope{Roots: []string{root}}), Config{WorkspaceRoot: ws})
	for _, p := range []string{"a.txt", filepath.Join(root, "a.txt")} {
		if out, err := call(t, ts, "backend_read", map[string]any{"path": p}); err != nil || out != "coupon handler body" {
			t.Errorf("read %q: %q, %v", p, out, err)
		}
	}
	ts = Tools(fixed(team.Scope{Roots: []string{root}}), Config{WorkspaceRoot: ws, MaxBytes: 6})
	out, err := call(t, ts, "backend_read", map[string]any{"path": "a.txt"})
	if err != nil || !strings.HasPrefix(out, "coupon\n") || strings.Contains(out, "handler") || !strings.Contains(out, "truncated") {
		t.Errorf("truncated read: %q, %v", out, err)
	}
}

func TestGrepStaysInsideRoot(t *testing.T) {
	ws, root := workspace(t)
	os.Symlink(ws, filepath.Join(root, "up")) // directory symlink out of the root
	ts := Tools(fixed(team.Scope{Roots: []string{root}}), Config{WorkspaceRoot: ws})
	out, err := call(t, ts, "backend_grep", map[string]any{"pattern": "coupon", "path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt:1:") || !strings.Contains(out, filepath.Join("sub", "b.ts")+":1:") || strings.Contains(out, "SECRET") {
		t.Errorf("grep: %q", out)
	}
	if _, err := call(t, ts, "backend_grep", map[string]any{"pattern": "coupon", "path": ".."}); err == nil {
		t.Error("grep outside the root must be refused")
	}
	ts = Tools(fixed(team.Scope{Roots: []string{root}}), Config{WorkspaceRoot: ws, MaxBytes: 10})
	if out, _ := call(t, ts, "backend_grep", map[string]any{"pattern": "coupon", "path": "."}); len(out) > 10+len("\n[truncated]") {
		t.Errorf("grep output not capped: %q", out)
	}
}

func TestGetRefusesNonGetAndUnnamedHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Method+" ok")
	}))
	defer srv.Close()
	ts := Tools(fixed(team.Scope{URLs: []string{srv.URL}}), Config{})
	props := find(t, ts, "backend_get").Schema().Properties
	if len(props) != 1 || props["url"] == nil {
		t.Fatalf("backend_get must take only url, got %v", props)
	}
	if out, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL + "/x"}); err != nil || !strings.Contains(out, "GET ok") {
		t.Fatalf("named host: %q, %v", out, err)
	}
	for _, u := range []string{"http://127.0.0.1:1/x", "file:///etc/passwd", "ftp://" + strings.TrimPrefix(srv.URL, "http://"), "http://user@" + strings.TrimPrefix(srv.URL, "http://") + ".evil/"} {
		if _, err := call(t, ts, "backend_get", map[string]any{"url": u}); err == nil {
			t.Errorf("get %q must be refused", u)
		}
	}
}

func TestGetRefusesRedirectToOtherHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "other") }))
	defer other.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, other.URL+"/", http.StatusFound)
		case "/home":
			http.Redirect(w, r, srv.URL+"/ok", http.StatusFound)
		default:
			fmt.Fprint(w, "home")
		}
	}))
	defer srv.Close()
	ts := Tools(fixed(team.Scope{URLs: []string{srv.URL}}), Config{})
	if out, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL + "/away"}); err == nil || strings.Contains(out, "other") {
		t.Errorf("redirect to another host: %q, %v", out, err)
	}
	if out, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL + "/home"}); err != nil || !strings.Contains(out, "home") {
		t.Errorf("redirect within the host: %q, %v", out, err)
	}
}

func TestGetRefusesLoopbackUnlessNamed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "loop") }))
	defer srv.Close()
	ts := Tools(fixed(team.Scope{URLs: []string{"http://api.example.invalid:8080"}}), Config{})
	if _, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL}); err == nil {
		t.Error("loopback must be refused when another host is named")
	}
	ts = Tools(fixed(team.Scope{URLs: []string{srv.URL}}), Config{})
	if out, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL}); err != nil || !strings.Contains(out, "loop") {
		t.Errorf("named loopback: %q, %v", out, err)
	}
}

// A named host that resolves to a loopback IP is caught at dial time: only an
// IP literal in the scope unlocks a loopback address.
func TestGetRefusesNameResolvingToLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "loop") }))
	defer srv.Close()
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	ts := Tools(fixed(team.Scope{URLs: []string{u}}), Config{})
	if out, err := call(t, ts, "backend_get", map[string]any{"url": u}); err == nil || !strings.Contains(err.Error(), "not named as an IP") {
		t.Errorf("localhost -> loopback must be refused by the dial guard: %q, %v", out, err)
	}
	if specialIP("169.254.169.254") == false || specialIP("::1") == false || specialIP("0.0.0.0") == false || specialIP("10.1.2.3") {
		t.Error("specialIP classification")
	}
}

func TestGetBodyCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", 100)) }))
	defer srv.Close()
	ts := Tools(fixed(team.Scope{URLs: []string{srv.URL}}), Config{MaxBytes: 10})
	out, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL})
	if err != nil || strings.Count(after(out), "x") != 10 || !strings.Contains(out, "truncated") {
		t.Errorf("capped body: %q, %v", out, err)
	}
}

func TestNoScopeMeansNoAccess(t *testing.T) {
	ws, root := workspace(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "x") }))
	defer srv.Close()
	scopes := map[string]func(context.Context) (team.Scope, error){
		"empty":    fixed(team.Scope{}),
		"app only": fixed(team.Scope{AppURL: srv.URL}),
		"no scope": func(context.Context) (team.Scope, error) { return team.Scope{}, team.ErrNoScope },
		"hub down": func(context.Context) (team.Scope, error) {
			return team.Scope{Roots: []string{root}}, errors.New("down")
		},
	}
	for name, sc := range scopes {
		ts := Tools(sc, Config{WorkspaceRoot: ws})
		for tool, args := range map[string]map[string]any{
			"backend_read": {"path": filepath.Join(root, "a.txt")},
			"backend_grep": {"pattern": "coupon", "path": root},
			"backend_get":  {"url": srv.URL},
		} {
			if out, err := call(t, ts, tool, args); err == nil || !tools.IsToolError(err) {
				t.Errorf("%s: %s = %q, %v; want a tool error", name, tool, out, err)
			}
		}
	}
}

// Only calls that passed the guards and reached the backend are counted.
func TestCallsCountsGuardedCallsPerRequest(t *testing.T) {
	ws, root := workspace(t)
	resolve := func(context.Context) (string, team.Scope, error) {
		return "req-count", team.Scope{Roots: []string{root}}, nil
	}
	ts := newTools(resolve, Config{WorkspaceRoot: ws})
	call(t, ts, "backend_read", map[string]any{"path": "../secret.txt"})
	if n := Calls("req-count"); n != 0 {
		t.Fatalf("refused call counted: %d", n)
	}
	call(t, ts, "backend_read", map[string]any{"path": "a.txt"})
	call(t, ts, "backend_grep", map[string]any{"pattern": "x", "path": "."})
	if n := Calls("req-count"); n != 2 {
		t.Fatalf("calls = %d, want 2", n)
	}
}

// after is the body of a backend_get answer.
func after(out string) string { _, b, _ := strings.Cut(out, "\n\n"); return b }
