package babysit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe/babysit"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

type check struct {
	ID         int
	Name       string
	Status     string
	Conclusion string
	Slug       string
}

type fakeCI struct {
	mu       sync.Mutex
	state    string // open | closed
	merged   bool
	head     string
	checks   []check
	required []string
	reruns   []int
	apiDown  bool
	denyRun  bool
}

func (f *fakeCI) set(fn func(*fakeCI)) { f.mu.Lock(); fn(f); f.mu.Unlock() }

func (f *fakeCI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.apiDown {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		p := r.URL.Path
		switch {
		case r.Method == "GET" && p == "/repos/o/r/pulls/4":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 4, "state": f.state, "merged": f.merged,
				"head": map[string]any{"sha": f.head, "ref": "feat"}, "base": map[string]any{"ref": "main"}})
		case r.Method == "GET" && strings.HasSuffix(p, "/check-runs"):
			var runs []map[string]any
			for _, c := range f.checks {
				runs = append(runs, map[string]any{"id": c.ID, "name": c.Name, "status": c.Status, "conclusion": c.Conclusion,
					"html_url": "https://github.com/o/r/actions/runs/900" + string(rune('0'+c.ID)) + "/job/1", "app": map[string]any{"slug": c.Slug}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": runs})
		case r.Method == "GET" && strings.HasSuffix(p, "/protection/required_status_checks"):
			if f.required == nil {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"contexts": f.required})
		case r.Method == "POST" && strings.Contains(p, "/rerun-failed-jobs"):
			if f.denyRun {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
				return
			}
			f.reruns = append(f.reruns, 1)
			w.WriteHeader(201)
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	})
}

type wakes struct {
	mu   sync.Mutex
	list []babysit.Wake
	err  error
}

func (w *wakes) Wake(_ context.Context, wk babysit.Wake) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	w.list = append(w.list, wk)
	return nil
}

func (w *wakes) kinds() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var k []string
	for _, x := range w.list {
		k = append(k, string(x.Kind))
	}
	return strings.Join(k, ",")
}

var key = github.PRKey{Owner: "o", Repo: "r", Number: 4}

func setup(t *testing.T) (*babysit.Watcher, *fakeCI, *wakes) {
	f := &fakeCI{state: "open", head: "aaaa1111", checks: []check{{1, "lint", "completed", "success", "github-actions"}, {2, "test", "in_progress", "", "github-actions"}}}
	ts := httptest.NewServer(f.handler())
	t.Cleanup(ts.Close)
	c := github.NewClient(github.StaticToken("t"))
	c.BaseURL = ts.URL
	wk := &wakes{}
	return &babysit.Watcher{Store: store.NewMemory(), GitHub: c, Dispatcher: wk}, f, wk
}

func TestStartDecisions(t *testing.T) {
	ctx := context.Background()
	w, f, _ := setup(t)
	res, err := w.Start(ctx, key, "thread-1")
	if err != nil || !res.Active || res.Watch.HeadSHA != "aaaa1111" || res.Watch.ThreadID != "thread-1" || res.Summary.State != "pending" {
		t.Fatalf("%+v %v", res, err)
	}
	// everything already green: no watch is needed
	f.set(func(f *fakeCI) { f.checks[1] = check{2, "test", "completed", "success", "github-actions"} })
	w2, f2, _ := setup(t)
	f2.set(func(f *fakeCI) { f.checks = f.checks[:1] })
	if res, _ := w2.Start(ctx, key, "t"); res.Active || !strings.Contains(res.Reason, "already passed") {
		t.Fatalf("%+v", res)
	}
	// closed PR
	w3, f3, _ := setup(t)
	f3.set(func(f *fakeCI) { f.state = "closed" })
	if res, _ := w3.Start(ctx, key, "t"); res.Active || !strings.Contains(res.Reason, "closed") {
		t.Fatalf("%+v", res)
	}
	if act, _ := w3.Active(ctx); len(act) != 0 {
		t.Fatal("no watch must be stored")
	}
}

