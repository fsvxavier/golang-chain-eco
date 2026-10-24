package team

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
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
		"rag_answer": "[1]", "cag_answer": "[1]", "kag_query": "aria-live", "knowledge_answer": "[1]",
	} {
		out, err := mc.CallTool(c, tool, map[string]any{"question": "how must the coupon field announce an expired coupon? aria-live region double submit acceptance criteria"})
		if err != nil || !strings.Contains(strings.ToLower(fmt.Sprint(out)), strings.ToLower(want)) {
			t.Fatalf("%s: %v / %v", tool, out, err)
		}
	}
	_, _ = mc.CallTool(c, "memory_put", map[string]any{"scope": "r1", "key": "k1", "text": "po: reservation must expire with a TTL"})
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

// countMW counts the model calls its workers make.
type countMW struct{ n *atomic.Int32 }

func (countMW) Name() string { return "count" }

func (c countMW) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	c.n.Add(1)
	return next(ctx, req)
}

// A middleware passed through LocalOptions reaches every worker's agent.
func TestStartLocalWithMiddleware(t *testing.T) {
	c := ctx(t)
	var n atomic.Int32
	l, err := StartLocalWith(c, LocalOptions{Middleware: []agents.Middleware{countMW{&n}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Invoke(c, l.ManagerURL, "req-mw", DefaultRequest); err != nil {
		t.Fatal(err)
	}
	if n.Load() == 0 {
		t.Fatal("worker middleware never ran")
	}
}

func TestCastHasTwelveWorkers(t *testing.T) {
	n := map[string]int{}
	for _, c := range Cast {
		n[c.Role]++
	}
	want := map[string]int{"po": 1, "leader": 1, "junior": 2, "mid": 2, "senior": 2, "specialist": 2, "qa": 2}
	if len(Cast) != 12 || fmt.Sprint(n) != fmt.Sprint(want) {
		t.Fatalf("cast %v (%d)", n, len(Cast))
	}
}

func TestDefaultModels(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "x")
	t.Setenv("AGENT_MODEL", "")
	for role, want := range map[string]string{"po": "opus-5-5", "senior": "haiku-5-5", "qa": "sonnet-5-5", "leader": "sonnet-5-5"} {
		if got := ModelSpec(role); got != "anthropic:claude-"+want {
			t.Errorf("%s: %s", role, got)
		}
	}
}

func TestPinnedJobWaitsForItsWorker(t *testing.T) {
	h := NewHub(time.Minute)
	h.Register("senior-1", "senior", "")
	h.Register("senior-2", "senior", "")
	busy := h.Submit(Job{Prefer: []string{"senior-1"}, Thread: "busy"})
	if h.jobs[busy].Worker != "senior-1" {
		t.Fatalf("setup: busy job on %q", h.jobs[busy].Worker)
	}
	id := h.Submit(Job{Prefer: []string{"senior-1", "senior"}, Pin: true, Thread: "rework"})
	if h.jobs[id].Status != "queued" || h.workers["senior-2"].Job != "" {
		t.Fatalf("pinned job must wait for senior-1, is %s on %q", h.jobs[id].Status, h.jobs[id].Worker)
	}
	if err := h.Complete("senior-1", busy, "ok", ""); err != nil {
		t.Fatal(err)
	}
	if h.jobs[id].Worker != "senior-1" {
		t.Fatalf("pinned job must run on senior-1, is on %q", h.jobs[id].Worker)
	}
}

func TestPinnedJobFallsBackWhenLeaseExpired(t *testing.T) {
	h := NewHub(time.Second)
	h.MaxAttempts = 1 // the blocker fails instead of jumping the queue
	now := time.Now()
	h.now = func() time.Time { return now }
	h.Register("senior-1", "senior", "")
	h.Register("senior-2", "senior", "")
	h.Submit(Job{Prefer: []string{"senior-1"}, Thread: "busy"})
	id := h.Submit(Job{Prefer: []string{"senior-1", "senior"}, Pin: true, Thread: "rework"})
	now = now.Add(2 * time.Second)
	h.Heartbeat("senior-2")
	h.Reap()
	if h.jobs[id].Worker != "senior-2" {
		t.Fatalf("job must fall back to senior-2, is on %q (%s)", h.jobs[id].Worker, h.jobs[id].Status)
	}
}

// resultsOf indexes the manager's per-task results by id.
func resultsOf(t *testing.T, out map[string]any) map[string]map[string]any {
	t.Helper()
	rs, _ := out["results"].([]any)
	m := map[string]map[string]any{}
	for _, r := range rs {
		x := r.(map[string]any)
		m[fmt.Sprint(x["id"])] = x
	}
	if len(m) == 0 {
		t.Fatalf("no results in %v", out)
	}
	return m
}

func TestJointReviewNeedsBothApprovals(t *testing.T) {
	c := ctx(t)
	l, err := StartLocal(c)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Invoke(c, l.ManagerURL, "req-joint", DefaultRequest)
	if err != nil {
		t.Fatal(err)
	}
	if r := resultsOf(t, out)["T2"]; fmt.Sprint(r["rounds"]) != "1" || r["status"] != "approved" {
		t.Fatalf("T2 must be returned once by the tech lead alone: %v", r)
	}
	var found bool
	for _, h := range strings.Split(fmt.Sprint(out["history"]), "T2:") {
		if strings.Contains(h, "review returned") && strings.Contains(h, "tech-leader:") && !strings.Contains(h, "specialist:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("history must hold the tech-leader rejection: %v", out["history"])
	}
}

// Rework and QA returns go back to whoever wrote the code.
func TestRejectedWorkReturnsToSameDeveloper(t *testing.T) {
	c := ctx(t)
	l, err := StartLocal(c)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Invoke(c, l.ManagerURL, "req-same", DefaultRequest)
	if err != nil {
		t.Fatal(err)
	}
	res := resultsOf(t, out)
	hist := fmt.Sprint(out["history"])
	for _, id := range []string{"T2", "T3", "T4"} { // joint review (leader, specialist) and QA returns
		if n, err := strconv.Atoi(fmt.Sprint(res[id]["rounds"])); err != nil || n < 1 {
			t.Fatalf("%s was never returned (rounds %v)", id, res[id]["rounds"])
		}
		for _, w := range []string{"junior-1", "junior-2", "mid-1", "mid-2", "senior-1", "senior-2"} {
			if w != res[id]["by"] && strings.Contains(hist, id+": "+w+" -> review") {
				t.Fatalf("%s was reworked by %s but ended by %v: %s", id, w, res[id]["by"], hist)
			}
		}
	}
}

func TestPlanParsesPreferField(t *testing.T) {
	ts := parsePlan("TASK|T1|a|low|do a|PREFER:mid-2\nTASK|T2|b|high|do b with | inside?\nnoise")
	if len(ts) != 2 || ts[0].Prefer != "mid-2" || ts[0].Desc != "do a" || ts[1].Prefer != "" || ts[1].Desc != "do b with | inside?" {
		t.Fatalf("%+v", ts)
	}
}

// Two running jobs on the same thread that carry different request scopes
// make the thread ambiguous: ThreadScope returns nothing instead of picking one.
func TestThreadScopeFailsClosedOnAmbiguity(t *testing.T) {
	h := NewHub(time.Minute)
	h.Register("senior-1", "senior", "http://127.0.0.1:1")
	h.Register("senior-2", "senior", "http://127.0.0.1:1")
	h.SetScope("req-a", Scope{Roots: []string{"/a"}})
	h.SetScope("req-b", Scope{Roots: []string{"/b"}})
	h.Submit(Job{Prefer: []string{"senior"}, Thread: "th", Scope: "req-a"})
	if _, id, ok := h.ThreadScope("th"); !ok || id != "req-a" {
		t.Fatalf("single scope: id=%q ok=%v", id, ok)
	}
	h.Submit(Job{Prefer: []string{"senior"}, Thread: "th", Scope: "req-b"})
	if sc, id, ok := h.ThreadScope("th"); ok || id != "" || len(sc.Roots) != 0 {
		t.Fatalf("ambiguous thread returned %v %q %v", sc, id, ok)
	}
}

// PREFER names one registered worker whose role suits the task's complexity;
// anything else is dropped, so developer work never goes to a reviewer and a
// high task never to a junior.
func TestPlanValidatesPrefer(t *testing.T) {
	for _, bad := range []string{"po", "leader", "qa-1", "specialist-2", "ghost-9", "senior", "junior-1"} {
		ts := parsePlan("TASK|T1|a|high|do a|PREFER:" + bad)
		if len(ts) != 1 || ts[0].Prefer != "" || ts[0].Desc != "do a" {
			t.Errorf("PREFER:%s kept: %+v", bad, ts)
		}
	}
	if ts := parsePlan("TASK|T1|a|high|do a|PREFER:senior-2"); ts[0].Prefer != "senior-2" {
		t.Errorf("valid PREFER dropped: %+v", ts)
	}
	if ts := parsePlan("TASK|T1|a|low|do a|PREFER:junior-1"); ts[0].Prefer != "junior-1" {
		t.Errorf("junior on a low task dropped: %+v", ts)
	}
}

// An unknown complexity becomes medium: a job with an empty Prefer would wait forever.
func TestPlanNormalisesComplexity(t *testing.T) {
	for _, c := range []string{"Medium", "med", "", " HIGH "} {
		ts := parsePlan("TASK|T1|a|" + c + "|do a")
		want := "medium"
		if strings.EqualFold(strings.TrimSpace(c), "high") {
			want = "high"
		}
		if len(ts) != 1 || ts[0].Complexity != want {
			t.Errorf("complexity %q -> %+v, want %s", c, ts, want)
		}
		if len(preference[ts[0].Complexity]) == 0 {
			t.Errorf("complexity %q has no preference list", c)
		}
	}
}

func TestTaskBlockedAfterMaxRounds(t *testing.T) {
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{Offline: func(ms []messages.Message) messages.Message {
		m, _ := messages.LastOfRole(ms, messages.RoleHuman)
		p := m.Text()
		if r := field(p, "ROLE:"); r == "specialist" || r == "qa" || (r == "leader" && field(p, "MODE:") == "review") {
			return messages.AI("VERDICT: RETURN\nFEEDBACK: no")
		}
		return messages.AI(Brain(p))
	}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Invoke(c, l.ManagerURL, "req-block", DefaultRequest)
	if err != nil {
		t.Fatal(err)
	}
	for id, r := range resultsOf(t, out) {
		if r["status"] != "blocked" || fmt.Sprint(r["rounds"]) != "3" {
			t.Fatalf("%s: %v", id, r)
		}
	}
}

func TestScopeRoundTripOverHTTP(t *testing.T) {
	h := NewHub(time.Minute)
	ts := newTestServer(t, h.Handler(nil))
	c := NewHubClient(ts.URL)
	if _, err := c.Scope(ctx(t), "r-1"); !errors.Is(err, ErrNoScope) {
		t.Fatalf("unset scope: %v, want ErrNoScope", err)
	}
	want := Scope{Roots: []string{"/srv/api"}, URLs: []string{"http://api:8080"}, AppURL: "http://app:3000"}
	if err := c.SetScope(ctx(t), "r-1", want); err != nil {
		t.Fatal(err)
	}
	got, err := c.Scope(ctx(t), "r-1")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	if s, ok := h.Scope("r-1"); !ok || !reflect.DeepEqual(s, want) {
		t.Fatalf("hub scope %+v %v", s, ok)
	}
}

func TestSubmitStoresScope(t *testing.T) {
	c := ctx(t)
	l, err := StartLocal(c)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHubClient(l.HubURL)
	b := &Bridge{Manager: remote.NewClient(l.ManagerURL), Hub: hub, HubURL: l.HubURL}
	var submit tools.Tool
	for _, tl := range b.Tools() {
		if tl.Name() == "team_submit" {
			submit = tl
		}
	}
	// lists arrive as []any after schema validation
	out, err := submit.Call(c, map[string]any{"request": DefaultRequest,
		"backend_roots": []any{"/srv/api"}, "backend_urls": []any{"http://api:8080"}, "app_url": "http://app:3000"})
	if err != nil {
		t.Fatal(err)
	}
	tid := strings.TrimPrefix(strings.Fields(fmt.Sprint(out))[0], "thread_id=")
	want := Scope{Roots: []string{"/srv/api"}, URLs: []string{"http://api:8080"}, AppURL: "http://app:3000"}
	var got Scope
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if got, err = hub.Scope(c, tid); err == nil {
			break
		}
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("scope of %s: %+v, %v; want %+v", tid, got, err, want)
	}
}

// A tool running inside a worker's agent (reached over Agent Protocol) finds
// the scope of its job from the thread id alone; the model supplies nothing.
func TestToolSeesJobScope(t *testing.T) {
	c := ctx(t)
	var hubURL atomic.Value
	var seen, thread atomic.Value
	probe := tools.New("probe", "returns the scope of the running job", func(tc context.Context, _ struct{}) (any, error) {
		thread.Store(runnables.GetConfig(tc).Configurable["thread_id"])
		s, err := ScopeFromContext(tc, NewHubClient(hubURL.Load().(string)))
		seen.Store(s)
		return fmt.Sprintf("%+v %v", s, err), err
	})
	l, err := StartLocalWith(c, LocalOptions{Tools: []tools.Tool{probe}, Offline: OfflineWithTool("probe")})
	if err != nil {
		t.Fatal(err)
	}
	hubURL.Store(l.HubURL)
	hub := NewHubClient(l.HubURL)
	want := Scope{Roots: []string{"/srv/api"}, URLs: []string{"http://api:8080"}, AppURL: "http://app:3000"}
	if err := hub.SetScope(c, "req-abc-9", want); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Run(c, Job{Thread: "req-abc-9-T1-dev", Prefer: []string{"junior-1"}, Scope: "req-abc-9", Prompt: "ROLE: engineer\nTASK: T1\n"}); err != nil {
		t.Fatal(err)
	}
	t.Logf("thread_id seen by the tool: %v", thread.Load())
	if got, _ := seen.Load().(Scope); !reflect.DeepEqual(got, want) {
		t.Fatalf("tool saw %+v, want %+v", got, want)
	}
}

// scopeCtx is a context as a worker's tool sees it: the run's thread id.
func scopeCtx(t *testing.T, thread string) context.Context {
	return runnables.WithConfig(ctx(t), runnables.Config{Configurable: map[string]any{"thread_id": thread}})
}

// A thread no job owns gets no scope, even when its prefix is a real request.
func TestScopeNeverResolvedByPrefix(t *testing.T) {
	h := NewHub(time.Minute)
	ts := newTestServer(t, h.Handler(nil))
	c := NewHubClient(ts.URL)
	h.SetScope("R1", Scope{Roots: []string{"/victim"}})
	s, err := ScopeFromContext(scopeCtx(t, "R1-x"), c)
	if !errors.Is(err, ErrNoScope) || !s.IsEmpty() {
		t.Fatalf("client-chosen thread got %+v, %v", s, err)
	}
}

// Requests R1 and R1-T1 coexist: job thread R1-T1-dev belongs to R1.
func TestScopeIdCollision(t *testing.T) {
	h := NewHub(time.Minute)
	ts := newTestServer(t, h.Handler(nil))
	c := NewHubClient(ts.URL)
	h.SetScope("R1", Scope{Roots: []string{"/r1"}})
	h.SetScope("R1-T1", Scope{Roots: []string{"/r1-t1"}})
	h.Register("dev", "junior", "http://x")
	h.Submit(Job{Thread: "R1-T1-dev", Prefer: []string{"dev"}, Scope: "R1"})
	if j, err := c.Claim(ctx(t), "dev"); err != nil || j == nil {
		t.Fatalf("claim: %v %v", j, err)
	}
	s, err := ScopeFromContext(scopeCtx(t, "R1-T1-dev"), c)
	if err != nil || len(s.Roots) != 1 || s.Roots[0] != "/r1" {
		t.Fatalf("got %+v, %v; want R1's scope", s, err)
	}
}

// A demand that names a backend needs evidence: a PO answer without an
// EVIDENCE block (or without a backend call) gets flagged in OPEN ITEMS.
func TestPOSpecWithoutEvidenceIsMarkedUnverified(t *testing.T) {
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{
		Offline: func(ms []messages.Message) messages.Message {
			m, _ := messages.LastOfRole(ms, messages.RoleHuman)
			return messages.AI(Brain(m.Text()))
		},
		CallCounter: func(string) int { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := InvokeWithScope(c, l.ManagerURL, "req-unverified", DefaultRequest, Scope{Roots: []string{"/srv/api"}})
	if err != nil {
		t.Fatal(err)
	}
	spec := fmt.Sprint(out["spec"])
	if !strings.Contains(after(spec, "OPEN ITEMS:"), "UNVERIFIED: no backend evidence") {
		t.Fatalf("spec must be flagged: %s", spec)
	}
	// No backend named: nothing to verify, and the PO says so itself.
	out, err = Invoke(c, l.ManagerURL, "req-nobackend", DefaultRequest)
	if err != nil {
		t.Fatal(err)
	}
	if spec := fmt.Sprint(out["spec"]); strings.Contains(spec, "UNVERIFIED") {
		t.Fatalf("no scope, no flag: %s", spec)
	}
}

func TestVerifiedSpec(t *testing.T) {
	sc := Scope{URLs: []string{"http://api:8080"}}
	withEv := "SPEC:\nAC1: x\nEVIDENCE:\n- backend_get /x: 200\nOPEN ITEMS:\n- none"
	for _, tc := range []struct {
		name, spec string
		sc         Scope
		calls      func(string) int
		flagged    bool
	}{
		{"evidence and calls", withEv, sc, func(string) int { return 1 }, false},
		{"evidence, no calls", withEv, sc, func(string) int { return 0 }, true},
		{"evidence, no counter", withEv, sc, nil, false},
		{"no evidence", "SPEC:\nAC1: x", sc, func(string) int { return 3 }, true},
		{"app url only", "SPEC:\nAC1: x", Scope{AppURL: "http://app"}, nil, false},
	} {
		got := verifiedSpec(tc.spec, tc.sc, tc.calls, "r")
		if strings.Contains(got, "UNVERIFIED: no backend evidence") != tc.flagged {
			t.Errorf("%s: %q", tc.name, got)
		}
		if tc.flagged && !strings.Contains(after(got, "OPEN ITEMS:"), "UNVERIFIED") {
			t.Errorf("%s: flag outside OPEN ITEMS: %q", tc.name, got)
		}
	}
}
