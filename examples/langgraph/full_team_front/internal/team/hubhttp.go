package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

const claimWait = 10 * time.Second

// Handler serves the hub over HTTP. mcp, when set, is mounted at /mcp.
func (h *Hub) Handler(mcp http.Handler) http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	read := func(r *http.Request, v any) error {
		return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20)).Decode(v)
	}

	mux.HandleFunc("POST /workers", func(w http.ResponseWriter, r *http.Request) {
		var wi WorkerInfo
		if err := read(r, &wi); err != nil || wi.ID == "" || wi.Role == "" {
			reply(w, 400, map[string]string{"error": "id and role are required"})
			return
		}
		h.RegisterInfo(wi)
		reply(w, 200, map[string]any{"ttl_seconds": h.TTL.Seconds()})
	})
	mux.HandleFunc("POST /workers/{id}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if !h.Heartbeat(r.PathValue("id")) {
			reply(w, 404, map[string]string{"error": ErrUnknownWorker.Error()})
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /workers/{id}/claim", func(w http.ResponseWriter, r *http.Request) {
		j, err := h.Claim(r.Context(), r.PathValue("id"), claimWait)
		switch {
		case errors.Is(err, ErrUnknownWorker):
			reply(w, 404, map[string]string{"error": err.Error()})
		case j == nil:
			w.WriteHeader(http.StatusNoContent)
		default:
			reply(w, 200, j)
		}
	})
	mux.HandleFunc("POST /workers/{id}/activity", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Activity string }
		if err := read(r, &b); err != nil || !h.SetActivity(r.PathValue("id"), head(b.Activity, 120)) {
			reply(w, 404, map[string]string{"error": ErrUnknownWorker.Error()})
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var j Job
		if err := read(r, &j); err != nil || j.Thread == "" || len(j.Prefer) == 0 {
			reply(w, 400, map[string]string{"error": "thread and prefer are required"})
			return
		}
		reply(w, 200, map[string]string{"id": h.Submit(j)})
	})
	mux.HandleFunc("POST /jobs/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Worker, Output, Error string
			Usage                 *messages.Usage
		}
		if err := read(r, &b); err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := h.CompleteUsage(b.Worker, r.PathValue("id"), b.Output, b.Error, b.Usage); err != nil {
			reply(w, 409, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		secs, _ := strconv.Atoi(r.URL.Query().Get("wait"))
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(secs)*time.Second)
		defer cancel()
		j, err := h.Wait(ctx, r.PathValue("id"))
		if errors.Is(err, context.DeadlineExceeded) { // still running: report where it is
			h.mu.Lock()
			if jj := h.jobs[r.PathValue("id")]; jj != nil {
				j, err = *jj, nil
			}
			h.mu.Unlock()
		}
		if err != nil {
			reply(w, 404, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, j)
	})
	// The hub is trusted and loopback-only in this example; the PO and QA guards
	// rely on that, since anyone who can PUT here can grant any request a scope.
	mux.HandleFunc("PUT /scope/{id}", func(w http.ResponseWriter, r *http.Request) {
		var s Scope
		if err := read(r, &s); err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		h.SetScope(r.PathValue("id"), s)
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /scope/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := h.Scope(r.PathValue("id"))
		if !ok {
			reply(w, 404, map[string]string{"error": ErrNoScope.Error()})
			return
		}
		reply(w, 200, s)
	})
	// Same trust as /scope: anyone who can POST here can add calls to any request.
	mux.HandleFunc("POST /calls/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]int{"calls": h.AddCall(r.PathValue("id"))})
	})
	mux.HandleFunc("GET /calls/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]int{"calls": h.Calls(r.PathValue("id"))})
	})
	mux.HandleFunc("GET /thread-scope/{thread}", func(w http.ResponseWriter, r *http.Request) {
		s, id, ok := h.ThreadScope(r.PathValue("thread"))
		if !ok {
			reply(w, 404, map[string]string{"error": ErrNoScope.Error()})
			return
		}
		reply(w, 200, threadScope{s, id})
	})
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, h.Snapshot()) })
	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, h.Jobs()) })
	mux.HandleFunc("POST /usage/{id}", func(w http.ResponseWriter, r *http.Request) {
		var u messages.Usage
		if err := read(r, &u); err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		h.AddMgrUsage(r.PathValue("id"), u)
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /usage", func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, h.MgrUsage()) })
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, map[string]bool{"ok": true}) })
	if mcp != nil {
		mux.Handle("/mcp", mcp)
	}
	return mux
}

