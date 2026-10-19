package agentprotocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client talks to any Agent Protocol server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient returns a client for baseURL (e.g. "http://localhost:8123").
func NewClient(baseURL, apiKey string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, HTTP: http.DefaultClient}
}

// StatusError is a non-2xx response.
type StatusError struct {
	Status int
	ErrorResponse
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("agentprotocol: HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

func (c *Client) req(ctx context.Context, method, path string, q url.Values, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		se := &StatusError{Status: resp.StatusCode}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var body struct {
			Code    string `json:"code"`
			Err     string `json:"error"` // streaming-protocol errors use "error"
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &body) != nil || body.Message == "" {
			se.Message = strings.TrimSpace(string(b))
		} else {
			se.Code, se.Message = body.Code, body.Message
			if se.Code == "" {
				se.Code = body.Err
			}
		}
		return nil, se
	}
	return resp, nil
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	resp, err := c.req(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ----- agents

func (c *Client) SearchAgents(ctx context.Context, r SearchAgentsRequest) (out []Agent, err error) {
	return out, c.do(ctx, "POST", "/agents/search", nil, r, &out)
}

func (c *Client) GetAgent(ctx context.Context, id string) (a Agent, err error) {
	return a, c.do(ctx, "GET", "/agents/"+url.PathEscape(id), nil, nil, &a)
}

func (c *Client) GetAgentSchemas(ctx context.Context, id string) (a AgentSchema, err error) {
	return a, c.do(ctx, "GET", "/agents/"+url.PathEscape(id)+"/schemas", nil, nil, &a)
}

// ----- threads

func (c *Client) CreateThread(ctx context.Context, r ThreadCreate) (t Thread, err error) {
	return t, c.do(ctx, "POST", "/threads", nil, r, &t)
}

func (c *Client) GetThread(ctx context.Context, id string) (t Thread, err error) {
	return t, c.do(ctx, "GET", "/threads/"+url.PathEscape(id), nil, nil, &t)
}

func (c *Client) SearchThreads(ctx context.Context, r ThreadSearchRequest) (out []Thread, err error) {
	return out, c.do(ctx, "POST", "/threads/search", nil, r, &out)
}

func (c *Client) PatchThread(ctx context.Context, id string, p ThreadPatch) (t Thread, err error) {
	return t, c.do(ctx, "PATCH", "/threads/"+url.PathEscape(id), nil, p, &t)
}

func (c *Client) DeleteThread(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/threads/"+url.PathEscape(id), nil, nil, nil)
}

func (c *Client) CopyThread(ctx context.Context, id string) (t Thread, err error) {
	return t, c.do(ctx, "POST", "/threads/"+url.PathEscape(id)+"/copy", nil, nil, &t)
}

// ThreadHistory lists a thread's state revisions, newest first.
func (c *Client) ThreadHistory(ctx context.Context, id string, limit int, before string) (out []ThreadState, err error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if before != "" {
		q.Set("before", before)
	}
	return out, c.do(ctx, "GET", "/threads/"+url.PathEscape(id)+"/history", q, nil, &out)
}

// ----- runs

func (c *Client) CreateRun(ctx context.Context, r RunCreate) (run Run, err error) {
	return run, c.do(ctx, "POST", "/runs", nil, r, &run)
}

func (c *Client) CreateAndWait(ctx context.Context, r RunCreate) (out RunWaitResponse, err error) {
	return out, c.do(ctx, "POST", "/runs/wait", nil, r, &out)
}

func (c *Client) WaitRun(ctx context.Context, id string) (out RunWaitResponse, err error) {
	return out, c.do(ctx, "GET", "/runs/"+url.PathEscape(id)+"/wait", nil, nil, &out)
}

func (c *Client) GetRun(ctx context.Context, id string) (run Run, err error) {
	return run, c.do(ctx, "GET", "/runs/"+url.PathEscape(id), nil, nil, &run)
}

func (c *Client) SearchRuns(ctx context.Context, r RunSearchRequest) (out []Run, err error) {
	return out, c.do(ctx, "POST", "/runs/search", nil, r, &out)
}

func (c *Client) DeleteRun(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/runs/"+url.PathEscape(id), nil, nil, nil)
}

// CancelRun cancels with action "interrupt" (default) or "rollback".
func (c *Client) CancelRun(ctx context.Context, id, action string, wait bool) error {
	q := url.Values{}
	if action != "" {
		q.Set("action", action)
	}
	if wait {
		q.Set("wait", "true")
	}
	return c.do(ctx, "POST", "/runs/"+url.PathEscape(id)+"/cancel", q, nil, nil)
}

// SSEEvent is one Server-Sent Event.
type SSEEvent struct {
	ID    string
	Event string
	Data  json.RawMessage
}

// sse parses an event stream until EOF or ctx ends.
func sse(ctx context.Context, resp *http.Response) iter.Seq2[SSEEvent, error] {
	return func(yield func(SSEEvent, error) bool) {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		var ev SSEEvent
		var data []string
		flush := func() bool {
			if ev.Event == "" && len(data) == 0 {
				return true
			}
			ev.Data = json.RawMessage(strings.Join(data, "\n"))
			ok := yield(ev, nil)
			ev, data = SSEEvent{}, nil
			return ok
		}
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if !flush() {
					return
				}
			case strings.HasPrefix(line, ":"):
			case strings.HasPrefix(line, "id:"):
				ev.ID = strings.TrimSpace(line[3:])
			case strings.HasPrefix(line, "event:"):
				ev.Event = strings.TrimSpace(line[6:])
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			yield(SSEEvent{}, err)
		}
	}
}

// StreamRun creates a run and streams its events (POST /runs/stream).
func (c *Client) StreamRun(ctx context.Context, r RunCreate) iter.Seq2[SSEEvent, error] {
	return func(yield func(SSEEvent, error) bool) {
		resp, err := c.req(ctx, "POST", "/runs/stream", nil, r)
		if err != nil {
			yield(SSEEvent{}, err)
			return
		}
		for ev, err := range sse(ctx, resp) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

// Events opens a filtered streaming-protocol event stream on a thread.
func (c *Client) Events(ctx context.Context, threadID string, f Filter) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		resp, err := c.req(ctx, "POST", "/threads/"+url.PathEscape(threadID)+"/stream", nil, f)
		if err != nil {
			yield(Event{}, err)
			return
		}
		for raw, err := range sse(ctx, resp) {
			if err != nil {
				yield(Event{}, err)
				return
			}
			var e Event
			if err := json.Unmarshal(raw.Data, &e); err != nil {
				yield(Event{}, err)
				return
			}
			if !yield(e, nil) {
				return
			}
		}
	}
}

// CommandResult is the decoded response of a streaming command.
type CommandResult struct {
	Type    string          `json:"type"`
	ID      *uint64         `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
	Message string          `json:"message"`
	Meta    map[string]any  `json:"meta"`
}

// Command sends a streaming-protocol command over HTTP. A protocol error
// comes back as a StatusError carrying the error code.
func (c *Client) Command(ctx context.Context, threadID, method string, params any) (CommandResult, error) {
	var out CommandResult
	id := uint64(1)
	err := c.do(ctx, "POST", "/threads/"+url.PathEscape(threadID)+"/commands", nil,
		map[string]any{"id": id, "method": method, "params": params}, &out)
	var se *StatusError
	if asStatus(err, &se) {
		// the body of a failed command is a protocol error, not an ErrorResponse
		return CommandResult{Type: "error", Error: se.Code, Message: se.Message}, err
	}
	return out, err
}

func asStatus(err error, target **StatusError) bool {
	se, ok := err.(*StatusError)
	if ok {
		*target = se
	}
	return ok
}

// ----- store

func (c *Client) PutItem(ctx context.Context, ns []string, key string, value map[string]any) error {
	return c.do(ctx, "PUT", "/store/items", nil, StorePutRequest{Namespace: ns, Key: key, Value: value}, nil)
}

func (c *Client) GetItem(ctx context.Context, ns []string, key string) (it Item, err error) {
	q := url.Values{"key": {key}}
	for _, n := range ns {
		q.Add("namespace", n)
	}
	return it, c.do(ctx, "GET", "/store/items", q, nil, &it)
}

func (c *Client) DeleteItem(ctx context.Context, ns []string, key string) error {
	return c.do(ctx, "DELETE", "/store/items", nil, StoreDeleteRequest{Namespace: ns, Key: key}, nil)
}

func (c *Client) SearchItems(ctx context.Context, r StoreSearchRequest) ([]Item, error) {
	var out SearchItemsResponse
	return out.Items, c.do(ctx, "POST", "/store/items/search", nil, r, &out)
}

func (c *Client) ListNamespaces(ctx context.Context, r StoreListNamespacesRequest) (out [][]string, err error) {
	return out, c.do(ctx, "POST", "/store/namespaces", nil, r, &out)
}