func TestFailureWakesOncePerStateAndNeverWhilePending(t *testing.T) {
	ctx := context.Background()
	w, f, wk := setup(t)
	if _, err := w.Start(ctx, key, "thread-1"); err != nil {
		t.Fatal(err)
	}
	// pending: idle, zero wakes (this is the no-token path)
	for range 3 {
		o, err := w.Evaluate(ctx, key)
		if err != nil || !o.Idle || o.Woken != nil {
			t.Fatalf("%+v %v", o, err)
		}
	}
	if wk.kinds() != "" {
		t.Fatal("pending must not wake the agent")
	}
	// test fails -> one wake with the diagnosis context
	f.set(func(f *fakeCI) { f.checks[1] = check{2, "test", "completed", "failure", "github-actions"} })
	o, err := w.Evaluate(ctx, key)
	if err != nil || o.Woken == nil || o.Woken.Kind != babysit.WakeFailure || !strings.Contains(o.Woken.Summary, "test (failure, GitHub Actions)") {
		t.Fatalf("%+v %v", o, err)
	}
	if m := o.Woken.Message(); !strings.HasPrefix(m, "/baby-sit --continue https://github.com/o/r/pull/4") || !strings.Contains(m, "aaaa1111") {
		t.Fatalf("%s", m)
	}
	// same failure again: deduplicated
	for range 3 {
		if o, _ := w.Evaluate(ctx, key); !o.Idle || o.Woken != nil {
			t.Fatalf("%+v", o)
		}
	}
	if wk.kinds() != "failure" {
		t.Fatalf("%s", wk.kinds())
	}
	// a different failing set on the same head wakes again
	f.set(func(f *fakeCI) { f.checks[0] = check{1, "lint", "completed", "failure", "github-actions"} })
	if o, _ := w.Evaluate(ctx, key); o.Woken == nil {
		t.Fatal("new failing set must wake")
	}
	if wk.kinds() != "failure,failure" {
		t.Fatalf("%s", wk.kinds())
	}
}

func TestGreenEndsTheWatchOnce(t *testing.T) {
	ctx := context.Background()
	w, f, wk := setup(t)
	_, _ = w.Start(ctx, key, "th")
	f.set(func(f *fakeCI) { f.checks[1] = check{2, "test", "completed", "success", "github-actions"} })
	o, err := w.Evaluate(ctx, key)
	if err != nil || o.Woken == nil || o.Woken.Kind != babysit.WakeGreen {
		t.Fatalf("%+v %v", o, err)
	}
	if wt, _ := w.Get(ctx, key); wt.Active || wt.StoppedFor == "" {
		t.Fatalf("%+v", wt)
	}
	if o, _ := w.Evaluate(ctx, key); !o.Idle {
		t.Fatal("a finished watch is idle")
	}
	if wk.kinds() != "green" {
		t.Fatal(wk.kinds())
	}
}

func TestMergedClosedAndHeadChange(t *testing.T) {
	ctx := context.Background()
	w, f, wk := setup(t)
	_, _ = w.Start(ctx, key, "th")
	// a force-push / new commit resets retries and dedupe keys
	f.set(func(f *fakeCI) { f.checks[1] = check{2, "test", "completed", "failure", "github-actions"} })
	_, _ = w.Evaluate(ctx, key)
	if _, err := w.RerunFlaky(ctx, key, "aaaa1111", "test", "timeout in CI runner"); err != nil {
		t.Fatal(err)
	}
	f.set(func(f *fakeCI) { f.head = "bbbb2222" })
	if o, _ := w.Evaluate(ctx, key); o.Woken == nil {
		t.Fatal("the same failure on a NEW head is new evidence and must wake")
	}
	wt, _ := w.Get(ctx, key)
	if wt.HeadSHA != "bbbb2222" || wt.Retries != 0 {
		t.Fatalf("%+v", wt)
	}
	// merged
	f.set(func(f *fakeCI) { f.state, f.merged = "closed", true })
	o, _ := w.Evaluate(ctx, key)
	if o.Woken == nil || o.Woken.Kind != babysit.WakeStopped || !strings.Contains(o.Woken.Watch.StoppedFor, "merged") {
		t.Fatalf("%+v", o)
	}
	_ = wk
}

func TestUndeliveredWakeIsRetried(t *testing.T) {
	ctx := context.Background()
	w, f, wk := setup(t)
	_, _ = w.Start(ctx, key, "th")
	f.set(func(f *fakeCI) { f.checks[1] = check{2, "test", "completed", "failure", "github-actions"} })
	wk.err = errors.New("thread unreachable")
	if _, err := w.Evaluate(ctx, key); err == nil {
		t.Fatal("delivery failure must surface")
	}
	wk.err = nil
	if o, _ := w.Evaluate(ctx, key); o.Woken == nil {
		t.Fatal("a failure that could not be delivered must be delivered next time, not lost")
	}
}

