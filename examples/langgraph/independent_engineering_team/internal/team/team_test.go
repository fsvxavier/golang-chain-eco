package team

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/mcp"
	"github.com/fsvxavier/golang-chain-eco/remote"
)

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// Two jobs, one senior: exactly one is reserved; the other waits until the
// first completes. 50 racing submitters never double-book a worker.
func TestReservationIsAtomic(t *testing.T) {
	h := NewHub(time.Minute)
	h.Register("senior-1", "senior", "")
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() { defer wg.Done(); h.Submit(Job{Prefer: []string{"senior"}, Thread: fmt.Sprint("t", i)}) }()
	}
	wg.Wait()
	running := 0
	for _, j := range h.jobs {
		if j.Status == "running" {
			running++
		}
	}
	if running != 1 || len(h.Snapshot().Queued) != 49 {
		t.Fatalf("running=%d queued=%d", running, len(h.Snapshot().Queued))
	}
	j, _ := h.Claim(ctx(t), "senior-1", time.Second)
	if err := h.Complete("senior-1", j.ID, "ok", ""); err != nil {
		t.Fatal(err)
	}
	if h.Snapshot().Workers[0].Job == "" || len(h.Snapshot().Queued) != 48 {
		t.Fatal("completing must hand the worker the next queued job")
	}
}

func TestPreferenceOrder(t *testing.T) {
	h := NewHub(time.Minute)
	h.Register("junior-1", "junior", "")
	h.Register("senior-1", "senior", "")
	id := h.Submit(Job{Prefer: []string{"senior", "mid"}, Thread: "high"})
	if h.jobs[id].Worker != "senior-1" {
		t.Fatalf("high work must go to the senior, got %q", h.jobs[id].Worker)
	}
	id2 := h.Submit(Job{Prefer: []string{"senior", "mid"}, Thread: "high2"})
	if h.jobs[id2].Status != "queued" {
		t.Fatal("high work must wait for a senior or mid, never take the junior")
	}
}

// A worker that stops heartbeating loses its lease; its job goes back to the
// queue and another worker finishes it. The dead worker's late result is dropped.
func TestLeaseExpiryRequeues(t *testing.T) {
	h := NewHub(time.Second)
	now := time.Now()
	h.now = func() time.Time { return now }
	h.Register("qa-1", "qa", "")
	h.Register("qa-2", "qa", "")
	id := h.Submit(Job{Prefer: []string{"qa"}, Thread: "t"})
	first := h.jobs[id].Worker
	other := "qa-1"
	if first == "qa-1" {
		other = "qa-2"
	}
	now = now.Add(2 * time.Second)
	h.Heartbeat(other)
	h.Reap()
	if h.jobs[id].Worker != other {
		t.Fatalf("job must move to %s, is on %q (%s)", other, h.jobs[id].Worker, h.jobs[id].Status)
	}
	if err := h.Complete(first, id, "late", ""); err != ErrNotOwner {
		t.Fatalf("late result must be rejected, got %v", err)
	}
	if err := h.Complete(other, id, "done", ""); err != nil {
		t.Fatal(err)
	}
}

func TestJobFailsAfterMaxAttempts(t *testing.T) {
	h := NewHub(time.Second)
	now := time.Now()
	h.now = func() time.Time { return now }
	h.Register("qa-1", "qa", "")
	id := h.Submit(Job{Prefer: []string{"qa"}, Thread: "t"})
	for range h.MaxAttempts {
		now = now.Add(2 * time.Second)
		h.Reap() // the worker went silent: lease lost, job requeued or failed
		if h.jobs[id].Status == "failed" {
			return
		}
		h.Register("qa-1", "qa", "") // it comes back and is handed the job again
	}
	t.Fatal("job must fail after MaxAttempts lost leases")
}

