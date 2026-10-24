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
	"sync/atomic"
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
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	if _, err := call(t, ts, "backend_get", map[string]any{"url": target.URL + "/x"}); err == nil || hits.Load() != 0 {
		t.Errorf("unnamed host: err %v, %d requests reached it; want refusal and 0", err, hits.Load())
	}
	for _, u := range []string{"http://127.0.0.1:1/x", "file:///etc/passwd", "ftp://" + strings.TrimPrefix(srv.URL, "http://"), "http://user@" + strings.TrimPrefix(srv.URL, "http://") + ".evil/"} {
		if _, err := call(t, ts, "backend_get", map[string]any{"url": u}); err == nil {
			t.Errorf("get %q must be refused", u)
		}
	}
}

func TestGetRefusesRedirectToOtherHost(t *testing.T) {
	var otherHits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherHits.Add(1); fmt.Fprint(w, "other") }))
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
	if otherHits.Load() != 0 {
		t.Errorf("redirect target was hit %d times, want 0", otherHits.Load())
	}
	if out, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL + "/home"}); err != nil || !strings.Contains(out, "home") {
		t.Errorf("redirect within the host: %q, %v", out, err)
	}
}

func TestGetRefusesLoopbackUnlessNamed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); fmt.Fprint(w, "loop") }))
	defer srv.Close()
	ts := Tools(fixed(team.Scope{URLs: []string{"http://api.example.invalid:8080"}}), Config{})
	if _, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL}); err == nil || hits.Load() != 0 {
		t.Errorf("loopback must be refused when another host is named: err %v, %d hits", err, hits.Load())
	}
	ts = Tools(fixed(team.Scope{URLs: []string{srv.URL}}), Config{})
	if out, err := call(t, ts, "backend_get", map[string]any{"url": srv.URL}); err != nil || !strings.Contains(out, "loop") {
		t.Errorf("named loopback: %q, %v", out, err)
	}
}

// The name localhost may be named, and then reaches only loopback IPs. Any
// other name resolving to a special IP is caught at dial time.
func TestGetNamedLocalhost(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); fmt.Fprint(w, "loop") }))
	defer srv.Close()
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	ts := Tools(fixed(team.Scope{URLs: []string{u}}), Config{})
	if out, err := call(t, ts, "backend_get", map[string]any{"url": u}); err != nil || !strings.Contains(out, "loop") {
		t.Errorf("named localhost: %q, %v", out, err)
	}
	n := hits.Load()
	ts = Tools(fixed(team.Scope{URLs: []string{"http://api.example.invalid"}}), Config{})
	if _, err := call(t, ts, "backend_get", map[string]any{"url": u}); err == nil || hits.Load() != n {
		t.Errorf("unnamed localhost: err %v, hits %d -> %d", err, n, hits.Load())
	}
	lit := map[string]bool{"10.9.9.9:80": true}
	for _, tc := range []struct {
		host, addr string
		ok         bool
	}{
		{"localhost", "127.0.0.1:80", true},
		{"localhost", "[::1]:80", true},
		{"localhost", "10.0.0.1:80", false}, // localhost must stay loopback
		{"api.internal", "127.0.0.1:80", false},
		{"api.internal", "169.254.169.254:80", false},
		{"api.internal", "10.0.0.1:80", true},
		{"127.0.0.1", "127.0.0.1:80", false}, // not named as a literal here
		{"10.9.9.9", "10.9.9.9:80", true},
	} {
		if err := checkDial(tc.host, tc.addr, lit); (err == nil) != tc.ok {
			t.Errorf("checkDial(%s, %s) = %v, want ok=%v", tc.host, tc.addr, err, tc.ok)
		}
	}
	if !checkDialOK("127.0.0.1", "127.0.0.1:80", map[string]bool{"127.0.0.1:80": true}) {
		t.Error("a named loopback literal must pass")
	}
	if specialIP("169.254.169.254") == false || specialIP("::1") == false || specialIP("0.0.0.0") == false || specialIP("10.1.2.3") {
		t.Error("specialIP classification")
	}
}

func checkDialOK(host, addr string, lit map[string]bool) bool {
	return checkDial(host, addr, lit) == nil
}

