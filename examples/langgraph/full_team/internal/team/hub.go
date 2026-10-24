// Package team holds what the independent services share: the hub (registry
// with atomic reservation, job queue, shared knowledge over MCP), the worker
// that serves one agent over Agent Protocol and pulls jobs, and the manager
// that runs the delivery flow.
package team

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Job is one unit of work for one agent. It carries everything the worker
// needs (the prompt with the whole dossier, the thread that keeps the agent's
// conversation across rounds, the grounding question), so a worker that
// restarts loses nothing.
type Job struct {
	ID string `json:"id"`
	// Prefer lists worker ids or roles in order of preference. The first one
	// with a free worker wins, so "senior" never waits behind a junior.
	Prefer []string `json:"prefer"`
	Thread string   `json:"thread"`
	Prompt string   `json:"prompt"`
	Query  string   `json:"query,omitempty"` // grounding question for the knowledge tools
	Scope  string   `json:"scope,omitempty"` // team-memory scope: the request id

	Status   string `json:"status"` // queued, running, done, failed
	Worker   string `json:"worker,omitempty"`
	Role     string `json:"role,omitempty"` // role of the worker the job was reserved for
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Attempts int    `json:"attempts"`
	// Unix milliseconds: when the job was handed to a worker and when it ended
	// (0 = not yet). The dashboard derives execution time from them.
	StartedMS int64 `json:"started_ms,omitempty"`
	DoneMS    int64 `json:"done_ms,omitempty"`
	// Usage is what the model calls of this job consumed (nil when the worker
	// did not report any, e.g. the offline model).
	Usage *messages.Usage `json:"usage,omitempty"`

	delivered bool
}

// CancelledMsg is the error of a job cancelled with CancelScope.
const CancelledMsg = "cancelled by user"

// WorkerInfo is a registered worker. It is alive while Seen is within the TTL.
type WorkerInfo struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	URL  string `json:"url"`
	// Model is the label of the model the worker was built with ("offline" or
	// the provider:model spec). Empty for a worker that does not report it.
	Model string    `json:"model,omitempty"`
	Job   string    `json:"job,omitempty"` // non-empty means reserved
	Seen  time.Time `json:"seen"`
	// Skill is the role's skill folder, Refs the reference skills vendored into
	// it, and Activity what the worker says it is doing right now (reference
	// consulted, tool called); the dashboard shows them.
	Skill    string   `json:"skill,omitempty"`
	Refs     []string `json:"refs,omitempty"`
	Activity string   `json:"activity,omitempty"`

	cancel string // id of a job cancelled while this worker ran it; sent on its next heartbeat
}

// ErrUnknownWorker tells a worker its lease expired: it must register again.
var ErrUnknownWorker = errors.New("hub: unknown worker (lease expired), register again")

// ErrNotOwner is returned when a worker completes a job it no longer holds.
var ErrNotOwner = errors.New("hub: job is not reserved by this worker")

// Hub is the registry and the queue. Every reservation happens under one lock.
// ponytail: in-memory single instance; a replicated hub needs the same
// transitions as compare-and-swap in a shared store (Redis, SQL).
type Hub struct {
	TTL         time.Duration // lease: a worker silent for longer is dropped
	MaxAttempts int           // times a job may be handed out before it fails

	cancelled map[string]bool           // request ids whose work was cancelled: new jobs for them fail at once
	mgrUse    map[string]messages.Usage // manager model tokens per request id
	mu        sync.Mutex
	workers   map[string]*WorkerInfo
	jobs      map[string]*Job
	queue     []string
	wake      chan struct{}
	seq       int
	now       func() time.Time
}

// NewHub returns an empty hub.
func NewHub(ttl time.Duration) *Hub {
	return &Hub{TTL: ttl, MaxAttempts: 3, cancelled: map[string]bool{}, mgrUse: map[string]messages.Usage{}, workers: map[string]*WorkerInfo{}, jobs: map[string]*Job{}, wake: make(chan struct{}), now: time.Now}
}

// notifyLocked wakes every waiter; callers hold h.mu.
func (h *Hub) notifyLocked() {
	close(h.wake)
	h.wake = make(chan struct{})
}

// Register adds or refreshes a worker. A worker that registers again while
// holding a job crashed and came back: its job goes back to the queue.
func (h *Hub) Register(id, role, url string) {
	h.RegisterInfo(WorkerInfo{ID: id, Role: role, URL: url})
}

