package team

import (
	"context"
	"encoding/json"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/internal/tasktrack"
)

func TestBuildDash(t *testing.T) {
	t0 := time.Unix(1000, 0)
	ev := func(s float64, subj string, to tasktrack.Status, agent, detail string) tasktrack.Event {
		return tasktrack.Event{Time: t0.Add(time.Duration(s * float64(time.Second))), Subject: subj, To: to, Agent: agent, Detail: detail}
	}
	events := []tasktrack.Event{
		ev(0, "r1", tasktrack.Received, "manager", "add coupon"),
		ev(1, "r1", tasktrack.TaskAssignment, "tech-leader", ""),
		ev(2, "r1/T1", tasktrack.TaskAssignment, "tech-leader", "field (low)"),
		ev(3, "r1/T1", tasktrack.InDevelopment, "junior-1", ""),
		ev(10, "r1/T1", tasktrack.Completed, "junior-1", ""),
	}
	hub := State{Workers: []WorkerInfo{{ID: "junior-1", Role: "junior", Job: "r1.2"}}, Queued: []string{"x"}}
	jobs := []Job{
		{ID: "r1.1", Worker: "junior-1", Status: "done", StartedMS: t0.UnixMilli(), DoneMS: t0.Add(4 * time.Second).UnixMilli()},
		{ID: "r1.2", Worker: "junior-1", Status: "running", StartedMS: t0.Add(5 * time.Second).UnixMilli()},
	}
	d := BuildDash(t0.Add(8*time.Second), events, hub, jobs, nil)

	if len(d.Requests) != 1 || d.Requests[0].Title != "add coupon" || len(d.Requests[0].Tasks) != 1 {
		t.Fatalf("requests: %+v", d.Requests)
	}
	task := d.Requests[0].Tasks[0]
	if task.Title != "field (low)" || task.Rounds != 1 || task.Progress != 100 || task.EndedMS == 0 {
		t.Fatalf("task: %+v", task)
	}
	if d.Requests[0].Progress != 95 || d.Queued != 1 {
		t.Fatalf("request progress %d queued %d", d.Requests[0].Progress, d.Queued)
	}
	var p DashPersona
	for _, x := range d.Personas {
		if x.ID == "junior-1" {
			p = x
		}
	}
	if p.State != "busy" || p.Done != 1 || p.JobMS != 3000 || p.BusyMS != 7000 {
		t.Fatalf("persona: %+v", p)
	}
	if len(d.Personas) != len(Cast) || d.Events[0].To != tasktrack.Completed {
		t.Fatalf("personas %d, events %+v", len(d.Personas), d.Events[0])
	}
}

func TestDashboardHandler(t *testing.T) {
	h := NewHub(time.Minute)
	h.Register("junior-1", "junior", "http://x")
	hs := httptest.NewServer(h.Handler(nil))
	defer hs.Close()
	track := tasktrack.New()
	_ = track.Move(context.Background(), tasktrack.Request, "r1", tasktrack.Received, "manager", "hello")
	ds := httptest.NewServer(DashboardHandler(track, NewHubClient(hs.URL), "/work/checkio-front"))
	defer ds.Close()

	resp, err := http.Get(ds.URL + "/")
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("page: %v %v", resp, err)
	}
	resp.Body.Close()
	resp, err = http.Get(ds.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var d DashState
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatal(err)
	}
	if d.HubError != "" || d.Project != "checkio-front" || d.Root != "/work/checkio-front" || len(d.Requests) != 1 || d.Requests[0].Title != "hello" || len(d.Personas) != len(Cast) {
		t.Fatalf("state: %+v", d)
	}
}

func TestBuildDashNoTasksIsEmptyArray(t *testing.T) {
	ev := []tasktrack.Event{{Time: time.Unix(1, 0), Subject: "r1", To: tasktrack.Received}}
	b, _ := json.Marshal(BuildDash(time.Unix(2, 0), ev, State{}, nil, nil))
	if !strings.Contains(string(b), `"tasks":[]`) {
		t.Fatalf("tasks must be [] not null: %s", b)
	}
}

func TestBuildDashTokens(t *testing.T) {
	t0 := time.Unix(1000, 0)
	ev := []tasktrack.Event{
		{Time: t0, Subject: "abcdefgh", To: tasktrack.Received, Detail: "x"},
		{Time: t0, Subject: "abcdefgh/T1", To: tasktrack.TaskAssignment, Detail: "t1"},
		{Time: t0, Subject: "abcdefgh/T10", To: tasktrack.TaskAssignment, Detail: "t10"},
	}
	full := "abcdefghZZZ"
	u := func(n int) *messages.Usage {
		return &messages.Usage{InputTokens: n, OutputTokens: 1, TotalTokens: n + 1}
	}
	jobs := []Job{
		{ID: "a.1", Worker: "po", Scope: full, Thread: full + "-po", Status: "done", StartedMS: 1, DoneMS: 2, Usage: u(100)},
		{ID: "a.2", Worker: "junior-1", Scope: full, Thread: full + "-T1-dev", Status: "done", StartedMS: 1, DoneMS: 2, Usage: u(10)},
		{ID: "a.3", Worker: "junior-1", Scope: full, Thread: full + "-T10-dev", Status: "done", StartedMS: 1, DoneMS: 2, Usage: u(20)},
	}
	d := BuildDash(t0, ev, State{}, jobs, map[string]messages.Usage{full: {InputTokens: 5, TotalTokens: 5}})
	r := d.Requests[0]
	if r.Tasks[0].Tokens.Total != 11 || r.Tasks[1].Tokens.Total != 21 { // T1 must not swallow T10
		t.Fatalf("tasks: %+v", r.Tasks)
	}
	if r.Tokens.Total != 138 || d.Tokens.Total != 138 { // 101+11+21 jobs + 5 manager
		t.Fatalf("request %+v project %+v", r.Tokens, d.Tokens)
	}
	var po, mgr DashPersona
	for _, p := range d.Personas {
		switch p.ID {
		case "po":
			po = p
		case "manager":
			mgr = p
		}
	}
	if po.Tokens.Total != 101 || mgr.Tokens.Total != 5 {
		t.Fatalf("po %+v manager %+v", po, mgr)
	}
}