func TestRerunFlakyGuards(t *testing.T) {
	ctx := context.Background()
	w, f, _ := setup(t)
	_, _ = w.Start(ctx, key, "th")
	f.set(func(f *fakeCI) {
		f.checks[1] = check{2, "test", "completed", "failure", "github-actions"}
		f.checks = append(f.checks, check{3, "sonar", "completed", "failure", "sonarcloud"})
	})
	_, _ = w.Evaluate(ctx, key)
	cases := []struct{ head, name, want string }{
		{"stale000", "test", "not the watched head"},
		{"aaaa1111", "ghost", "no check named"},
		{"aaaa1111", "lint", "not failing"},
		{"aaaa1111", "sonar", "not a GitHub Actions check"},
	}
	for _, c := range cases {
		if _, err := w.RerunFlaky(ctx, key, c.head, c.name, "ev"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v -> %v", c, err)
		}
	}
	if len(f.reruns) != 0 {
		t.Fatal("no guard-failing request may reach GitHub")
	}
	for i := 1; i <= babysit.MaxRetriesPerHead; i++ {
		wt, err := w.RerunFlaky(ctx, key, "aaaa1111", "test", "network timeout pulling image")
		if err != nil || wt.Retries != i {
			t.Fatalf("%d: %+v %v", i, wt, err)
		}
	}
	if _, err := w.RerunFlaky(ctx, key, "aaaa1111", "test", "again"); !errors.Is(err, babysit.ErrRetryLimit) || len(f.reruns) != 3 {
		t.Fatalf("the 4th re-run must be refused: %v (%d reruns)", err, len(f.reruns))
	}
	// RecordRetry past the budget ends the watch so a human triages
	if _, err := w.RecordRetry(ctx, key, "aaaa1111", "test", "ev", "u"); !errors.Is(err, babysit.ErrRetryLimit) {
		t.Fatalf("%v", err)
	}
	if wt, _ := w.Get(ctx, key); wt.Active || !strings.Contains(wt.StoppedFor, "limit") {
		t.Fatalf("%+v", wt)
	}
	// permission denied is reported with the instruction not to work around it
	w2, f2, _ := setup(t)
	_, _ = w2.Start(ctx, key, "th")
	f2.set(func(f *fakeCI) {
		f.checks[1] = check{2, "test", "completed", "failure", "github-actions"}
		f.denyRun = true
	})
	if _, err := w2.RerunFlaky(ctx, key, "aaaa1111", "test", "ev"); err == nil || !strings.Contains(err.Error(), "Actions write permission") {
		t.Fatalf("%v", err)
	}
	if wt, _ := w2.Get(ctx, key); wt.Retries != 0 {
		t.Fatal("a denied re-run must not consume budget")
	}
}

func TestWebhookEventsAndTick(t *testing.T) {
	ctx := context.Background()
	w, f, wk := setup(t)
	_, _ = w.Start(ctx, key, "th")
	f.set(func(f *fakeCI) { f.checks[1] = check{2, "test", "completed", "failure", "github-actions"} })
	ev := github.Event{Type: "check_run", Action: "completed", Owner: "o", Repo: "r", HeadRef: "feat", HeadSHA: "aaaa1111", Delivery: "d1"}
	outs, err := w.HandleCheckEvent(ctx, ev)
	if err != nil || len(outs) != 1 || outs[0].Woken == nil {
		t.Fatalf("%+v %v", outs, err)
	}
	// redelivery of the same webhook is ignored
	if outs, _ := w.HandleCheckEvent(ctx, ev); len(outs) != 0 {
		t.Fatal("duplicate delivery processed twice")
	}
	// unrelated repo / in-progress / other event types are ignored
	for _, e := range []github.Event{
		{Type: "check_run", Action: "completed", Owner: "o", Repo: "other", HeadRef: "feat", Delivery: "d2"},
		{Type: "check_run", Action: "created", Owner: "o", Repo: "r", HeadRef: "feat", Delivery: "d3"},
		{Type: "push", Action: "", Owner: "o", Repo: "r", Delivery: "d4"},
		{Type: "check_run", Action: "completed", Owner: "o", Repo: "r", HeadRef: "elsewhere", HeadSHA: "zzzz", Delivery: "d5"},
	} {
		if outs, err := w.HandleCheckEvent(ctx, e); err != nil || len(outs) != 0 {
			t.Errorf("%+v -> %v %v", e, outs, err)
		}
	}
	// by PR number
	if outs, _ := w.HandleCheckEvent(ctx, github.Event{Type: "check_suite", Action: "completed", Owner: "o", Repo: "r", PRNumbers: []int{4}, Delivery: "d6"}); len(outs) != 1 || outs[0].Woken != nil {
		t.Fatalf("matched by PR number but state is unchanged => idle: %+v", outs)
	}
	// the fallback tick evaluates everything and is idle on unchanged state
	outs, err = w.Tick(ctx)
	if err != nil || len(outs) != 1 || !outs[0].Idle {
		t.Fatalf("%+v %v", outs, err)
	}
	if wk.kinds() != "failure" {
		t.Fatalf("%s", wk.kinds())
	}
}