// RegisterInfo is Register for a worker that also reports its model.
func (h *Hub) RegisterInfo(wi WorkerInfo) {
	id := wi.ID
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.workers[id]; old != nil && old.Job != "" {
		h.requeueLocked(old.Job)
	}
	h.workers[id] = &WorkerInfo{ID: id, Role: wi.Role, URL: wi.URL, Model: wi.Model, Skill: wi.Skill, Refs: wi.Refs, Seen: h.now()}
	h.scheduleLocked()
	h.notifyLocked()
}

// Heartbeat renews a lease. It reports false when the worker is unknown.
func (h *Hub) Heartbeat(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.workers[id]
	if w != nil {
		w.Seen = h.now()
	}
	return w != nil
}

// Submit queues a job and returns its id.
func (h *Hub) Submit(j Job) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	j.ID = j.Thread + "." + strconv.Itoa(h.seq)
	j.Status = "queued"
	h.jobs[j.ID] = &j
	if j.Scope != "" && h.cancelled[j.Scope] {
		j.Status, j.Error, j.DoneMS = "failed", CancelledMsg, h.now().UnixMilli()
		h.notifyLocked()
		return j.ID
	}
	h.queue = append(h.queue, j.ID)
	h.scheduleLocked()
	h.notifyLocked()
	return j.ID
}

// scheduleLocked reserves, in queue order, the first preferred free worker for
// each job. Reserving is the only place a worker becomes busy.
func (h *Hub) scheduleLocked() {
	ids := make([]string, 0, len(h.workers))
	for id := range h.workers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var left []string
	for _, jid := range h.queue {
		j := h.jobs[jid]
		w := h.pickLocked(j.Prefer, ids)
		if w == nil {
			left = append(left, jid)
			continue
		}
		w.Job, j.Worker, j.Role, j.Status, j.delivered = j.ID, w.ID, w.Role, "running", false
		j.StartedMS = h.now().UnixMilli()
	}
	h.queue = left
}

func (h *Hub) pickLocked(prefer, ids []string) *WorkerInfo {
	for _, p := range prefer {
		for _, id := range ids {
			if w := h.workers[id]; w.Job == "" && (w.ID == p || w.Role == p) && h.now().Sub(w.Seen) <= h.TTL {
				return w
			}
		}
	}
	return nil
}

// requeueLocked takes a job back from its worker and puts it first in line, or
// fails it once it has been handed out MaxAttempts times.
func (h *Hub) requeueLocked(jid string) {
	j := h.jobs[jid]
	if j == nil || j.Status != "running" {
		return
	}
	j.Attempts++
	j.Worker = ""
	if j.Attempts >= h.MaxAttempts {
		j.Status, j.Error = "failed", "no worker finished the job after "+strconv.Itoa(j.Attempts)+" attempts"
		j.DoneMS = h.now().UnixMilli()
		return
	}
	j.Status = "queued"
	h.queue = append([]string{jid}, h.queue...)
}

// Reap drops workers whose lease expired and requeues their jobs.
func (h *Hub) Reap() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, w := range h.workers {
		if h.now().Sub(w.Seen) > h.TTL {
			delete(h.workers, id)
			if w.Job != "" {
				h.requeueLocked(w.Job)
			}
		}
	}
	h.scheduleLocked()
	h.notifyLocked()
}

// Run reaps expired leases until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(h.TTL / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Reap()
		}
	}
}

// Claim hands the worker the job reserved for it, waiting up to wait. A nil
// job with a nil error means nothing arrived. Claiming renews the lease.
func (h *Hub) Claim(ctx context.Context, id string, wait time.Duration) (*Job, error) {
	timeout := time.After(wait)
	for {
		h.mu.Lock()
		w := h.workers[id]
		if w == nil {
			h.mu.Unlock()
			return nil, ErrUnknownWorker
		}
		w.Seen = h.now()
		if j := h.jobs[w.Job]; j != nil && !j.delivered {
			j.delivered = true
			cp := *j
			h.mu.Unlock()
			return &cp, nil
		}
		wake := h.wake
		h.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return nil, nil
		case <-timeout:
			return nil, nil
		}
	}
}

// Complete records a worker's result and frees it. A worker that lost its
// lease meanwhile gets ErrNotOwner and its result is dropped.
func (h *Hub) Complete(workerID, jobID, output, errMsg string) error {
	return h.CompleteUsage(workerID, jobID, output, errMsg, nil)
}