// The knowledge techniques and the team memory answer through MCP, offline.
func TestKnowledgeAndMemoryOverMCP(t *testing.T) {
	c := ctx(t)
	chat, _, _ := KnowledgeModel()
	emb, _ := Embedder()
	srv, err := NewMCP(c, chat, emb)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(time.Minute)
	ts := newTestServer(t, hub.Handler(srv.Handler()))
	mc, err := mcp.Connect(c, mcp.HTTP(ts.URL+"/mcp", nil))
	if err != nil {
		t.Fatal(err)
	}
	for tool, want := range map[string]string{
		"rag_answer": "[1]", "cag_answer": "[1]", "kag_query": "Atomic", "knowledge_answer": "[1]",
	} {
		out, err := mc.CallTool(c, tool, map[string]any{"question": "how must the coupon in a payment be guarded? atomic reservation idempotency acceptance criteria"})
		if err != nil || !strings.Contains(strings.ToLower(fmt.Sprint(out)), strings.ToLower(want)) {
			t.Fatalf("%s: %v / %v", tool, out, err)
		}
	}
	_, _ = mc.CallTool(c, "memory_put", map[string]any{"scope": "r1", "key": "k1", "text": "architect: reservation must expire with a TTL"})
	_, _ = mc.CallTool(c, "memory_put", map[string]any{"scope": "r2", "key": "k2", "text": "other request"})
	out, _ := mc.CallTool(c, "memory_search", map[string]any{"scope": "r1", "query": "reservation TTL"})
	if !strings.Contains(fmt.Sprint(out), "TTL") || strings.Contains(fmt.Sprint(out), "other request") {
		t.Fatalf("memory must be scoped by request: %v", out)
	}
}

func TestNormalizeModel(t *testing.T) {
	for in, want := range map[string]string{
		"anthropic:opus_5_5":        "anthropic:claude-opus-5-5",
		"anthropic/haiku_5_5":       "anthropic:claude-haiku-5-5",
		"anthropic:claude-opus-5-5": "anthropic:claude-opus-5-5",
		"openai:gpt-4o-mini":        "openai:gpt-4o-mini",
	} {
		if got := normalize(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
	t.Setenv("AGENT_MODEL", "openai:gpt-4o-mini")
	t.Setenv("AGENT_MODEL_PO", "anthropic:opus_5_5")
	if ModelSpec("po") != "anthropic:claude-opus-5-5" || ModelSpec("qa") != "openai:gpt-4o-mini" {
		t.Fatal("AGENT_MODEL_<ROLE> must win over AGENT_MODEL")
	}
}

// The whole team over real HTTP: every task ships, both gates return a task
// once, and nobody is double-booked.
func TestTeamDeliversOffline(t *testing.T) {
	c := ctx(t)
	l, err := StartLocal(c)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Invoke(c, l.ManagerURL, "req-1", DefaultRequest)
	if err != nil {
		t.Fatal(err)
	}
	report := fmt.Sprint(out["report"])
	if !strings.Contains(report, "Blocked: none") || !strings.Contains(report, "T3") {
		t.Fatalf("report: %s", report)
	}
	st, _ := out["status"].(map[string]any)
	if len(st) != 6 { // the request and five tasks
		t.Fatalf("status: %v", st)
	}
	for id, v := range st {
		if v != "completed" {
			t.Fatalf("%q ended at %v, want completed", id, v)
		}
	}
	hist := fmt.Sprint(out["history"])
	if !strings.Contains(hist, "review returned") || !strings.Contains(hist, "qa returned") {
		t.Fatalf("gates must return a task once: %s", hist)
	}
}

func TestBridgeQueueAndAskUnknown(t *testing.T) {
	hub := NewHub(time.Minute)
	hub.Register("dev-1", "junior", "http://127.0.0.1:1")
	ts := newTestServer(t, hub.Handler(nil))
	b := &Bridge{Hub: NewHubClient(ts.URL), HubURL: ts.URL}
	call := func(name string, args map[string]any) (any, error) {
		for _, tl := range b.Tools() {
			if tl.Name() == name {
				return tl.Call(context.Background(), args)
			}
		}
		t.Fatalf("no tool %s", name)
		return nil, nil
	}
	if out, err := call("team_queue", nil); err != nil || !strings.Contains(fmt.Sprint(out), "dev-1") {
		t.Fatalf("queue: %v %v", out, err)
	}
	if _, err := call("team_ask_worker", map[string]any{"worker": "nope", "question": "hi"}); err == nil {
		t.Fatal("want unknown worker error")
	}
}

func TestBridgeToolSetAndProxy(t *testing.T) {
	b := &Bridge{Manager: remote.NewClient("http://127.0.0.1:1")}
	names := map[string]bool{}
	for _, tl := range append(b.Tools(), b.GraphTools()...) {
		names[tl.Name()] = true
	}
	for _, n := range []string{"team_submit", "graph_run", "graph_state", "graph_resume"} {
		if !names[n] {
			t.Errorf("missing tool %s", n)
		}
	}
	if ts, err := Proxy(ctx(t), ""); ts != nil || err != nil {
		t.Fatalf("empty proxy: %v %v", ts, err)
	}
	if _, err := Proxy(ctx(t), "/nonexistent-mcp-binary"); err == nil {
		t.Fatal("want error for missing binary")
	}
}
