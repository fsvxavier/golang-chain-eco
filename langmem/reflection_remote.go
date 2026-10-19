package langmem

import (
	"context"
	"errors"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/remote"
)

// RemoteReflectionExecutor is the remote mode of ReflectionExecutor: the memory
// graph runs on a LangGraph Platform style server (remote/server), so the
// debounced run survives this process shutting down. Each Submit creates a run
// with after_seconds on the thread; a newer Submit on the same thread uses
// multitask_strategy "rollback", which cancels the pending run on the server.
//
// Differences from the local executor: a run that already started is rolled
// back too (the newer submission carries the full conversation anyway), the
// thread ID is required, and Future.Wait returns no Puts because the writes
// happen on the server.
type RemoteReflectionExecutor struct {
	Client  *remote.Client
	GraphID string
	// Config is sent with every run (configurable, recursion_limit...).
	Config map[string]any

	mu      sync.Mutex
	pending map[string]*remoteJob
	wg      sync.WaitGroup
}

type remoteJob struct {
	thread, runID string
}

// RemoteOption tunes NewRemoteReflectionExecutor.
type RemoteOption func(*RemoteReflectionExecutor)

// WithRemoteConfig sets the config sent with every run.
func WithRemoteConfig(cfg map[string]any) RemoteOption {
	return func(e *RemoteReflectionExecutor) { e.Config = cfg }
}

// NewRemoteReflectionExecutor runs the graph graphID on client's server.
func NewRemoteReflectionExecutor(client *remote.Client, graphID string, opts ...RemoteOption) *RemoteReflectionExecutor {
	e := &RemoteReflectionExecutor{Client: client, GraphID: graphID, pending: map[string]*remoteJob{}}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Submit schedules a memory-formation run on the server after o.After.
func (e *RemoteReflectionExecutor) Submit(ctx context.Context, in Payload, o SubmitOptions) (*Future, error) {
	thread := o.ThreadID
	if thread == "" {
		thread, _ = runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	}
	if thread == "" {
		return nil, errors.New("langmem: remote reflection needs a thread ID (runs are debounced per thread)")
	}
	bg := context.WithoutCancel(ctx)
	run, err := e.Client.CreateRun(bg, thread, remote.RunRequest{
		AssistantID:  e.GraphID,
		Input:        map[string]any{"messages": in.Messages, "max_steps": in.MaxSteps},
		Config:       e.Config,
		Multitask:    "rollback",
		AfterSeconds: o.After.Seconds(),
	})
	if err != nil {
		return nil, err
	}

	fut := &Future{done: make(chan struct{})}
	j := &remoteJob{thread: thread, runID: run.RunID}
	e.mu.Lock()
	e.pending[thread] = j
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer close(fut.done)
		defer func() {
			e.mu.Lock()
			if e.pending[thread] == j {
				delete(e.pending, thread)
			}
			e.mu.Unlock()
		}()
		if _, err := e.Client.Join(bg, thread, run.RunID); err != nil {
			fut.err = err
			return
		}
		st, err := e.Client.GetRun(bg, thread, run.RunID)
		switch {
		case err != nil:
			fut.err = err
		case st.Status == "interrupted":
			fut.err = ErrCancelled
		case st.Status == "error":
			fut.err = errors.New("langmem: remote reflection failed")
		}
	}()
	return fut, nil
}

// Shutdown ends the executor. With cancel, the pending server runs are
// cancelled; otherwise they fire at their scheduled time on the server (there
// is no API to start them early). When wait is set it blocks until every
// tracked run has finished.
func (e *RemoteReflectionExecutor) Shutdown(wait, cancel bool) {
	e.mu.Lock()
	jobs := make([]*remoteJob, 0, len(e.pending))
	for _, j := range e.pending {
		jobs = append(jobs, j)
	}
	e.mu.Unlock()
	if cancel {
		for _, j := range jobs {
			_ = e.Client.CancelRun(context.Background(), j.thread, j.runID)
		}
	}
	if wait {
		e.wg.Wait()
	}
}
