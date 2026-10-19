package langmem

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// ErrCancelled resolves the Future of a run that was replaced or cancelled
// before it started.
var ErrCancelled = errors.New("langmem: reflection cancelled")

// Future is the eventual result of a submitted reflection.
type Future struct {
	done chan struct{}
	puts []Put
	err  error
}

// Done is closed when the run finished or was cancelled.
func (f *Future) Done() <-chan struct{} { return f.done }

// Wait blocks until the run ends and returns the writes it made.
func (f *Future) Wait() ([]Put, error) { <-f.done; return f.puts, f.err }

// ReflectionExecutor forms memories in the background, "subconsciously": after
// a delay, off the request path (ReflectionExecutor, local mode). Submitting
// again for the same thread before the delay elapses cancels the pending run,
// so the manager sees the complete, longer conversation once instead of
// fragments many times.
//
// For the remote mode of LangMem (running the reflector on a LangGraph
// server so it survives serverless shutdowns) use RemoteReflectionExecutor.
type ReflectionExecutor struct {
	// Manager does the work. Alternatively set Run.
	Manager *StoreManager
	// Run replaces Manager for custom reflectors.
	Run func(ctx context.Context, in Payload) ([]Put, error)
	// Store used by Manager and Search when the context carries none.
	Store store.Store

	mu      sync.Mutex
	pending map[string]*job
	wg      sync.WaitGroup
}

type job struct {
	timer  *time.Timer
	fut    *Future
	start  func()
	begun  bool
	cancel func()
}

// SubmitOptions tunes Submit.
type SubmitOptions struct {
	// After delays the run (typically minutes in production).
	After time.Duration
	// ThreadID identifies the conversation to debounce; default
	// Configurable["thread_id"]. Empty means never debounced.
	ThreadID string
}

// Submit schedules a memory-formation run. The run receives ctx's values
// (configurable, store) but not its cancellation: the request that submitted
// it usually finishes first.
func (e *ReflectionExecutor) Submit(ctx context.Context, in Payload, o SubmitOptions) (*Future, error) {
	run := e.Run
	if run == nil {
		if e.Manager == nil {
			return nil, fmt.Errorf("langmem: ReflectionExecutor needs a Manager or Run")
		}
		run = e.Manager.Invoke
	}
	thread := o.ThreadID
	if thread == "" {
		thread, _ = runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	}
	bg := context.WithoutCancel(ctx)
	if e.Store != nil && e.Manager != nil && e.Manager.cfg.store == nil {
		e.Manager.cfg.store = e.Store
	}
	in.Messages = append(in.Messages[:0:0], in.Messages...)

	fut := &Future{done: make(chan struct{})}
	j := &job{fut: fut}
	finish := func(puts []Put, err error) {
		fut.puts, fut.err = puts, err
		close(fut.done)
		e.wg.Done()
	}
	j.start = func() {
		e.mu.Lock()
		if j.begun {
			e.mu.Unlock()
			return
		}
		j.begun = true
		if thread != "" && e.pending[thread] == j {
			delete(e.pending, thread)
		}
		e.mu.Unlock()
		finish(run(bg, in))
	}
	j.cancel = func() {
		if j.timer.Stop() {
			j.begun = true
			finish(nil, ErrCancelled)
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pending == nil {
		e.pending = map[string]*job{}
	}
	if old, ok := e.pending[thread]; ok && thread != "" {
		delete(e.pending, thread)
		if !old.begun {
			old.cancel()
		}
	}
	e.wg.Add(1)
	j.timer = time.AfterFunc(o.After, j.start)
	if thread != "" {
		e.pending[thread] = j
	}
	return fut, nil
}

// Search reads memories from the executor's namespace.
func (e *ReflectionExecutor) Search(ctx context.Context, query string, filter map[string]any, limit int) ([]Hit, error) {
	if e.Manager == nil {
		return nil, fmt.Errorf("langmem: Search needs a Manager")
	}
	return e.Manager.Search(ctx, query, filter, limit)
}

// Shutdown ends the executor. With cancel, pending runs are dropped;
// otherwise they start immediately instead of waiting out their delay. When
// wait is set it blocks until every run has finished.
func (e *ReflectionExecutor) Shutdown(wait, cancel bool) {
	e.mu.Lock()
	jobs := make([]*job, 0, len(e.pending))
	for k, j := range e.pending {
		jobs = append(jobs, j)
		delete(e.pending, k)
	}
	e.mu.Unlock()
	for _, j := range jobs {
		if cancel {
			e.mu.Lock()
			if !j.begun {
				j.cancel()
			}
			e.mu.Unlock()
		} else if j.timer.Stop() {
			go j.start()
		}
	}
	if wait {
		e.wg.Wait()
	}
}
