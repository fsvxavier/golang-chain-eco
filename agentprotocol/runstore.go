package agentprotocol

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// RunStore keeps the durable record of each run (the Run resource). Event
// buffers and cancel functions stay in the process; only Run values go here.
// Implementations must be safe for concurrent use.
type RunStore interface {
	// Put inserts or replaces the record of run.RunID (its thread is run.ThreadID).
	Put(ctx context.Context, run Run) error
	// Get returns the record of a run, or nil when there is none.
	Get(ctx context.Context, runID string) (*Run, error)
	// List returns the records of one thread, or of every thread when threadID is "".
	List(ctx context.Context, threadID string) ([]Run, error)
	// Delete removes a record; deleting an unknown run is not an error.
	Delete(ctx context.Context, runID string) error
}

// NewMemoryRunStore returns the default RunStore: records live in the process
// and are lost on restart.
func NewMemoryRunStore() RunStore { return &memRunStore{runs: map[string]Run{}} }

type memRunStore struct {
	mu   sync.Mutex
	runs map[string]Run
}

func (m *memRunStore) Put(_ context.Context, run Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.RunID] = run
	return nil
}

func (m *memRunStore) Get(_ context.Context, runID string) (*Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return nil, nil
	}
	return &run, nil
}

func (m *memRunStore) List(_ context.Context, threadID string) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Run{}
	for _, run := range m.runs {
		if threadID == "" || run.ThreadID == threadID {
			out = append(out, run)
		}
	}
	return out, nil
}

func (m *memRunStore) Delete(_ context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runs, runID)
	return nil
}

// runsNS is the namespace prefix of run records in a store.Store.
var runsNS = []string{"agentprotocol", "runs"}

func runNamespace(threadID string) []string {
	return []string{"agentprotocol", "runs", threadID}
}

type storeRunStore struct{ st store.Store }

// NewStoreRunStore keeps run records in a langgraph store.Store, under
// namespace ["agentprotocol","runs",threadID] with the run id as key. Records
// outlive the process whenever the store does (for example a SQL-backed one).
func NewStoreRunStore(st store.Store) RunStore { return &storeRunStore{st: st} }

func (s *storeRunStore) Put(ctx context.Context, run Run) error {
	v, err := toValue(run)
	if err != nil {
		return err
	}
	return s.st.Put(ctx, runNamespace(run.ThreadID), run.RunID, v)
}

func (s *storeRunStore) Get(ctx context.Context, runID string) (*Run, error) {
	// the id is not part of the namespace, so search by its field
	items, err := s.st.Search(ctx, runsNS, map[string]any{"run_id": runID}, 1)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	run, err := fromValue(items[0].Value)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *storeRunStore) List(ctx context.Context, threadID string) ([]Run, error) {
	prefix := runsNS
	if threadID != "" {
		prefix = runNamespace(threadID)
	}
	items, err := s.st.Search(ctx, prefix, nil, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, len(items))
	for _, it := range items {
		run, err := fromValue(it.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (s *storeRunStore) Delete(ctx context.Context, runID string) error {
	run, err := s.Get(ctx, runID)
	if err != nil || run == nil {
		return err
	}
	return s.st.Delete(ctx, runNamespace(run.ThreadID), runID)
}

func toValue(run Run) (map[string]any, error) {
	b, err := json.Marshal(run)
	if err != nil {
		return nil, err
	}
	var v map[string]any
	err = json.Unmarshal(b, &v)
	return v, err
}

func fromValue(v map[string]any) (Run, error) {
	var run Run
	b, err := json.Marshal(v)
	if err != nil {
		return run, err
	}
	err = json.Unmarshal(b, &run)
	return run, err
}

// crashedRunError is the error recorded on runs that were still pending when
// their process died.
const crashedRunError = "server stopped before the run finished"

// recoverRuns marks runs left pending by an earlier process as errored. A run
// is pending only between admission and its final write, so a pending record
// found at startup belongs to a process that is gone. The protocol has no
// "crashed" status; "error" is the closest one.
// ponytail: assumes one process per store; replicas sharing a store would
// mark each other's live runs. Add a process/lease id when that matters.
func recoverRuns(ctx context.Context, rs RunStore) error {
	runs, err := rs.List(ctx, "")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, run := range runs {
		if run.Status != RunPending {
			continue
		}
		run.Status, run.Error, run.UpdatedAt = RunError, crashedRunError, now
		if err := rs.Put(ctx, run); err != nil {
			return err
		}
	}
	return nil
}
