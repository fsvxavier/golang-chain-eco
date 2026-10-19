package guards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
	"github.com/fsvxavier/golang-chain-eco/openswe/sandbox"
)

// ErrSandboxDown is returned by SandboxCircuitBreaker once the sandbox has
// failed repeatedly: nothing the model does next can succeed, so the run ends.
var ErrSandboxDown = errors.New("sandbox is down")

func threadOf(ctx context.Context) string {
	t, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	return t
}

// ------------------------------------------------------------ tool errors

// ToolErrorHandler turns a failing tool into an error result the model can
// read, instead of crashing the run. Fatal errors (a dead sandbox, a cancelled
// context) still end the run.
type ToolErrorHandler struct {
	// Fatal decides which errors propagate (default: ErrSandboxDown and
	// context cancellation).
	Fatal func(error) bool
}

// Name implements agents.Middleware.
func (ToolErrorHandler) Name() string { return "tool_error_handler" }

// WrapToolCall implements agents.WrapTool.
func (h ToolErrorHandler) WrapToolCall(ctx context.Context, call messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	out, err := next(ctx, call)
	if err == nil {
		return out, nil
	}
	fatal := h.Fatal
	if fatal == nil {
		fatal = func(e error) bool {
			return errors.Is(e, ErrSandboxDown) || errors.Is(e, context.Canceled)
		}
	}
	if fatal(err) || ctx.Err() != nil {
		return messages.Message{}, err
	}
	payload, _ := json.Marshal(map[string]string{"status": "error", "error": err.Error(), "error_type": fmt.Sprintf("%T", err), "name": call.Name})
	m := messages.Tool(string(payload), call.ID)
	m.Name, m.Status = call.Name, "error"
	return m, nil
}

// ---------------------------------------------------------- circuit breaker

// SandboxCircuitBreaker watches for a sandbox that stopped answering. The
// first transient failures are shown to the model (it may be a blip); at
// Threshold consecutive failures the user is notified once and the run ends
// with ErrSandboxDown instead of burning the step budget on doomed calls.
type SandboxCircuitBreaker struct {
	// Threshold of consecutive transient failures (default 3).
	Threshold int
	// Notify tells the person who triggered the thread, on the channel they
	// used. Called at most once per thread until a call succeeds again.
	Notify func(ctx context.Context, thread string, err error)

	mu       sync.Mutex
	failures map[string]int
	notified map[string]bool
}

// Name implements agents.Middleware.
func (*SandboxCircuitBreaker) Name() string { return "sandbox_circuit_breaker" }

// WrapToolCall implements agents.WrapTool.
func (b *SandboxCircuitBreaker) WrapToolCall(ctx context.Context, call messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	out, err := next(ctx, call)
	thread := threadOf(ctx)
	b.mu.Lock()
	if b.failures == nil {
		b.failures, b.notified = map[string]int{}, map[string]bool{}
	}
	if err == nil || !sandbox.IsTransient(err) {
		if err == nil {
			delete(b.failures, thread)
			delete(b.notified, thread)
		}
		b.mu.Unlock()
		return out, err
	}
	b.failures[thread]++
	n, limit := b.failures[thread], b.Threshold
	if limit <= 0 {
		limit = 3
	}
	first := !b.notified[thread]
	if n >= limit {
		b.notified[thread] = true
	}
	b.mu.Unlock()
	if n >= limit {
		if first && b.Notify != nil {
			b.Notify(ctx, thread, err)
		}
		return messages.Message{}, fmt.Errorf("%w after %d consecutive failures: %v", ErrSandboxDown, n, err)
	}
	m := messages.Tool(fmt.Sprintf("The sandbox did not respond (%d/%d): %v. It may recover; retry once, and do not assume your last command ran.", n, limit, err), call.ID)
	m.Name, m.Status = call.Name, "error"
	return m, nil
}

// ------------------------------------------------------- orphaned tool calls