// Two teams on one address: the second registers with the first, and the
// owner serves it under /p/<name>/ and lists both.
func TestGatewayProxiesRegisteredProject(t *testing.T) {
	hub := NewHub(time.Minute)
	hs := httptest.NewServer(hub.Handler(nil))
	defer hs.Close()
	mk := func(root string) project {
		return project{tasktrack.New(), NewHubClient(hs.URL), root}
	}
	other := mk("/w/other")
	_ = other.track.Move(context.Background(), tasktrack.Request, "r9", tasktrack.Received, "m", "other demand")
	os := httptest.NewServer(other.handler())
	defer os.Close()
	gw := httptest.NewServer(newGateway(mk("/w/main")).handler())
	defer gw.Close()

	name, err := registerWith(context.Background(), strings.TrimPrefix(gw.URL, "http://"), "other", "/w/other", os.URL)
	if err != nil || name != "other" {
		t.Fatalf("register: %q %v", name, err)
	}
	get := func(path string, v any) {
		resp, err := http.Get(gw.URL + path)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", path, resp, err)
		}
		defer resp.Body.Close()
		if v != nil {
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				t.Fatal(err)
			}
		}
	}
	var d DashState
	get("/p/other/api/state", &d)
	if d.Project != "other" || len(d.Requests) != 1 {
		t.Fatalf("proxied state: %+v", d)
	}
	get("/p/main/api/state", &d)
	if d.Project != "main" {
		t.Fatalf("own state: %+v", d)
	}
	get("/p/other/", nil)
	var ps []ProjectInfo
	get("/api/projects", &ps)
	if len(ps) != 2 || ps[0].Name != "main" || ps[1].Name != "other" || !ps[1].Alive || ps[1].Requests != 1 {
		t.Fatalf("projects: %+v", ps)
	}
	// Same name, different root: disambiguated; a non-loopback url is refused.
	if n, _ := registerWith(context.Background(), strings.TrimPrefix(gw.URL, "http://"), "other", "/elsewhere/other", os.URL); n == "other" {
		t.Fatalf("name clash not resolved: %q", n)
	}
	if _, err := registerWith(context.Background(), strings.TrimPrefix(gw.URL, "http://"), "evil", "/x", "http://example.com"); err == nil {
		t.Fatal("non-loopback url accepted")
	}
	resp, _ := http.Get(gw.URL + "/p/nope/")
	if resp.StatusCode != 404 {
		t.Fatalf("unknown project: %d", resp.StatusCode)
	}
}

func TestCancelScope(t *testing.T) {
	h := NewHub(time.Minute)
	h.Register("junior-1", "junior", "u")
	run := h.Submit(Job{Prefer: []string{"junior"}, Thread: "r1-T1-dev", Scope: "r1"})
	queued := h.Submit(Job{Prefer: []string{"junior"}, Thread: "r1-T2-dev", Scope: "r1"})
	other := h.Submit(Job{Prefer: []string{"senior"}, Thread: "r2-x", Scope: "r2"})

	if n := h.CancelScope("r1"); n != 2 {
		t.Fatalf("cancelled %d jobs, want 2", n)
	}
	st := map[string]Job{}
	for _, j := range h.Jobs() {
		st[j.ID] = j
	}
	if st[run].Status != "failed" || st[run].Error != CancelledMsg || st[queued].Status != "failed" || st[other].Status != "queued" {
		t.Fatalf("jobs: %+v", st)
	}
	if s := h.Snapshot(); s.Workers[0].Job != "" || len(s.Queued) != 1 {
		t.Fatalf("worker must be free and only the other request queued: %+v", s)
	}
	if got := h.TakeCancel("junior-1"); got != run || h.TakeCancel("junior-1") != "" {
		t.Fatalf("worker must learn of the cancelled job exactly once, got %q", got)
	}
	// A job submitted after the cancellation fails at once; a new run (SetScope) lifts it.
	late := h.Submit(Job{Prefer: []string{"junior"}, Thread: "r1-T3-dev", Scope: "r1"})
	if j, _ := h.Wait(context.Background(), late); j.Status != "failed" || j.Error != CancelledMsg {
		t.Fatalf("late job: %+v", j)
	}
	h.SetScope("r1", Scope{})
	h.Submit(Job{Prefer: []string{"junior"}, Thread: "r1-T4-dev", Scope: "r1"})
	for _, j := range h.Jobs() {
		if j.Thread == "r1-T4-dev" && j.Status == "failed" {
			t.Fatal("a new run of the thread must not be cancelled")
		}
	}
}
