package team_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
		CallCounter: backend.Calls,
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
	// Not UNVERIFIED also proves backend.Calls saw the request's calls.
	spec := fmt.Sprint(out["spec"])
	t.Logf("calls %v\nspec:\n%s", n, spec)
	if !strings.Contains(spec, "EVIDENCE:") || strings.Contains(spec, "UNVERIFIED") || !strings.Contains(spec, "OPEN ITEMS:") {
		t.Fatalf("spec: %s", spec)
	}
	if fmt.Sprint(out["report"]) == "" {
		t.Fatal("run did not complete")
	}
}