// Empty roots grant nothing, and relative roots hang off the workspace, never
// off the process cwd.
func TestRootsResolveAgainstWorkspace(t *testing.T) {
	ws, root := workspace(t)
	t.Chdir(ws) // an empty root must not become the cwd, here the whole workspace
	ts := Tools(fixed(team.Scope{Roots: []string{""}}), Config{WorkspaceRoot: ws})
	if out, err := call(t, ts, "backend_read", map[string]any{"path": filepath.Join(ws, "secret.txt")}); err == nil {
		t.Errorf("empty root read the workspace: %q", out)
	}
	t.Chdir(t.TempDir())
	ts = Tools(fixed(team.Scope{Roots: []string{"root"}}), Config{WorkspaceRoot: ws})
	if out, err := call(t, ts, "backend_read", map[string]any{"path": "a.txt"}); err != nil || out != "coupon handler body" {
		t.Errorf("relative root: %q, %v", out, err)
	}
	ts = Tools(fixed(team.Scope{Roots: []string{"."}}), Config{WorkspaceRoot: ws})
	if out, err := call(t, ts, "backend_read", map[string]any{"path": filepath.Join(root, "a.txt")}); err != nil || out != "coupon handler body" {
		t.Errorf("root '.': %q, %v", out, err)
	}
}

// grep stops on a cancelled context and after maxGrepFiles files.
func TestGrepLimits(t *testing.T) {
	ws, root := workspace(t)
	big := filepath.Join(root, "node_modules")
	os.MkdirAll(big, 0o755)
	for i := range 20 {
		os.WriteFile(filepath.Join(big, fmt.Sprintf("f%02d.js", i)), []byte("coupon\n"), 0o644)
	}
	ts := Tools(fixed(team.Scope{Roots: []string{root}}), Config{WorkspaceRoot: ws})
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := find(t, ts, "backend_grep").Call(cctx, map[string]any{"pattern": "coupon", "path": "."}); err == nil {
		t.Errorf("cancelled grep: %v", out)
	}
	old := maxGrepFiles
	maxGrepFiles = 5
	defer func() { maxGrepFiles = old }()
	out, err := call(t, ts, "backend_grep", map[string]any{"pattern": "coupon", "path": "."})
	if err != nil || strings.Count(out, "coupon") > 5 || !strings.Contains(out, "stopped after 5 files") {
		t.Errorf("file cap: %q, %v", out, err)
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

// Only calls that passed the guards and reached the backend are reported,
// under the request id the tools resolved.
func TestReportsGuardedCallsPerRequest(t *testing.T) {
	ws, root := workspace(t)
	resolve := func(context.Context) (string, team.Scope, error) {
		return "req-count", team.Scope{Roots: []string{root}}, nil
	}
	var got []string
	ts := newTools(resolve, Config{WorkspaceRoot: ws, Report: func(_ context.Context, id string) error {
		got = append(got, id)
		return nil
	}})
	call(t, ts, "backend_read", map[string]any{"path": "../secret.txt"})
	if len(got) != 0 {
		t.Fatalf("refused call reported: %v", got)
	}
	call(t, ts, "backend_read", map[string]any{"path": "a.txt"})
	call(t, ts, "backend_grep", map[string]any{"pattern": "x", "path": "."})
	if len(got) != 2 || got[0] != "req-count" || got[1] != "req-count" {
		t.Fatalf("reported = %v, want two for req-count", got)
	}
}

// A reporting failure is logged and does not fail the tool call: the hub's
// count is then short, which the manager treats as unverified.
func TestReportFailureDoesNotFailTheCall(t *testing.T) {
	ws, root := workspace(t)
	resolve := func(context.Context) (string, team.Scope, error) {
		return "r", team.Scope{Roots: []string{root}}, nil
	}
	ts := newTools(resolve, Config{WorkspaceRoot: ws, Report: func(context.Context, string) error { return errors.New("hub down") }})
	if out, err := call(t, ts, "backend_read", map[string]any{"path": "a.txt"}); err != nil || out == "" {
		t.Fatalf("read = %q, %v; the call must succeed", out, err)
	}
}

// after is the body of a backend_get answer.
func after(out string) string { _, b, _ := strings.Cut(out, "\n\n"); return b }
