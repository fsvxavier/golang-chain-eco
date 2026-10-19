// Package babysit watches pull requests until CI is green. A watch is durable
// and opt-in; it costs no model tokens while nothing changes: GitHub check
// webhooks and a deterministic fallback poll evaluate the PR, and the agent is
// woken only when the state needs a decision (a failure to diagnose, or all
// green). Flaky failures may be re-run, but only with evidence, only on
// GitHub Actions, only for failed jobs, and at most three times per head.
package babysit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

// Limits.
const (
	// MaxRetriesPerHead caps flaky re-runs for one head commit.
	MaxRetriesPerHead = 3
	// MaxEvaluationErrors consecutive API failures end a watch.
	MaxEvaluationErrors = 3
	maxKeys             = 30
	maxDeliveries       = 50
)

// ErrRetryLimit is returned when a head already used its flaky re-runs.
var ErrRetryLimit = errors.New("babysit: flaky re-run limit reached for this head")

// Watch is a durable CI watch on one pull request.
type Watch struct {
	Key      github.PRKey `json:"key"`
	ThreadID string       `json:"thread_id"`
	Active   bool         `json:"active"`
	HeadSHA  string       `json:"head_sha"`
	HeadRef  string       `json:"head_ref"`
	Base     string       `json:"base"`
	// Retries used for HeadSHA; reset when the head changes.
	Retries      int       `json:"retries"`
	DispatchKeys []string  `json:"dispatch_keys,omitempty"`
	Deliveries   []string  `json:"deliveries,omitempty"`
	AlertKeys    []string  `json:"alert_keys,omitempty"`
	EvalErrors   int       `json:"eval_errors"`
	StoppedFor   string    `json:"stopped_for,omitempty"`
	Retried      []Retry   `json:"retried,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Retry records one flaky re-run.
type Retry struct {
	HeadSHA  string    `json:"head_sha"`
	Check    string    `json:"check"`
	Evidence string    `json:"evidence"`
	URL      string    `json:"url"`
	At       time.Time `json:"at"`
}

// WakeKind says why the agent is woken.
type WakeKind string

// Wake kinds.
const (
	WakeFailure WakeKind = "failure"
	WakeGreen   WakeKind = "green"
	WakeStopped WakeKind = "stopped" // closed, merged, or the watch gave up
)

// Wake asks the owning thread to act.
type Wake struct {
	Watch   Watch
	Kind    WakeKind
	Summary string
}

// Message is the instruction delivered to the agent for a wake: failures
// re-enter the skill with --continue so no second watch is registered.
func (w Wake) Message() string {
	pr := w.Watch.Key.String()
	switch w.Kind {
	case WakeFailure:
		return fmt.Sprintf("/baby-sit --continue %s\n\nCI failed on head %s.\n%s", pr, short(w.Watch.HeadSHA), w.Summary)
	case WakeGreen:
		return fmt.Sprintf("Baby-sit update for %s: every required check passed on head %s. The watch has ended; report it to the requester.", pr, short(w.Watch.HeadSHA))
	}
	return fmt.Sprintf("Baby-sit update for %s: the watch ended (%s).", pr, w.Watch.StoppedFor)
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// Dispatcher delivers wakes to the agent that owns the watch.
type Dispatcher interface {
	Wake(ctx context.Context, w Wake) error
}

// DispatcherFunc adapts a function to Dispatcher.
type DispatcherFunc func(ctx context.Context, w Wake) error

// Wake implements Dispatcher.
func (f DispatcherFunc) Wake(ctx context.Context, w Wake) error { return f(ctx, w) }

// Watcher manages watches.
type Watcher struct {
	Store      store.Store
	GitHub     *github.Client
	Dispatcher Dispatcher
	// Now defaults to time.Now.
	Now func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func nsWatches() []string { return []string{"openswe", "babysit"} }

func keyOf(k github.PRKey) string {
	return fmt.Sprintf("%s/%s#%d", strings.ToLower(k.Owner), strings.ToLower(k.Repo), k.Number)
}

func (w *Watcher) lock(k github.PRKey) func() {
	w.mu.Lock()
	if w.locks == nil {
		w.locks = map[string]*sync.Mutex{}
	}
	m := w.locks[keyOf(k)]
	if m == nil {
		m = &sync.Mutex{}
		w.locks[keyOf(k)] = m
	}
	w.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// Get loads a watch (nil when none).
func (w *Watcher) Get(ctx context.Context, k github.PRKey) (*Watch, error) {
	it, err := w.Store.Get(ctx, nsWatches(), keyOf(k))
	if err != nil || it == nil {
		return nil, err
	}
	return decode(it.Value)
}

func (w *Watcher) save(ctx context.Context, wt *Watch) error {
	wt.UpdatedAt = w.now()
	m, err := encode(wt)
	if err != nil {
		return err
	}
	return w.Store.Put(ctx, nsWatches(), keyOf(wt.Key), m)
}

// Active lists active watches.
func (w *Watcher) Active(ctx context.Context) ([]*Watch, error) {
	items, err := w.Store.Search(ctx, nsWatches(), nil, 0)
	if err != nil {
		return nil, err
	}
	var out []*Watch
	for _, it := range items {
		wt, err := decode(it.Value)
		if err != nil {
			return nil, err
		}
		if wt.Active {
			out = append(out, wt)
		}
	}
	sort.Slice(out, func(i, j int) bool { return keyOf(out[i].Key) < keyOf(out[j].Key) })
	return out, nil
}

// StartResult is what Start decided.
type StartResult struct {
	Watch  *Watch
	Active bool
	// Reason explains why no watch was needed (closed PR, all green already).
	Reason  string
	Summary github.ChecksSummary
}

func (w *Watcher) repo(k github.PRKey) *github.Repo { return w.GitHub.Repo(k.Owner, k.Repo) }

// Start opens a watch unless the PR is closed or its checks already passed.
// threadID is the agent thread woken on failures.
func (w *Watcher) Start(ctx context.Context, k github.PRKey, threadID string) (StartResult, error) {
	defer w.lock(k)()
	repo := w.repo(k)
	pr, err := repo.GetPR(ctx, k.Number)
	if err != nil {
		return StartResult{}, err
	}
	if pr.State != "open" {
		return StartResult{Reason: "the pull request is " + pr.State + "; no watch needed"}, nil
	}
	sum, err := w.summarize(ctx, repo, pr)
	if err != nil {
		return StartResult{}, err
	}
	if sum.State == "green" {
		return StartResult{Reason: "every required check already passed; no watch needed", Summary: sum}, nil
	}
	wt, err := w.Get(ctx, k)
	if err != nil {
		return StartResult{}, err
	}
	if wt == nil {
		wt = &Watch{Key: k, CreatedAt: w.now()}
	}
	wt.ThreadID, wt.Active, wt.StoppedFor = threadID, true, ""
	wt.EvalErrors = 0
	if wt.HeadSHA != pr.Head.SHA {
		wt.HeadSHA, wt.Retries, wt.DispatchKeys = pr.Head.SHA, 0, nil
	}
	wt.HeadRef, wt.Base = pr.Head.Ref, pr.Base.Ref
	return StartResult{Watch: wt, Active: true, Summary: sum}, w.save(ctx, wt)
}

// Stop ends a watch.
func (w *Watcher) Stop(ctx context.Context, k github.PRKey, reason string) (*Watch, error) {
	defer w.lock(k)()
	wt, err := w.Get(ctx, k)
	if err != nil || wt == nil {
		return wt, err
	}
	wt.Active, wt.StoppedFor = false, reason
	return wt, w.save(ctx, wt)
}

// RecordRetry notes a flaky re-run the agent (or RerunFlaky) did. It refuses
// when the head moved (the evidence is about another commit) or the head
// already used MaxRetriesPerHead, ending the watch so a human triages.
func (w *Watcher) RecordRetry(ctx context.Context, k github.PRKey, head, check, evidence, url string) (*Watch, error) {
	defer w.lock(k)()
	return w.recordRetry(ctx, k, head, check, evidence, url)
}

func (w *Watcher) recordRetry(ctx context.Context, k github.PRKey, head, check, evidence, url string) (*Watch, error) {
	wt, err := w.Get(ctx, k)
	if err != nil {
		return nil, err
	}
	if wt == nil || !wt.Active {
		return nil, fmt.Errorf("babysit: no active watch on %s", k)
	}
	if head != wt.HeadSHA {
		return nil, fmt.Errorf("babysit: head %s is not the watched head %s; re-check the PR", short(head), short(wt.HeadSHA))
	}
	if wt.Retries >= MaxRetriesPerHead {
		wt.Active, wt.StoppedFor = false, "flaky re-run limit reached"
		_ = w.save(ctx, wt)
		return wt, ErrRetryLimit
	}
	wt.Retries++
	wt.Retried = append(wt.Retried, Retry{HeadSHA: head, Check: check, Evidence: evidence, URL: url, At: w.now()})
	if len(wt.Retried) > maxKeys {
		wt.Retried = wt.Retried[len(wt.Retried)-maxKeys:]
	}
	return wt, w.save(ctx, wt)
}

// RerunFlaky re-runs the failed jobs of a failing GitHub Actions check and
// records it. Guards: the watch must be active on this head, the check must
// currently be failing, it must be an Actions check (external providers have
// no safe re-run API), and the head's retry budget must not be spent.
func (w *Watcher) RerunFlaky(ctx context.Context, k github.PRKey, head, check, evidence string) (*Watch, error) {
	defer w.lock(k)()
	wt, err := w.Get(ctx, k)
	if err != nil {
		return nil, err
	}
	if wt == nil || !wt.Active {
		return nil, fmt.Errorf("babysit: no active watch on %s", k)
	}
	if head != wt.HeadSHA {
		return nil, fmt.Errorf("babysit: head %s is not the watched head %s", short(head), short(wt.HeadSHA))
	}
	if wt.Retries >= MaxRetriesPerHead {
		return wt, ErrRetryLimit
	}
	repo := w.repo(k)
	checks, err := repo.ListCheckRuns(ctx, head)
	if err != nil {
		return nil, err
	}
	var target *github.CheckRun
	for i := range checks {
		if checks[i].Name == check {
			target = &checks[i]
		}
	}
	switch {
	case target == nil:
		return nil, fmt.Errorf("babysit: no check named %q on %s", check, short(head))
	case !target.Failing():
		return nil, fmt.Errorf("babysit: check %q is not failing (status %s, conclusion %s)", check, target.Status, target.Conclusion)
	case !target.IsActions():
		return nil, fmt.Errorf("babysit: check %q is not a GitHub Actions check; surface its link to the owner instead: %s", check, target.HTMLURL)
	}
	runID, ok := runIDFromURL(target.HTMLURL, target.DetailsURL)
	if !ok {
		return nil, fmt.Errorf("babysit: cannot find the workflow run of check %q", check)
	}
	if err := repo.RerunFailedJobs(ctx, runID); err != nil {
		if github.IsAccessError(err) {
			return nil, fmt.Errorf("babysit: GitHub denied the re-run (the app needs Actions write permission); do not work around it: %w", err)
		}
		return nil, err
	}
	return w.recordRetry(ctx, k, head, check, evidence, target.HTMLURL)
}

// runIDFromURL extracts N from .../actions/runs/N/....
func runIDFromURL(urls ...string) (int64, bool) {
	for _, u := range urls {
		parts := strings.Split(u, "/")
		for i, p := range parts {
			if p == "runs" && i+1 < len(parts) {
				if n, err := strconv.ParseInt(parts[i+1], 10, 64); err == nil {
					return n, true
				}
			}
		}
	}
	return 0, false
}

func (w *Watcher) summarize(ctx context.Context, repo *github.Repo, pr github.PullRequest) (github.ChecksSummary, error) {
	checks, err := repo.ListCheckRuns(ctx, pr.Head.SHA)
	if err != nil {
		return github.ChecksSummary{}, err
	}
	return github.Summarize(checks, repo.RequiredChecks(ctx, pr.Base.Ref)), nil
}

// Outcome is the result of evaluating a watch.
type Outcome struct {
	State github.ChecksSummary
	// Woken is the wake sent to the agent, if any.
	Woken *Wake
	// Idle means nothing needed a decision: no tokens were spent.
	Idle bool
}

// Evaluate fetches the PR and its checks and decides whether the agent must be
// woken. Pending or unchanged states are idle; a failure wakes the agent once
// per (head, failing set); all-green wakes once and ends the watch; a closed
// PR or repeated API failures end the watch.
func (w *Watcher) Evaluate(ctx context.Context, k github.PRKey) (Outcome, error) {
	defer w.lock(k)()
	wt, err := w.Get(ctx, k)
	if err != nil {
		return Outcome{}, err
	}
	if wt == nil || !wt.Active {
		return Outcome{Idle: true}, nil
	}
	repo := w.repo(k)
	pr, err := repo.GetPR(ctx, k.Number)
	var sum github.ChecksSummary
	if err == nil {
		sum, err = w.summarize(ctx, repo, pr)
	}
	if err != nil {
		wt.EvalErrors++
		if wt.EvalErrors >= MaxEvaluationErrors || github.IsAccessError(err) {
			wt.Active, wt.StoppedFor = false, "GitHub access failed: "+err.Error()
			if serr := w.save(ctx, wt); serr != nil {
				return Outcome{}, serr
			}
			return w.wake(ctx, wt, WakeStopped, "", sum)
		}
		return Outcome{Idle: true}, w.save(ctx, wt)
	}
	wt.EvalErrors = 0
	if pr.State != "open" {
		wt.Active, wt.StoppedFor = false, "the pull request was "+map[bool]string{true: "merged", false: "closed"}[pr.Merged || pr.MergedAt != nil]
		if err := w.save(ctx, wt); err != nil {
			return Outcome{}, err
		}
		return w.wake(ctx, wt, WakeStopped, "", sum)
	}
	if pr.Head.SHA != wt.HeadSHA { // new commit: new evidence, new retry budget
		wt.HeadSHA, wt.Retries, wt.DispatchKeys = pr.Head.SHA, 0, nil
	}
	switch sum.State {
	case "failing":
		key := dispatchKey(wt.HeadSHA, sum.Failing)
		if slices.Contains(wt.DispatchKeys, key) {
			return Outcome{State: sum, Idle: true}, w.save(ctx, wt)
		}
		wt.DispatchKeys = trim(append(wt.DispatchKeys, key), maxKeys)
		if err := w.save(ctx, wt); err != nil {
			return Outcome{}, err
		}
		return w.wake(ctx, wt, WakeFailure, failureSummary(sum, wt), sum)
	case "green":
		wt.Active, wt.StoppedFor = false, "all required checks passed"
		if err := w.save(ctx, wt); err != nil {
			return Outcome{}, err
		}
		return w.wake(ctx, wt, WakeGreen, "", sum)
	}
	return Outcome{State: sum, Idle: true}, w.save(ctx, wt)
}

func dispatchKey(head string, failing []github.CheckRun) string {
	names := make([]string, len(failing))
	for i, f := range failing {
		names[i] = fmt.Sprintf("%s@%d", f.Name, f.ID)
	}
	sort.Strings(names)
	return head + "|" + strings.Join(names, ",")
}

func failureSummary(sum github.ChecksSummary, wt *Watch) string {
	var b strings.Builder
	b.WriteString("Failing checks:\n")
	for _, c := range sum.Failing {
		kind := "GitHub Actions"
		if !c.IsActions() {
			kind = "external provider"
		}
		fmt.Fprintf(&b, "- %s (%s, %s) %s\n", c.Name, c.Conclusion, kind, c.HTMLURL)
	}
	if len(sum.Pending) > 0 {
		fmt.Fprintf(&b, "Still pending: %s\n", github.Names(sum.Pending))
	}
	fmt.Fprintf(&b, "Flaky re-runs used for this head: %d/%d.\n", wt.Retries, MaxRetriesPerHead)
	return b.String()
}

func trim(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func (w *Watcher) wake(ctx context.Context, wt *Watch, kind WakeKind, summary string, sum github.ChecksSummary) (Outcome, error) {
	wk := Wake{Watch: *wt, Kind: kind, Summary: summary}
	if w.Dispatcher != nil {
		if err := w.Dispatcher.Wake(ctx, wk); err != nil {
			// the wake was not delivered: forget the dispatch key so the next
			// evaluation tries again instead of silently losing the failure
			if kind == WakeFailure && len(wt.DispatchKeys) > 0 {
				wt.DispatchKeys = wt.DispatchKeys[:len(wt.DispatchKeys)-1]
				_ = w.save(ctx, wt)
			}
			return Outcome{State: sum}, fmt.Errorf("babysit: waking the agent: %w", err)
		}
	}
	return Outcome{State: sum, Woken: &wk}, nil
}

// HandleCheckEvent reacts to a completed check_run / check_suite webhook: it
// evaluates the watches of the PRs the event belongs to. A delivery is
// processed once even if GitHub redelivers it.
func (w *Watcher) HandleCheckEvent(ctx context.Context, ev github.Event) ([]Outcome, error) {
	if (ev.Type != "check_run" && ev.Type != "check_suite") || ev.Action != "completed" {
		return nil, nil
	}
	watches, err := w.Active(ctx)
	if err != nil {
		return nil, err
	}
	var outs []Outcome
	for _, wt := range watches {
		if !strings.EqualFold(wt.Key.Owner, ev.Owner) || !strings.EqualFold(wt.Key.Repo, ev.Repo) {
			continue
		}
		match := ev.HeadRef != "" && ev.HeadRef == wt.HeadRef || ev.HeadSHA != "" && ev.HeadSHA == wt.HeadSHA || slices.Contains(ev.PRNumbers, wt.Key.Number)
		if !match {
			continue
		}
		if ev.Delivery != "" {
			dup, err := w.markDelivery(ctx, wt.Key, ev.Delivery)
			if err != nil {
				return outs, err
			}
			if dup {
				continue
			}
		}
		o, err := w.Evaluate(ctx, wt.Key)
		if err != nil {
			return outs, err
		}
		outs = append(outs, o)
	}
	return outs, nil
}

func (w *Watcher) markDelivery(ctx context.Context, k github.PRKey, id string) (dup bool, err error) {
	defer w.lock(k)()
	wt, err := w.Get(ctx, k)
	if err != nil || wt == nil {
		return false, err
	}
	if slices.Contains(wt.Deliveries, id) {
		return true, nil
	}
	wt.Deliveries = trim(append(wt.Deliveries, id), maxDeliveries)
	return false, w.save(ctx, wt)
}

// Tick evaluates every active watch: the deterministic fallback that catches
// missed webhooks. It never calls a model; unchanged states are idle.
func (w *Watcher) Tick(ctx context.Context) ([]Outcome, error) {
	watches, err := w.Active(ctx)
	if err != nil {
		return nil, err
	}
	var outs []Outcome
	var errs []error
	for _, wt := range watches {
		o, err := w.Evaluate(ctx, wt.Key)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", wt.Key, err))
			continue
		}
		outs = append(outs, o)
	}
	return outs, errors.Join(errs...)
}

// Run ticks every interval (the original uses 10 minutes) until ctx ends.
func (w *Watcher) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := w.Tick(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
