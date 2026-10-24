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
		h.Register(wi.ID, wi.Role, wi.URL)
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
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var j Job
		if err := read(r, &j); err != nil || j.Thread == "" || len(j.Prefer) == 0 {
			reply(w, 400, map[string]string{"error": "thread and prefer are required"})
			return
		}
		reply(w, 200, map[string]string{"id": h.Submit(j)})
	})
	mux.HandleFunc("POST /jobs/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Worker, Output, Error string }
		if err := read(r, &b); err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := h.Complete(b.Worker, r.PathValue("id"), b.Output, b.Error); err != nil {
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
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, h.Snapshot()) })
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
func (c *HubClient) Register(ctx context.Context, id, role, url string) (time.Duration, error) {
	var out struct {
		TTL float64 `json:"ttl_seconds"`
	}
	code, err := c.call(ctx, "POST", "/workers", WorkerInfo{ID: id, Role: role, URL: url}, &out)
	if err == nil && code != 200 {
		err = fmt.Errorf("hub: register: http %d", code)
	}
	return time.Duration(out.TTL * float64(time.Second)), err
}

// Heartbeat renews the lease; ErrUnknownWorker means register again.
func (c *HubClient) Heartbeat(ctx context.Context, id string) error {
	code, err := c.call(ctx, "POST", "/workers/"+id+"/heartbeat", nil, nil)
	if err == nil && code == 404 {
		return ErrUnknownWorker
	}
	return err
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
	code, err := c.call(ctx, "POST", "/jobs/"+job+"/complete", map[string]string{"worker": worker, "output": output, "error": errMsg}, nil)
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