// HubClient is what workers and the manager use to talk to the hub.
type HubClient struct {
	Base string
	HTTP *http.Client
}

// NewHubClient returns a client for the hub at base (e.g. http://hub:8000).
func NewHubClient(base string) *HubClient {
	return &HubClient{Base: base, HTTP: &http.Client{Timeout: claimWait + 10*time.Second}}
}

// call does one JSON request and returns the status code. It fails on 5xx and
// transport errors; callers inspect 4xx themselves.
func (c *HubClient) call(ctx context.Context, method, path string, in, out any) (int, error) {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return resp.StatusCode, fmt.Errorf("hub: %s %s: http %d", method, path, resp.StatusCode)
	}
	if out != nil && resp.StatusCode == 200 {
		return 200, json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

// Register announces a worker; it returns the lease TTL.
func (c *HubClient) Register(ctx context.Context, id, role, url, model string) (time.Duration, error) {
	return c.RegisterInfo(ctx, WorkerInfo{ID: id, Role: role, URL: url, Model: model})
}

// RegisterInfo is Register for a worker that also reports its skill and references.
func (c *HubClient) RegisterInfo(ctx context.Context, wi WorkerInfo) (time.Duration, error) {
	var out struct {
		TTL float64 `json:"ttl_seconds"`
	}
	code, err := c.call(ctx, "POST", "/workers", wi, &out)
	if err == nil && code != 200 {
		err = fmt.Errorf("hub: register: http %d", code)
	}
	return time.Duration(out.TTL * float64(time.Second)), err
}

// SetActivity tells the hub what the worker is doing now. Best effort: it is
// only shown on the dashboard.
func (c *HubClient) SetActivity(ctx context.Context, id, activity string) {
	_, _ = c.call(ctx, "POST", "/workers/"+id+"/activity", map[string]string{"activity": activity}, nil)
}

// Heartbeat renews the lease; ErrUnknownWorker means register again.
func (c *HubClient) Heartbeat(ctx context.Context, id string) error {
	_, err := c.HeartbeatCancel(ctx, id)
	return err
}

// HeartbeatCancel is Heartbeat that also returns the id of a job of this
// worker that was cancelled since the last heartbeat ("" when none).
func (c *HubClient) HeartbeatCancel(ctx context.Context, id string) (string, error) {
	var out struct{ Cancel string }
	code, err := c.call(ctx, "POST", "/workers/"+id+"/heartbeat", nil, &out)
	if err == nil && code == 404 {
		return "", ErrUnknownWorker
	}
	return out.Cancel, err
}

// CancelScope cancels the work of request id and returns how many jobs it ended.
func (c *HubClient) CancelScope(ctx context.Context, id string) (int, error) {
	var out struct{ Cancelled int }
	code, err := c.call(ctx, "POST", "/cancel/"+url.PathEscape(id), nil, &out)
	if err == nil && code != 200 {
		err = fmt.Errorf("hub: cancel: http %d", code)
	}
	return out.Cancelled, err
}

// Claim waits for the job reserved for this worker; nil means none yet.
func (c *HubClient) Claim(ctx context.Context, id string) (*Job, error) {
	var j Job
	code, err := c.call(ctx, "POST", "/workers/"+id+"/claim", nil, &j)
	switch {
	case err != nil:
		return nil, err
	case code == 404:
		return nil, ErrUnknownWorker
	case code == 200:
		return &j, nil
	}
	return nil, nil
}

// Complete reports the result of a job.
func (c *HubClient) Complete(ctx context.Context, worker, job, output, errMsg string) error {
	return c.CompleteUsage(ctx, worker, job, output, errMsg, nil)
}

// CompleteUsage is Complete that also reports the tokens the job consumed.
func (c *HubClient) CompleteUsage(ctx context.Context, worker, job, output, errMsg string, u *messages.Usage) error {
	code, err := c.call(ctx, "POST", "/jobs/"+job+"/complete", map[string]any{"worker": worker, "output": output, "error": errMsg, "usage": u}, nil)
	if err == nil && code == 409 {
		return ErrNotOwner
	}
	return err
}

// Run submits a job and waits for it. It is the manager's only way to use an agent.
func (c *HubClient) Run(ctx context.Context, j Job) (Job, error) {
	var sub struct{ ID string }
	if code, err := c.call(ctx, "POST", "/jobs", j, &sub); err != nil || code != 200 {
		return Job{}, fmt.Errorf("hub: submit: http %d: %v", code, err)
	}
	for {
		var out Job
		code, err := c.call(ctx, "GET", "/jobs/"+sub.ID+"?wait=20", nil, &out)
		if err != nil || code != 200 {
			if ctx.Err() != nil {
				return Job{}, ctx.Err()
			}
			return Job{}, fmt.Errorf("hub: wait %s: http %d: %v", sub.ID, code, err)
		}
		switch out.Status {
		case "done":
			return out, nil
		case "failed":
			return out, fmt.Errorf("job %s failed: %s", out.ID, out.Error)
		}
	}
}

// State returns the registry snapshot.
func (c *HubClient) State(ctx context.Context) (State, error) {
	var s State
	_, err := c.call(ctx, "GET", "/state", nil, &s)
	return s, err
}

// AddUsage reports tokens the manager's agent used for request id.
func (c *HubClient) AddUsage(ctx context.Context, id string, u messages.Usage) error {
	_, err := c.call(ctx, "POST", "/usage/"+url.PathEscape(id), u, nil)
	return err
}

// MgrUsage returns the manager's tokens per request id.
func (c *HubClient) MgrUsage(ctx context.Context) (map[string]messages.Usage, error) {
	var m map[string]messages.Usage
	_, err := c.call(ctx, "GET", "/usage", nil, &m)
	return m, err
}

// Jobs returns every job without prompt and output.
func (c *HubClient) Jobs(ctx context.Context) ([]Job, error) {
	var js []Job
	_, err := c.call(ctx, "GET", "/jobs", nil, &js)
	return js, err
}

// SetScope stores the scope of request id.
func (c *HubClient) SetScope(ctx context.Context, id string, s Scope) error {
	code, err := c.call(ctx, "PUT", "/scope/"+url.PathEscape(id), s, nil)
	if err == nil && code != 200 {
		err = fmt.Errorf("hub: set scope: http %d", code)
	}
	return err
}

// Scope returns the scope of request id, or ErrNoScope (and the zero Scope)
// when none was stored. Callers treat that as no access.
func (c *HubClient) Scope(ctx context.Context, id string) (Scope, error) {
	var s Scope
	code, err := c.call(ctx, "GET", "/scope/"+url.PathEscape(id), nil, &s)
	switch {
	case err != nil:
		return Scope{}, err
	case code == 404:
		return Scope{}, ErrNoScope
	case code != 200:
		return Scope{}, fmt.Errorf("hub: scope: http %d", code)
	}
	return s, nil
}

// AddCall counts one backend call for request id.
func (c *HubClient) AddCall(ctx context.Context, id string) error {
	code, err := c.call(ctx, "POST", "/calls/"+url.PathEscape(id), nil, nil)
	if err == nil && code != 200 {
		err = fmt.Errorf("hub: add call: http %d", code)
	}
	return err
}

// Calls returns how many backend calls were counted for request id.
func (c *HubClient) Calls(ctx context.Context, id string) (int, error) {
	var r struct {
		Calls int `json:"calls"`
	}
	code, err := c.call(ctx, "GET", "/calls/"+url.PathEscape(id), nil, &r)
	if err == nil && code != 200 {
		err = fmt.Errorf("hub: calls: http %d", code)
	}
	return r.Calls, err
}

// threadScope is the /thread-scope reply: the scope and the id of the request
// that owns the thread's running job.
type threadScope struct {
	Scope
	Request string `json:"request"`
}

// ThreadScope returns the scope of the request that owns the running job on
// exactly this thread, or ErrNoScope (and the zero Scope) for any other thread.
func (c *HubClient) ThreadScope(ctx context.Context, thread string) (Scope, error) {
	_, s, err := c.ThreadRequest(ctx, thread)
	return s, err
}

// ThreadRequest is ThreadScope plus the owning request's id.
func (c *HubClient) ThreadRequest(ctx context.Context, thread string) (string, Scope, error) {
	var ts threadScope
	code, err := c.call(ctx, "GET", "/thread-scope/"+url.PathEscape(thread), nil, &ts)
	switch {
	case err != nil:
		return "", Scope{}, err
	case code == 404:
		return "", Scope{}, ErrNoScope
	case code != 200:
		return "", Scope{}, fmt.Errorf("hub: thread scope: http %d", code)
	}
	return ts.Request, ts.Scope, nil
}