// CompleteUsage is Complete that also records the tokens the job consumed.
func (h *Hub) CompleteUsage(workerID, jobID, output, errMsg string, u *messages.Usage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	j, w := h.jobs[jobID], h.workers[workerID]
	if j == nil || w == nil || j.Worker != workerID || j.Status != "running" {
		return ErrNotOwner
	}
	w.Job, w.Activity = "", ""
	w.Seen = h.now()
	j.Output, j.Error, j.Status = output, errMsg, "done"
	j.DoneMS, j.Usage = h.now().UnixMilli(), u
	if errMsg != "" {
		j.Status = "failed"
	}
	h.scheduleLocked()
	h.notifyLocked()
	return nil
}

// Wait blocks until the job is done or failed, or ctx ends.
func (h *Hub) Wait(ctx context.Context, id string) (Job, error) {
	for {
		h.mu.Lock()
		j := h.jobs[id]
		if j == nil {
			h.mu.Unlock()
			return Job{}, errors.New("hub: unknown job " + id)
		}
		if j.Status == "done" || j.Status == "failed" {
			cp := *j
			h.mu.Unlock()
			return cp, nil
		}
		wake := h.wake
		h.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return Job{}, ctx.Err()
		}
	}
}

// State is a snapshot for humans and tests.
type State struct {
	Workers []WorkerInfo `json:"workers"`
	Queued  []string     `json:"queued"`
}

// Snapshot returns the workers (sorted by id) and the queued job ids.
func (h *Hub) Snapshot() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := State{Queued: slices.Clone(h.queue)}
	for _, w := range h.workers {
		s.Workers = append(s.Workers, *w)
	}
	slices.SortFunc(s.Workers, func(a, b WorkerInfo) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	return s
}

// Jobs lists every job without its prompt and output (they can be large), in
// submission order, for the dashboard.
func (h *Hub) Jobs() []Job {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Job, 0, len(h.jobs))
	for _, j := range h.jobs {
		cp := *j
		cp.Prompt, cp.Output = "", ""
		out = append(out, cp)
	}
	slices.SortFunc(out, func(a, b Job) int { return cmpJobSeq(a.ID, b.ID) })
	return out
}

// cmpJobSeq orders ids "<thread>.<seq>" by thread, then by the numeric seq.
func cmpJobSeq(a, b string) int {
	ta, sa, _ := strings.Cut(a, ".")
	tb, sb, _ := strings.Cut(b, ".")
	if c := strings.Compare(ta, tb); c != 0 {
		return c
	}
	na, _ := strconv.Atoi(sa)
	nb, _ := strconv.Atoi(sb)
	return na - nb
}

// AddMgrUsage adds tokens the manager's own agent used for request id.
func (h *Hub) AddMgrUsage(id string, u messages.Usage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cur := h.mgrUse[id]
	cur.InputTokens += u.InputTokens
	cur.OutputTokens += u.OutputTokens
	cur.TotalTokens += u.TotalTokens
	cur.CacheReadTokens += u.CacheReadTokens
	cur.CacheCreationTokens += u.CacheCreationTokens
	h.mgrUse[id] = cur
}

// MgrUsage returns a copy of the manager's tokens per request id.
func (h *Hub) MgrUsage() map[string]messages.Usage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.mgrUse)
}

// SetActivity records what a worker is doing; false when the worker is unknown.
func (h *Hub) SetActivity(id, activity string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.workers[id]
	if w != nil {
		w.Activity = activity
	}
	return w != nil
}

// CancelScope ends the work of request id: queued jobs fail at once, running
// ones fail and free their worker (which learns of it on its next heartbeat and
// stops the agent), and any job submitted for the request later fails too, so
// the manager's flow unwinds on its next step. It returns how many jobs it
// ended. ClearCancel (a new run of the thread) lifts the cancellation.
func (h *Hub) CancelScope(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancelled[id] = true
	n := 0
	for _, j := range h.jobs {
		if j.Scope != id || (j.Status != "queued" && j.Status != "running") {
			continue
		}
		if w := h.workers[j.Worker]; j.Status == "running" && w != nil && w.Job == j.ID {
			w.Job, w.Activity, w.cancel = "", "", j.ID
		}
		j.Status, j.Error, j.DoneMS = "failed", CancelledMsg, h.now().UnixMilli()
		n++
	}
	h.queue = slices.DeleteFunc(h.queue, func(jid string) bool { return h.jobs[jid].Status != "queued" })
	h.scheduleLocked()
	h.notifyLocked()
	return n
}

// TakeCancel returns, once, the id of a job of this worker that was cancelled.
func (h *Hub) TakeCancel(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.workers[id]
	if w == nil {
		return ""
	}
	c := w.cancel
	w.cancel = ""
	return c
}

// ClearCancel lifts the cancellation of request id: a new run starts clean.
func (h *Hub) ClearCancel(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.cancelled, id)
}