func TestEvaluationErrorsEndTheWatch(t *testing.T) {
	ctx := context.Background()
	w, f, wk := setup(t)
	_, _ = w.Start(ctx, key, "th")
	f.set(func(f *fakeCI) { f.apiDown = true })
	for i := 1; i < babysit.MaxEvaluationErrors; i++ {
		if o, err := w.Evaluate(ctx, key); err != nil || !o.Idle {
			t.Fatalf("%d: transient errors are tolerated: %+v %v", i, o, err)
		}
	}
	o, err := w.Evaluate(ctx, key)
	if err != nil || o.Woken == nil || o.Woken.Kind != babysit.WakeStopped || !strings.Contains(o.Woken.Watch.StoppedFor, "access failed") {
		t.Fatalf("%+v %v", o, err)
	}
	_ = wk
}

func TestToolsAndSkill(t *testing.T) {
	ctx := runnables.WithConfig(context.Background(), graph.Thread("thread-9"))
	w, f, _ := setup(t)
	byName := map[string]func(map[string]any) (any, error){}
	for _, tl := range w.Tools() {
		tl := tl
		byName[tl.Name()] = func(a map[string]any) (any, error) { return tl.Call(ctx, a) }
	}
	pr := "https://github.com/o/r/pull/4"
	out, err := byName["manage_baby_sit"](map[string]any{"action": "start", "pr_url": pr})
	if err != nil || !strings.Contains(out.(string), "watching") {
		t.Fatalf("%v %v", out, err)
	}
	if wt, _ := w.Get(context.Background(), key); wt.ThreadID != "thread-9" {
		t.Fatalf("watch must wake the calling thread: %+v", wt)
	}
	if out, _ := byName["manage_baby_sit"](map[string]any{"action": "status", "pr_url": pr}); !strings.Contains(out.(string), "active=true") {
		t.Fatalf("%v", out)
	}
	if _, err := byName["rerun_flaky_check"](map[string]any{"pr_url": pr, "head_sha": "aaaa1111", "check_name": "test", "evidence": ""}); err == nil {
		t.Fatal("evidence is mandatory")
	}
	f.set(func(f *fakeCI) { f.checks[1] = check{2, "test", "completed", "failure", "github-actions"} })
	if out, err := byName["rerun_flaky_check"](map[string]any{"pr_url": pr, "head_sha": "aaaa1111", "check_name": "test", "evidence": "runner OOM"}); err != nil || !strings.Contains(out.(string), "1/3") {
		t.Fatalf("%v %v", out, err)
	}
	if out, _ := byName["manage_baby_sit"](map[string]any{"action": "stop", "pr_url": pr}); !strings.Contains(out.(string), "stopped") {
		t.Fatalf("%v", out)
	}
	if _, err := byName["manage_baby_sit"](map[string]any{"action": "start", "pr_url": "not a url"}); err == nil {
		t.Fatal("bad url")
	}
	// starting outside a thread is refused
	for _, tl := range w.Tools() {
		if tl.Name() == "manage_baby_sit" {
			if _, err := tl.Call(context.Background(), map[string]any{"action": "start", "pr_url": pr}); err == nil {
				t.Fatal("no thread, no watch")
			}
		}
	}
	for _, must := range []string{"name: baby-sit", "--continue", "rerun_flaky_check", "three times per head", "untrusted"} {
		if !strings.Contains(babysit.Skill, must) {
			t.Errorf("skill lacks %q", must)
		}
	}
}
