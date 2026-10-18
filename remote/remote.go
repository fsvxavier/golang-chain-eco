// Package remote is a client for LangGraph Platform / Agent Server
// (langgraph_sdk) and RemoteGraph: a graph that executes on a server but
// looks like a local runnables.Runnable.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Client talks to an Agent Server over HTTP.
type Client struct {
	BaseURL string
	// APIKey is sent as x-api-key (default $LANGGRAPH_API_KEY / $LANGSMITH_API_KEY).
	APIKey  string
	Headers map[string]string
	HTTP    *http.Client
}

// NewClient creates a client for baseURL.
func NewClient(baseURL string) *Client {
	key := os.Getenv("LANGGRAPH_API_KEY")
	if key == "" {
		key = os.Getenv("LANGSMITH_API_KEY")
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: key, HTTP: http.DefaultClient}
}

// APIError is a non-2xx response.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("remote: http %d: %s", e.Status, e.Body) }

func (c *Client) do(ctx context.Context, method, path string, body any, sse bool) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sse {
		req.Header.Set("Accept", "text/event-stream")
	}
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &APIError{resp.StatusCode, strings.TrimSpace(string(b))}
	}
	return resp, nil
}

func (c *Client) json(ctx context.Context, method, path string, body, out any) error {
	resp, err := c.do(ctx, method, path, body, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func seg(s string) string { return url.PathEscape(s) }

// ----------------------------------------------------------------- threads

// CreateThread creates a thread and returns its ID.
func (c *Client) CreateThread(ctx context.Context, metadata map[string]any) (string, error) {
	var t struct {
		ThreadID string `json:"thread_id"`
	}
	err := c.json(ctx, http.MethodPost, "/threads", map[string]any{"metadata": metadata}, &t)
	return t.ThreadID, err
}

// ThreadState is a thread snapshot.
type ThreadState struct {
	Values map[string]any `json:"values"`
	Next   []string       `json:"next"`
	Tasks  []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Interrupts []struct {
			Value any `json:"value"`
		} `json:"interrupts"`
	} `json:"tasks"`
}

// GetState returns the latest state of a thread.
func (c *Client) GetState(ctx context.Context, threadID string) (ThreadState, error) {
	var s ThreadState
	err := c.json(ctx, http.MethodGet, "/threads/"+seg(threadID)+"/state", nil, &s)
	return s, err
}

// UpdateState writes values into a thread.
func (c *Client) UpdateState(ctx context.Context, threadID string, values map[string]any, asNode string) error {
	return c.json(ctx, http.MethodPost, "/threads/"+seg(threadID)+"/state",
		map[string]any{"values": values, "as_node": asNode}, nil)
}

// ----------------------------------------------------------------- runs

// RunRequest is the body of a run creation.
type RunRequest struct {
	AssistantID string
	Input       any
	// Command resumes an interrupted run, e.g. map{"resume": v}.
	Command  map[string]any
	Config   map[string]any
	Metadata map[string]any
	// StreamMode for streaming calls (default ["values"]).
	StreamMode []string
	// Multitask: interrupt, rollback, reject or enqueue (default reject).
	Multitask string
}

func (r RunRequest) body() map[string]any {
	b := map[string]any{"assistant_id": r.AssistantID}
	if r.Input != nil {
		b["input"] = r.Input
	}
	if r.Command != nil {
		b["command"] = r.Command
	}
	if r.Config != nil {
		b["config"] = r.Config
	}
	if r.Metadata != nil {
		b["metadata"] = r.Metadata
	}
	if len(r.StreamMode) > 0 {
		b["stream_mode"] = r.StreamMode
	}
	if r.Multitask != "" {
		b["multitask_strategy"] = r.Multitask
	}
	return b
}

// Run is a server-side run.
type Run struct {
	RunID    string `json:"run_id"`
	ThreadID string `json:"thread_id"`
	// Status: pending, running, error, success, timeout, interrupted.
	Status string `json:"status"`
}

func runsPath(threadID string) string {
	if threadID == "" {
		return "/runs"
	}
	return "/threads/" + seg(threadID) + "/runs"
}

// CreateRun starts a background run (threadID "" = stateless).
func (c *Client) CreateRun(ctx context.Context, threadID string, r RunRequest) (Run, error) {
	var run Run
	err := c.json(ctx, http.MethodPost, runsPath(threadID), r.body(), &run)
	return run, err
}

// GetRun fetches a run.
func (c *Client) GetRun(ctx context.Context, threadID, runID string) (Run, error) {
	var run Run
	err := c.json(ctx, http.MethodGet, runsPath(threadID)+"/"+seg(runID), nil, &run)
	return run, err
}

// CancelRun cancels a run; interrupt=true interrupts instead of rolling back.
func (c *Client) CancelRun(ctx context.Context, threadID, runID string) error {
	return c.json(ctx, http.MethodPost, runsPath(threadID)+"/"+seg(runID)+"/cancel?action=interrupt", nil, nil)
}

// Join waits for a run and returns its final values.
func (c *Client) Join(ctx context.Context, threadID, runID string) (map[string]any, error) {
	var out map[string]any
	err := c.json(ctx, http.MethodGet, runsPath(threadID)+"/"+seg(runID)+"/join", nil, &out)
	return out, err
}

// Wait creates a run and blocks until it finishes, returning the final values.
func (c *Client) Wait(ctx context.Context, threadID string, r RunRequest) (map[string]any, error) {
	var out map[string]any
	err := c.json(ctx, http.MethodPost, runsPath(threadID)+"/wait", r.body(), &out)
	return out, err
}

// StreamPart is one server-sent event.
type StreamPart struct {
	Event string
	Data  json.RawMessage
}

// Stream creates a run and yields its server-sent events.
func (c *Client) Stream(ctx context.Context, threadID string, r RunRequest) iter.Seq2[StreamPart, error] {
	if len(r.StreamMode) == 0 {
		r.StreamMode = []string{"values"}
	}
	return func(yield func(StreamPart, error) bool) {
		resp, err := c.do(ctx, http.MethodPost, runsPath(threadID)+"/stream", r.body(), true)
		if err != nil {
			yield(StreamPart{}, err)
			return
		}
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 32<<20)
		var ev string
		var data []string
		flush := func() bool {
			if ev == "" && len(data) == 0 {
				return true
			}
			p := StreamPart{Event: ev, Data: json.RawMessage(strings.Join(data, "\n"))}
			ev, data = "", nil
			if p.Event == "error" {
				return yield(StreamPart{}, fmt.Errorf("remote: run error: %s", p.Data))
			}
			return yield(p, nil)
		}
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if !flush() {
					return
				}
			case strings.HasPrefix(line, "event:"):
				ev = strings.TrimSpace(line[6:])
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if err := sc.Err(); err != nil {
			yield(StreamPart{}, err)
			return
		}
		flush()
	}
}

