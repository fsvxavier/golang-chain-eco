package middleware

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
	"github.com/fsvxavier/golang-chain-eco/remote"
)

// AsyncStatus of a background task.
type AsyncStatus string

const (
	AsyncRunning   AsyncStatus = "running"
	AsyncSuccess   AsyncStatus = "success"
	AsyncError     AsyncStatus = "error"
	AsyncCancelled AsyncStatus = "cancelled"
)

// AsyncRunner executes subagent work in the background: in-process
// (LocalRunner) or on an Agent Server (RemoteRunner).
type AsyncRunner interface {
	Start(ctx context.Context, input string) (handle string, err error)
	Poll(ctx context.Context, handle string) (status AsyncStatus, result string, err error)
	// Send gives follow-up instructions; it may return a new handle.
	Send(ctx context.Context, handle, message string) (newHandle string, err error)
	Cancel(ctx context.Context, handle string) error
}

// AsyncSubAgent is a named background delegate.
type AsyncSubAgent struct {
	Name, Description string
	Runner            AsyncRunner
}

// AsyncTask is the tracked state of one launched task (state["async_tasks"]).
type AsyncTask struct {
	ID          string
	Agent       string
	Handle      string
	Description string
	Status      AsyncStatus
	Result      string
}

// ---------------------------------------------------------------- local

type localJob struct {
	cancel func()
	done   chan struct{}
	result string
	err    error
	status AsyncStatus
}

// LocalRunner runs a compiled agent in goroutines.
type LocalRunner struct {
	Agent *graph.Compiled
	mu    sync.Mutex
	jobs  map[string]*localJob
	n     int
}

func (l *LocalRunner) Start(ctx context.Context, input string) (string, error) {
	// the job outlives the tool call that launched it
	jctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	job := &localJob{cancel: cancel, done: make(chan struct{}), status: AsyncRunning}
	l.mu.Lock()
	if l.jobs == nil {
		l.jobs = map[string]*localJob{}
	}
	l.n++
	h := fmt.Sprintf("local-%d", l.n)
	l.jobs[h] = job
	l.mu.Unlock()
	go func() {
		defer close(job.done)
		out, err := l.Agent.Invoke(jctx, graph.State{"messages": []messages.Message{messages.Human(input)}})
		l.mu.Lock()
		defer l.mu.Unlock()
		switch {
		case job.status == AsyncCancelled:
		case err != nil:
			job.status, job.err = AsyncError, err
		default:
			job.status = AsyncSuccess
			ms, _ := messages.Coerce(out["messages"])
			if m, ok := messages.LastOfRole(ms, messages.RoleAI); ok {
				job.result = m.Text()
			}
		}
	}()
	return h, nil
}

func (l *LocalRunner) job(h string) (*localJob, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j := l.jobs[h]
	if j == nil {
		return nil, fmt.Errorf("unknown task handle %q", h)
	}
	return j, nil
}

func (l *LocalRunner) Poll(_ context.Context, h string) (AsyncStatus, string, error) {
	j, err := l.job(h)
	if err != nil {
		return "", "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if j.err != nil {
		return j.status, j.err.Error(), nil
	}
	return j.status, j.result, nil
}

func (l *LocalRunner) Cancel(_ context.Context, h string) error {
	j, err := l.job(h)
	if err != nil {
		return err
	}
	l.mu.Lock()
	if j.status == AsyncRunning {
		j.status = AsyncCancelled
	}
	l.mu.Unlock()
	j.cancel()
	return nil
}

// Send cancels the running job and restarts the agent with the message.
func (l *LocalRunner) Send(ctx context.Context, h, msg string) (string, error) {
	if err := l.Cancel(ctx, h); err != nil {
		return "", err
	}
	return l.Start(ctx, msg)
}

// ---------------------------------------------------------------- remote

// RemoteRunner delegates to an assistant on an Agent Server.
type RemoteRunner struct {
	Client      *remote.Client
	AssistantID string
}

func userInput(msg string) map[string]any {
	return map[string]any{"messages": []any{map[string]any{"role": "user", "content": msg}}}
}

