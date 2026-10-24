package team_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/backend"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/backend/fake"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

// counted records how often each wrapped tool ran.
type counted struct {
	tools.Tool
	mu *sync.Mutex
	n  map[string]int
}

func (c counted) Call(ctx context.Context, args map[string]any) (any, error) {
	c.mu.Lock()
	c.n[c.Name()]++
	c.mu.Unlock()
	return c.Tool.Call(ctx, args)
}

// Offline, end to end: the PO reads the fake backend the demand names through
// all three tools, and the spec carries the evidence.
func TestOfflineRunPOUsesBackendTools(t *testing.T) {
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ws := t.TempDir()
	root, err := fake.WriteRoot(ws)
	if err != nil {
		t.Fatal(err)
	}
	srv := fake.Serve()
	defer srv.Close()

	var hub atomic.Pointer[team.HubClient]
	var mu sync.Mutex
	n := map[string]int{}
	po := backend.RoleTools(hub.Load, backend.Config{WorkspaceRoot: ws})
	l, err := team.StartLocalWith(c, team.LocalOptions{
		RoleTools: func(role string) []tools.Tool {
			var out []tools.Tool
			for _, tl := range po(role) {
				out = append(out, counted{tl, &mu, n})
			}
			return out
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hub.Store(team.NewHubClient(l.HubURL))
	out, err := team.InvokeWithScope(c, l.ManagerURL, "req-backend", team.DefaultRequest, team.Scope{Roots: []string{root}, URLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"backend_read", "backend_grep", "backend_get"} {
		if n[name] < 1 {
			t.Errorf("PO never called %s: %v", name, n)
		}
	}
	// Not UNVERIFIED also proves the hub counted the request's calls.
	spec := fmt.Sprint(out["spec"])
	t.Logf("calls %v\nspec:\n%s", n, spec)
	if !strings.Contains(spec, "EVIDENCE:") || strings.Contains(spec, "UNVERIFIED") || !strings.Contains(spec, "OPEN ITEMS:") {
		t.Fatalf("spec: %s", spec)
	}
	if fmt.Sprint(out["report"]) == "" {
		t.Fatal("run did not complete")
	}

	// No backend named: the PO calls nothing and says so in OPEN ITEMS.
	before := fmt.Sprint(n)
	mu.Unlock()
	out, err = team.Invoke(c, l.ManagerURL, "req-none", team.DefaultRequest)
	mu.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(n) != before {
		t.Errorf("PO called backend tools with no backend named: %v -> %v", before, n)
	}
	if spec := fmt.Sprint(out["spec"]); !strings.Contains(spec, "OPEN ITEMS:\n- No backend was named") || strings.Contains(spec, "UNVERIFIED") {
		t.Errorf("no-backend spec: %s", spec)
	}
}

// startSeparate brings the team up as separate components wired only through
// URLs: the hub, the manager and every worker each get their own listener and
// their own HubClient. No Go variable carries the call count between them.
func startSeparate(t *testing.T, c context.Context, ws string, offline func([]messages.Message) messages.Message) string {
	t.Helper()
	listen := func() (net.Listener, string) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		return ln, "http://" + ln.Addr().String()
	}
	c, cancel := context.WithCancel(c)
	t.Cleanup(cancel)
	hubLn, hubURL := listen()
	mgrLn, mgrURL := listen()
	go func() { _ = team.RunHub(c, hubLn) }()
	go func() { _ = team.RunManager(c, hubURL, mgrLn, "") }()
	for _, m := range team.Cast {
		ln, _ := listen()
		wc := team.NewHubClient(hubURL) // the worker's own client
		cfg := team.WorkerConfig{ID: m.ID, Role: m.Role, Hub: hubURL, Offline: offline}
		cfg.Tools = backend.RoleTools(func() *team.HubClient { return wc }, backend.Config{WorkspaceRoot: ws})(m.Role)
		go func() { _ = team.RunWorker(c, cfg, ln) }()
	}
	hc := team.NewHubClient(hubURL)
	for i := 0; i < 200; i++ {
		if s, err := hc.State(c); err == nil && len(s.Workers) == len(team.Cast) {
			return mgrURL
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("team did not come up")
	return ""
}

// The evidence rule's call count lives in the hub, so it works when the manager
// and the PO worker share nothing but URLs.
func TestEvidenceRuleWorksAcrossServices(t *testing.T) {
	c, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ws := t.TempDir()
	root, err := fake.WriteRoot(ws)
	if err != nil {
		t.Fatal(err)
	}
	srv := fake.Serve()
	defer srv.Close()
	sc := team.Scope{Roots: []string{root}, URLs: []string{srv.URL}}

	// The scripted PO calls the backend tools: its calls reach the manager through the hub.
	mgr := startSeparate(t, c, ws, nil)
	out, err := team.InvokeWithScope(c, mgr, "req-sep-ok", team.DefaultRequest, sc)
	if err != nil {
		t.Fatal(err)
	}
	if spec := fmt.Sprint(out["spec"]); !strings.Contains(spec, "EVIDENCE:") || strings.Contains(spec, "UNVERIFIED") {
		t.Fatalf("spec must be verified: %s", spec)
	}

	// A PO that writes an EVIDENCE block but never calls a tool is flagged.
	mgr = startSeparate(t, c, ws, func(ms []messages.Message) messages.Message {
		m, _ := messages.LastOfRole(ms, messages.RoleHuman)
		return messages.AI(team.Brain(m.Text()) + "\nEVIDENCE:\n- backend_get /x: 200 (made up)")
	})
	out, err = team.InvokeWithScope(c, mgr, "req-sep-none", team.DefaultRequest, sc)
	if err != nil {
		t.Fatal(err)
	}
	if spec := fmt.Sprint(out["spec"]); !strings.Contains(spec, "UNVERIFIED: no backend evidence") {
		t.Fatalf("spec must be flagged: %s", spec)
	}
}
