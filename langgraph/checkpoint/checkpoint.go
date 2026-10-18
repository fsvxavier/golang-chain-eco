// Package checkpoint persists LangGraph state between runs
// (langgraph.checkpoint): a Saver stores one Checkpoint per super-step so a
// thread can be resumed, inspected, forked, or replayed.
package checkpoint

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Write is a single channel write produced by a task.
type Write struct {
	Channel string
	Value   any
}

// Task is a node invocation scheduled for the next super-step.
type Task struct {
	Node   string
	Arg    any  // Send payload; used when HasArg
	HasArg bool // true for tasks created by Send
	// Resumes holds the values already supplied to interrupt() calls of this
	// task, in call order.
	Resumes []any
	// Done tasks finished before the step was interrupted; their Writes are
	// applied on resume instead of re-running the node.
	Done   bool
	Writes []Write
	// InterruptValue is set when the task paused on interrupt().
	InterruptValue any
	Interrupted    bool
}

// Checkpoint is a snapshot of a thread at a super-step boundary.
type Checkpoint struct {
	ID       string
	ParentID string
	ThreadID string
	Step     int
	// Source is "input", "loop", or "update".
	Source   string
	Channels map[string]any // channel snapshots (only non-empty channels)
	Next     []Task
	Metadata map[string]any
}

// Config selects a thread and optionally a checkpoint inside it.
type Config struct {
	ThreadID     string
	CheckpointID string
}

// ErrNotFound is returned when a checkpoint does not exist.
var ErrNotFound = errors.New("checkpoint: not found")

// Saver is the persistence backend.
type Saver interface {
	// Put stores cp (assigning cp.ID when empty) and returns its ID.
	Put(ctx context.Context, cp Checkpoint) (string, error)
	// Get returns the checkpoint named by cfg, or the latest of the thread
	// when cfg.CheckpointID is empty. ok=false when nothing is stored.
	Get(ctx context.Context, cfg Config) (Checkpoint, bool, error)
	// List returns the thread's checkpoints, newest first.
	List(ctx context.Context, threadID string, limit int) ([]Checkpoint, error)
	// DeleteThread removes every checkpoint of the thread.
	DeleteThread(ctx context.Context, threadID string) error
}

// Memory is an in-memory Saver (InMemorySaver).
type Memory struct {
	mu      sync.RWMutex
	threads map[string][]Checkpoint
}

// NewMemory returns an empty in-memory saver.
func NewMemory() *Memory { return &Memory{threads: map[string][]Checkpoint{}} }

func clone(cp Checkpoint) Checkpoint {
	cp.Channels = maps.Clone(cp.Channels)
	cp.Metadata = maps.Clone(cp.Metadata)
	cp.Next = slices.Clone(cp.Next)
	return cp
}

func (m *Memory) Put(_ context.Context, cp Checkpoint) (string, error) {
	if cp.ThreadID == "" {
		return "", errors.New("checkpoint: empty thread id")
	}
	if cp.ID == "" {
		cp.ID = messages.NewID()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.threads[cp.ThreadID] = append(m.threads[cp.ThreadID], clone(cp))
	return cp.ID, nil
}

func (m *Memory) Get(_ context.Context, cfg Config) (Checkpoint, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.threads[cfg.ThreadID]
	if cfg.CheckpointID == "" {
		if len(list) == 0 {
			return Checkpoint{}, false, nil
		}
		return clone(list[len(list)-1]), true, nil
	}
	for _, cp := range list {
		if cp.ID == cfg.CheckpointID {
			return clone(cp), true, nil
		}
	}
	return Checkpoint{}, false, nil
}

func (m *Memory) List(_ context.Context, threadID string, limit int) ([]Checkpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.threads[threadID]
	out := make([]Checkpoint, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		out = append(out, clone(list[i]))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *Memory) DeleteThread(_ context.Context, threadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.threads, threadID)
	return nil
}