func (r RemoteRunner) Start(ctx context.Context, input string) (string, error) {
	tid, err := r.Client.CreateThread(ctx, nil)
	if err != nil {
		return "", err
	}
	run, err := r.Client.CreateRun(ctx, tid, remote.RunRequest{AssistantID: r.AssistantID, Input: userInput(input)})
	if err != nil {
		return "", err
	}
	return tid + "/" + run.RunID, nil
}

func splitHandle(h string) (string, string, error) {
	t, r, ok := strings.Cut(h, "/")
	if !ok {
		return "", "", fmt.Errorf("bad task handle %q", h)
	}
	return t, r, nil
}

func (r RemoteRunner) Poll(ctx context.Context, h string) (AsyncStatus, string, error) {
	tid, rid, err := splitHandle(h)
	if err != nil {
		return "", "", err
	}
	run, err := r.Client.GetRun(ctx, tid, rid)
	if err != nil {
		return "", "", err
	}
	switch run.Status {
	case "success":
		st, err := r.Client.GetState(ctx, tid)
		if err != nil {
			return "", "", err
		}
		return AsyncSuccess, lastContent(st.Values), nil
	case "error", "timeout":
		return AsyncError, "run " + run.Status, nil
	case "interrupted":
		return AsyncCancelled, "", nil
	}
	return AsyncRunning, "", nil
}

func lastContent(values map[string]any) string {
	ms, _ := values["messages"].([]any)
	for i := len(ms) - 1; i >= 0; i-- {
		if m, ok := ms[i].(map[string]any); ok && (m["type"] == "ai" || m["role"] == "assistant") {
			if s, ok := m["content"].(string); ok {
				return s
			}
		}
	}
	return ""
}

func (r RemoteRunner) Send(ctx context.Context, h, msg string) (string, error) {
	tid, _, err := splitHandle(h)
	if err != nil {
		return "", err
	}
	run, err := r.Client.CreateRun(ctx, tid, remote.RunRequest{AssistantID: r.AssistantID, Input: userInput(msg), Multitask: "interrupt"})
	if err != nil {
		return "", err
	}
	return tid + "/" + run.RunID, nil
}

func (r RemoteRunner) Cancel(ctx context.Context, h string) error {
	tid, rid, err := splitHandle(h)
	if err != nil {
		return err
	}
	return r.Client.CancelRun(ctx, tid, rid)
}

// ---------------------------------------------------------------- middleware

// AsyncSubagents lets the main agent launch long-running subagents without
// blocking: start_async_task returns an ID at once; the agent later checks,
// updates, cancels or lists tasks (AsyncSubAgentMiddleware).
type AsyncSubagents struct{ Agents []AsyncSubAgent }

func (AsyncSubagents) Name() string { return "async_subagents" }
func (AsyncSubagents) StateSchema() graph.Schema {
	return graph.Schema{"async_tasks": channels.BinOp(func(cur, upd any) (any, error) {
		out := map[string]AsyncTask{}
		if c, ok := cur.(map[string]AsyncTask); ok {
			for k, v := range c {
				out[k] = v
			}
		}
		u, ok := upd.(map[string]AsyncTask)
		if !ok {
			return nil, fmt.Errorf("async_tasks: bad update %T", upd)
		}
		for k, v := range u {
			out[k] = v
		}
		return out, nil
	}, func() any { return map[string]AsyncTask{} })}
}

func (a AsyncSubagents) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	var b strings.Builder
	b.WriteString("## Async subagents\nLaunch long-running work with start_async_task and keep going; poll with check_async_task. Available agents:\n")
	for _, s := range a.Agents {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	r := *req
	r.System = strings.TrimSpace(req.System + "\n\n" + b.String())
	return next(ctx, &r)
}