// ----------------------------------------------------------------- RemoteGraph

// Graph runs an assistant/graph deployed on an Agent Server. It implements
// runnables.Runnable[any, graph.State]; the thread comes from
// Configurable["thread_id"] like local graphs (stateless when absent).
type Graph struct {
	Client      *Client
	AssistantID string
}

var _ runnables.Runnable[any, graph.State] = (*Graph)(nil)

func (g *Graph) request(ctx context.Context, input any) (string, RunRequest) {
	cfg := runnables.GetConfig(ctx)
	thread, _ := cfg.Configurable["thread_id"].(string)
	req := RunRequest{AssistantID: g.AssistantID}
	switch in := input.(type) {
	case graph.Command:
		req.Command = commandBody(in)
	case *graph.Command:
		req.Command = commandBody(*in)
	default:
		req.Input = input
	}
	conf := map[string]any{}
	for k, v := range cfg.Configurable {
		conf[k] = v
	}
	if len(conf) > 0 || cfg.RecursionLimit > 0 {
		c := map[string]any{"configurable": conf}
		if cfg.RecursionLimit > 0 {
			c["recursion_limit"] = cfg.RecursionLimit
		}
		req.Config = c
	}
	return thread, req
}

func commandBody(c graph.Command) map[string]any {
	m := map[string]any{}
	if c.Resume != nil {
		m["resume"] = c.Resume
	}
	if c.Update != nil {
		m["update"] = c.Update
	}
	if len(c.Goto) == 1 {
		m["goto"] = c.Goto[0]
	} else if len(c.Goto) > 1 {
		m["goto"] = c.Goto
	}
	return m
}

// Invoke runs the remote graph to completion. A paused run reports its
// interrupts under graph.InterruptKey, as local graphs do.
func (g *Graph) Invoke(ctx context.Context, input any) (graph.State, error) {
	thread, req := g.request(ctx, input)
	out, err := g.Client.Wait(ctx, thread, req)
	if err != nil {
		return nil, err
	}
	if thread != "" {
		if st, err := g.Client.GetState(ctx, thread); err == nil {
			var ints []graph.InterruptInfo
			for _, t := range st.Tasks {
				for _, i := range t.Interrupts {
					ints = append(ints, graph.InterruptInfo{Node: t.Name, ID: t.ID, Value: i.Value})
				}
			}
			if len(ints) > 0 {
				if out == nil {
					out = map[string]any{}
				}
				out[graph.InterruptKey] = ints
			}
		}
	}
	return out, nil
}

// Stream yields the state after each step ("values" events).
func (g *Graph) Stream(ctx context.Context, input any) iter.Seq2[graph.State, error] {
	thread, req := g.request(ctx, input)
	return func(yield func(graph.State, error) bool) {
		for p, err := range g.Client.Stream(ctx, thread, req) {
			if err != nil {
				yield(nil, err)
				return
			}
			if p.Event != "values" {
				continue
			}
			var s graph.State
			if err := json.Unmarshal(p.Data, &s); err != nil {
				yield(nil, errors.Join(errors.New("remote: bad values event"), err))
				return
			}
			if !yield(s, nil) {
				return
			}
		}
	}
}

// GetState returns the thread state selected by Configurable["thread_id"].
func (g *Graph) GetState(ctx context.Context) (ThreadState, error) {
	thread, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	if thread == "" {
		return ThreadState{}, errors.New("remote: GetState needs a thread_id")
	}
	return g.Client.GetState(ctx, thread)
}