// RepairOrphanedToolCalls fixes message lists where an AI message asked for a
// tool but no result was ever stored (the run was cancelled or the sandbox
// died mid-call). Without this the next model request is rejected by the
// provider ("tool_use ids were found without tool_result"), and every retry
// hits the same error: the thread is wedged for good. A synthetic error
// result is inserted right after the call so the model sees an interrupted
// tool and can retry.
type RepairOrphanedToolCalls struct{}

// Name implements agents.Middleware.
func (RepairOrphanedToolCalls) Name() string { return "repair_orphaned_tool_calls" }

// InterruptedMessage is what the synthetic result says.
const InterruptedMessage = "The previous tool call did not complete: the run was interrupted (cancelled, or the sandbox became unavailable) before a result was returned. No output was captured. Retry the call if you still need it; if repository files look missing, re-clone or reinitialise the workspace first."

// Repair returns msgs with a synthetic result after every unanswered tool
// call, and whether anything changed.
func Repair(msgs []messages.Message) ([]messages.Message, bool) {
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == messages.RoleTool && m.ToolCallID != "" {
			answered[m.ToolCallID] = true
		}
	}
	var out []messages.Message
	changed := false
	for _, m := range msgs {
		out = append(out, m)
		if m.Role != messages.RoleAI {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == "" || answered[tc.ID] {
				continue
			}
			payload, _ := json.Marshal(map[string]string{"status": "error", "error_type": "InterruptedToolCall", "recovery": "tool_call_interrupted", "error": InterruptedMessage, "name": tc.Name})
			res := messages.Tool(string(payload), tc.ID)
			res.Name, res.Status = tc.Name, "error"
			out = append(out, res)
			changed = true
		}
	}
	return out, changed
}

// WrapModelCall implements agents.WrapModel.
func (RepairOrphanedToolCalls) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	if fixed, changed := Repair(req.Messages); changed {
		r := *req
		r.Messages = fixed
		req = &r
	}
	return next(ctx, req)
}

// ------------------------------------------------------------- model timeout

// ModelTimeout bounds one model call so a hung provider cannot stall a run
// forever; the error is then retried or fallen back on by other middleware.
type ModelTimeout struct{ D time.Duration }

// Name implements agents.Middleware.
func (ModelTimeout) Name() string { return "model_call_timeout" }

// WrapModelCall implements agents.WrapModel.
func (t ModelTimeout) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	if t.D <= 0 {
		return next(ctx, req)
	}
	ctx, cancel := context.WithTimeout(ctx, t.D)
	defer cancel()
	out, err := next(ctx, req)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("model call timed out after %s: %w", t.D, err)
	}
	return out, err
}

// ------------------------------------------------------------ message queue

// Queue holds follow-up messages that arrived while a thread's run was
// active. The run picks them up before its next model call, so an engineer
// can steer or add to the work in flight instead of waiting for it to end.
type Queue interface {
	Push(thread string, m messages.Message)
	// Drain removes and returns the thread's queued messages, oldest first.
	Drain(thread string) []messages.Message
}

// MemoryQueue is an in-process Queue.
type MemoryQueue struct {
	mu sync.Mutex
	q  map[string][]messages.Message
}

// Push implements Queue.
func (m *MemoryQueue) Push(thread string, msg messages.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.q == nil {
		m.q = map[string][]messages.Message{}
	}
	if msg.ID == "" {
		msg.ID = messages.NewID()
	}
	m.q[thread] = append(m.q[thread], msg)
}

// Drain implements Queue.
func (m *MemoryQueue) Drain(thread string) []messages.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.q[thread]
	delete(m.q, thread)
	return out
}

// Len reports how many messages wait for a thread.
func (m *MemoryQueue) Len(thread string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.q[thread])
}

// QueueMiddleware injects queued follow-ups before each model call.
type QueueMiddleware struct{ Queue Queue }

// Name implements agents.Middleware.
func (QueueMiddleware) Name() string { return "check_message_queue" }

// BeforeModel implements agents.BeforeModel.
func (q QueueMiddleware) BeforeModel(ctx context.Context, _ graph.State) (any, error) {
	msgs := q.Queue.Drain(threadOf(ctx))
	if len(msgs) == 0 {
		return nil, nil
	}
	return graph.State{"messages": msgs}, nil
}