func (a AsyncSubagents) Tools() []tools.Tool {
	byName := map[string]AsyncSubAgent{}
	for _, s := range a.Agents {
		byName[s.Name] = s
	}
	tasks := func(ctx context.Context) map[string]AsyncTask {
		m, _ := prebuilt.StateOf(ctx)["async_tasks"].(map[string]AsyncTask)
		return m
	}
	reply := func(ctx context.Context, name, text string, t *AsyncTask) (any, error) {
		m := messages.Tool(text, tools.CallID(ctx))
		m.Name = name
		upd := graph.State{"messages": []messages.Message{m}}
		if t != nil {
			upd["async_tasks"] = map[string]AsyncTask{t.ID: *t}
		}
		return graph.Command{Update: upd}, nil
	}
	find := func(ctx context.Context, id string) (AsyncTask, AsyncSubAgent, error) {
		t, ok := tasks(ctx)[id]
		if !ok {
			return t, AsyncSubAgent{}, tools.Errorf("unknown task id %q", id)
		}
		return t, byName[t.Agent], nil
	}
	type startArgs struct {
		Description  string `json:"description" description:"Self-contained task for the subagent"`
		SubagentType string `json:"subagent_type"`
	}
	type idArgs struct {
		TaskID string `json:"task_id"`
	}
	type updArgs struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message" description:"Follow-up instructions"`
	}
	type none struct{}
	return []tools.Tool{
		tools.New("start_async_task", "Launch a subagent in the background and return its task_id immediately.",
			func(ctx context.Context, x startArgs) (any, error) {
				s, ok := byName[x.SubagentType]
				if !ok {
					return nil, tools.Errorf("unknown subagent type %q", x.SubagentType)
				}
				h, err := s.Runner.Start(ctx, x.Description)
				if err != nil {
					return nil, tools.Errorf("could not start task: %v", err)
				}
				t := AsyncTask{ID: shortID(), Agent: s.Name, Handle: h, Description: x.Description, Status: AsyncRunning}
				return reply(ctx, "start_async_task", "Started async task "+t.ID+" on "+s.Name, &t)
			}),
		tools.New("check_async_task", "Check a background task; returns its status and, when finished, its result.",
			func(ctx context.Context, x idArgs) (any, error) {
				t, s, err := find(ctx, x.TaskID)
				if err != nil {
					return nil, err
				}
				if t.Status == AsyncRunning {
					st, res, err := s.Runner.Poll(ctx, t.Handle)
					if err != nil {
						return nil, tools.Errorf("poll failed: %v", err)
					}
					t.Status, t.Result = st, res
				}
				text := fmt.Sprintf("Task %s status: %s", t.ID, t.Status)
				if t.Status != AsyncRunning && t.Result != "" {
					text += "\nResult:\n" + t.Result
				}
				return reply(ctx, "check_async_task", text, &t)
			}),
		tools.New("update_async_task", "Send follow-up instructions to a running task.",
			func(ctx context.Context, x updArgs) (any, error) {
				t, s, err := find(ctx, x.TaskID)
				if err != nil {
					return nil, err
				}
				h, err := s.Runner.Send(ctx, t.Handle, x.Message)
				if err != nil {
					return nil, tools.Errorf("update failed: %v", err)
				}
				t.Handle, t.Status = h, AsyncRunning
				return reply(ctx, "update_async_task", "Updated task "+t.ID, &t)
			}),
		tools.New("cancel_async_task", "Cancel a running background task.",
			func(ctx context.Context, x idArgs) (any, error) {
				t, s, err := find(ctx, x.TaskID)
				if err != nil {
					return nil, err
				}
				if err := s.Runner.Cancel(ctx, t.Handle); err != nil {
					return nil, tools.Errorf("cancel failed: %v", err)
				}
				t.Status = AsyncCancelled
				return reply(ctx, "cancel_async_task", "Cancelled task "+t.ID, &t)
			}),
		tools.New("list_async_tasks", "List all background tasks and their last known status.",
			func(ctx context.Context, _ none) (any, error) {
				ts := tasks(ctx)
				ids := make([]string, 0, len(ts))
				for id := range ts {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				var b strings.Builder
				for _, id := range ids {
					fmt.Fprintf(&b, "%s [%s] %s: %s\n", id, ts[id].Status, ts[id].Agent, ts[id].Description)
				}
				if b.Len() == 0 {
					return "No async tasks", nil
				}
				return strings.TrimRight(b.String(), "\n"), nil
			}),
	}
}

func shortID() string { return strings.ReplaceAll(messages.NewID(), "-", "")[:8] }

func init() {
	checkpoint.Register("middleware.AsyncTask", AsyncTask{})
	checkpoint.Register("map[string]middleware.AsyncTask", map[string]AsyncTask{})
	checkpoint.Register("middleware.RubricEvaluation", RubricEvaluation{})
}
