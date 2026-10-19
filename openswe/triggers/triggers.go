// Package triggers turns the outside world into agent runs: GitHub webhooks
// (issue and PR comments, new PRs, pushes, replies to review comments, CI
// results), schedules (cron) and stored automations. Every inbound event is
// claimed once so redeliveries do not run twice, work for a busy thread is
// queued instead of started in parallel, and event text is always passed to
// the agent as untrusted data.
package triggers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Run kinds.
const (
	KindAgent    = "agent"
	KindReviewer = "reviewer"
)

// ThreadID derives a stable thread id (UUID-shaped) from what the thread is
// about, so every event about the same PR or issue lands in the same thread
// and its context and sandbox are reused. kind namespaces the id (the coding
// thread and the reviewer thread of one PR must differ).
func ThreadID(kind, owner, repo string, number int) string {
	sum := sha256.Sum256([]byte(strings.ToLower(fmt.Sprintf("%s|%s/%s#%d", kind, owner, repo, number))))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50 // version 5 layout
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Task is one unit of agent work.
type Task struct {
	// Kind selects the runner (KindAgent or KindReviewer).
	Kind     string
	ThreadID string
	// Source is "github", "schedule", "cli"... and is told to the agent.
	Source   string
	Owner    string
	Repo     string
	Number   int
	Messages []messages.Message
	// Metadata travels in the run's Configurable (requester, head sha...).
	Metadata map[string]any
	// Reason is a short human label for logs.
	Reason string
}

// Runner executes a task to completion.
type Runner interface {
	Run(ctx context.Context, t Task) error
}

// RunnerFunc adapts a function to Runner.
type RunnerFunc func(ctx context.Context, t Task) error

// Run implements Runner.
func (f RunnerFunc) Run(ctx context.Context, t Task) error { return f(ctx, t) }

// Queue is where follow-ups for a busy thread wait (guards.Queue).
type Queue interface {
	Push(thread string, m messages.Message)
	Drain(thread string) []messages.Message
}

// Claims makes "do this once" safe: the first caller for (scope,key) wins.
type Claims interface {
	Claim(ctx context.Context, scope, key string, ttl time.Duration) (bool, error)
}

// MemoryClaims is an in-process Claims with expiry.
type MemoryClaims struct {
	mu  sync.Mutex
	m   map[string]time.Time
	Now func() time.Time
}

// Claim implements Claims.
func (c *MemoryClaims) Claim(_ context.Context, scope, key string, ttl time.Duration) (bool, error) {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]time.Time{}
	}
	k := scope + "\x00" + key
	if exp, ok := c.m[k]; ok && now.Before(exp) {
		return false, nil
	}
	c.m[k] = now.Add(ttl)
	for kk, exp := range c.m { // keep the map from growing without bound
		if now.After(exp) {
			delete(c.m, kk)
		}
	}
	return true, nil
}

// Outcome of Dispatch.
type Outcome int

// Dispatch outcomes.
const (
	Started Outcome = iota // a run was started
	Queued                 // the thread was busy; the messages wait in its queue
)

// Dispatcher runs tasks, one active run per thread. A task for a busy thread
// is queued; the running agent picks the messages up before its next model
// call (guards.QueueMiddleware), and anything that arrives after its last
// call starts one more run, so nothing is lost.
type Dispatcher struct {
	Runners map[string]Runner
	Queue   Queue
	// OnError receives run failures (default: dropped).
	OnError func(Task, error)
	// Background is the context runs use (they outlive the request).
	Background context.Context

	mu   sync.Mutex
	busy map[string]bool
	wg   sync.WaitGroup
}

// Dispatch starts or queues t.
func (d *Dispatcher) Dispatch(ctx context.Context, t Task) (Outcome, error) {
	r := d.Runners[t.Kind]
	if r == nil {
		return 0, fmt.Errorf("triggers: no runner for kind %q", t.Kind)
	}
	if t.ThreadID == "" {
		return 0, fmt.Errorf("triggers: task has no thread id")
	}
	d.mu.Lock()
	if d.busy == nil {
		d.busy = map[string]bool{}
	}
	if d.busy[t.ThreadID] {
		d.mu.Unlock()
		if d.Queue == nil {
			return 0, fmt.Errorf("triggers: thread %s is busy and no queue is configured", t.ThreadID)
		}
		for _, m := range t.Messages {
			d.Queue.Push(t.ThreadID, m)
		}
		return Queued, nil
	}
	d.busy[t.ThreadID] = true
	d.wg.Add(1)
	d.mu.Unlock()

	bg := d.Background
	if bg == nil {
		bg = context.WithoutCancel(ctx)
	}
	go func() {
		defer d.wg.Done()
		d.loop(bg, r, t)
	}()
	return Started, nil
}

func (d *Dispatcher) loop(ctx context.Context, r Runner, t Task) {
	for {
		if err := r.Run(ctx, t); err != nil && d.OnError != nil {
			d.OnError(t, err)
		}
		// messages that arrived after the last model call start another run
		var pending []messages.Message
		if d.Queue != nil {
			pending = d.Queue.Drain(t.ThreadID)
		}
		d.mu.Lock()
		if len(pending) == 0 {
			delete(d.busy, t.ThreadID)
			d.mu.Unlock()
			return
		}
		d.mu.Unlock()
		t.Messages = pending
		t.Reason = "queued follow-up"
	}
}

// Busy reports whether a thread has an active run.
func (d *Dispatcher) Busy(thread string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.busy[thread]
}

// Wait blocks until every started run finished.
func (d *Dispatcher) Wait() { d.wg.Wait() }
