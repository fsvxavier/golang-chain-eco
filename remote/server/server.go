// Package server is a small Agent Server: it serves compiled graphs over the
// LangGraph Platform HTTP API (threads and runs), so remote.Client,
// remote.Graph and async subagents can talk to graphs written in Go. It
// implements the endpoints those clients use; it is not the full platform.
//
//	srv := server.New(map[string]*graph.Compiled{"researcher": g})
//	http.ListenAndServe(":2024", srv)
//
// Graphs that should keep state per thread must be compiled with a
// checkpointer.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Server implements http.Handler.
type Server struct {
	graphs map[string]*graph.Compiled
	// APIKey, if set, is required in the x-api-key header.
	APIKey string

	mu      sync.Mutex
	threads map[string]*thread
	runs    map[string]*run
}

type thread struct{ active string }

type run struct {
	ID, ThreadID, Assistant string
	Status                  string // pending, running, success, error, interrupted
	Err                     string
	cancel                  context.CancelFunc
	done                    chan struct{}
	values                  graph.State
}

// New serves the given graphs, keyed by assistant ID.
func New(graphs map[string]*graph.Compiled) *Server {
	return &Server{graphs: graphs, threads: map[string]*thread{}, runs: map[string]*run{}}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"detail": msg})
}

type runBody struct {
	AssistantID string         `json:"assistant_id"`
	Input       map[string]any `json:"input"`
	Command     map[string]any `json:"command"`
	Config      map[string]any `json:"config"`
	StreamMode  []string       `json:"stream_mode"`
	Multitask   string         `json:"multitask_strategy"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.APIKey != "" && r.Header.Get("x-api-key") != s.APIKey {
		fail(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && len(p) == 1 && p[0] == "ok":
		writeJSON(w, 200, map[string]bool{"ok": true})
	case r.Method == http.MethodPost && len(p) == 1 && p[0] == "threads":
		id := messages.NewID()
		s.mu.Lock()
		s.threads[id] = &thread{}
		s.mu.Unlock()
		writeJSON(w, 200, map[string]string{"thread_id": id})
	case len(p) == 3 && p[0] == "threads" && p[2] == "state" && r.Method == http.MethodGet:
		s.state(w, r, p[1])
	case len(p) >= 2 && p[len(p)-1] == "wait" && p[len(p)-2] == "runs" && r.Method == http.MethodPost:
		s.wait(w, r, threadOf(p))
	case len(p) >= 2 && p[len(p)-1] == "stream" && p[len(p)-2] == "runs" && r.Method == http.MethodPost:
		s.stream(w, r, threadOf(p))
	case len(p) >= 1 && p[len(p)-1] == "runs" && r.Method == http.MethodPost:
		s.create(w, r, threadOf(p))
	case len(p) == 4 && p[0] == "threads" && p[2] == "runs" && r.Method == http.MethodGet:
		s.getRun(w, p[3])
	case len(p) == 5 && p[0] == "threads" && p[2] == "runs" && p[4] == "join" && r.Method == http.MethodGet:
		s.join(w, r, p[3])
	case len(p) == 5 && p[0] == "threads" && p[2] == "runs" && p[4] == "cancel" && r.Method == http.MethodPost:
		s.cancel(w, p[3])
	default:
		fail(w, http.StatusNotFound, "not found")
	}
}

func threadOf(p []string) string {
	if len(p) >= 3 && p[0] == "threads" {
		return p[1]
	}
	return ""
}

func decode(r *http.Request) (runBody, error) {
	var b runBody
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(raw, &b)
}

func (s *Server) start(ctx context.Context, threadID string, b runBody) (*run, error) {
	g := s.graphs[b.AssistantID]
	if g == nil {
		return nil, fmt.Errorf("unknown assistant %q", b.AssistantID)
	}
	s.mu.Lock()
	if threadID != "" {
		th := s.threads[threadID]
		if th == nil {
			th = &thread{}
			s.threads[threadID] = th
		}
		if prev := s.runs[th.active]; prev != nil && (prev.Status == "pending" || prev.Status == "running") {
			switch b.Multitask {
			case "interrupt", "rollback":
				prev.cancel()
			default:
				s.mu.Unlock()
				return nil, errors.New("thread already has an active run (set multitask_strategy)")
			}
		}
	}
	rctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	ru := &run{ID: messages.NewID(), ThreadID: threadID, Assistant: b.AssistantID, Status: "pending", cancel: cancel, done: make(chan struct{})}
	s.runs[ru.ID] = ru
	if threadID != "" {
		s.threads[threadID].active = ru.ID
	}
	s.mu.Unlock()

	cfg := runnables.Config{Configurable: map[string]any{}}
	if c, ok := b.Config["configurable"].(map[string]any); ok {
		for k, v := range c {
			cfg.Configurable[k] = v
		}
	}
	if n, ok := b.Config["recursion_limit"].(float64); ok {
		cfg.RecursionLimit = int(n)
	}
	if threadID != "" {
		cfg.Configurable["thread_id"] = threadID
	} else if g != nil {
		delete(cfg.Configurable, "thread_id")
	}
	rctx = runnables.WithConfig(rctx, cfg)

	var input any
	switch {
	case b.Command != nil:
		cmd := graph.Command{Resume: b.Command["resume"]}
		if u, ok := b.Command["update"].(map[string]any); ok {
			cmd.Update = u
		}
		input = cmd
	case b.Input != nil:
		input = b.Input
	}
	go func() {
		defer close(ru.done)
		s.setStatus(ru, "running", "")
		out, err := g.Invoke(rctx, input)
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case rctx.Err() != nil:
			ru.Status = "interrupted"
		case err != nil:
			ru.Status, ru.Err = "error", err.Error()
		case len(graph.Interrupts(out)) > 0:
			ru.Status, ru.values = "interrupted", out
		default:
			ru.Status, ru.values = "success", out
		}
	}()
	return ru, nil
}

func (s *Server) setStatus(r *run, st, msg string) {
	s.mu.Lock()
	r.Status, r.Err = st, msg
	s.mu.Unlock()
}

func (s *Server) view(r *run) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"run_id": r.ID, "thread_id": r.ThreadID, "assistant_id": r.Assistant, "status": r.Status, "error": r.Err}
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, threadID string) {
	b, err := decode(r)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	ru, err := s.start(r.Context(), threadID, b)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, s.view(ru))
}

func (s *Server) results(ru *run) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ru.values == nil {
		return map[string]any{}
	}
	return ru.values
}

func (s *Server) wait(w http.ResponseWriter, r *http.Request, threadID string) {
	b, err := decode(r)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	ru, err := s.start(r.Context(), threadID, b)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	select {
	case <-ru.done:
	case <-r.Context().Done():
		ru.cancel()
		return
	}
	if ru.Status == "error" {
		fail(w, 500, ru.Err)
		return
	}
	writeJSON(w, 200, s.results(ru))
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request, threadID string) {
	b, err := decode(r)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	ru, err := s.start(r.Context(), threadID, b)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	send := func(ev string, v any) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev, raw)
		if fl != nil {
			fl.Flush()
		}
	}
	send("metadata", map[string]string{"run_id": ru.ID})
	select {
	case <-ru.done:
	case <-r.Context().Done():
		ru.cancel()
		return
	}
	if ru.Status == "error" {
		send("error", map[string]string{"error": "RunError", "message": ru.Err})
		return
	}
	send("values", s.results(ru))
}

func (s *Server) getRun(w http.ResponseWriter, id string) {
	s.mu.Lock()
	ru := s.runs[id]
	s.mu.Unlock()
	if ru == nil {
		fail(w, 404, "run not found")
		return
	}
	writeJSON(w, 200, s.view(ru))
}

func (s *Server) join(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	ru := s.runs[id]
	s.mu.Unlock()
	if ru == nil {
		fail(w, 404, "run not found")
		return
	}
	select {
	case <-ru.done:
	case <-r.Context().Done():
		return
	case <-time.After(10 * time.Minute):
		fail(w, 504, "timeout")
		return
	}
	writeJSON(w, 200, s.results(ru))
}

func (s *Server) cancel(w http.ResponseWriter, id string) {
	s.mu.Lock()
	ru := s.runs[id]
	s.mu.Unlock()
	if ru == nil {
		fail(w, 404, "run not found")
		return
	}
	ru.cancel()
	<-ru.done
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request, threadID string) {
	s.mu.Lock()
	th := s.threads[threadID]
	var ru *run
	if th != nil {
		ru = s.runs[th.active]
	}
	s.mu.Unlock()
	if th == nil || ru == nil {
		fail(w, 404, "thread not found")
		return
	}
	g := s.graphs[ru.Assistant]
	ctx := runnables.WithConfig(r.Context(), graph.Thread(threadID))
	snap, err := g.GetState(ctx)
	if err != nil { // graph without checkpointer: fall back to the last run output
		writeJSON(w, 200, map[string]any{"values": s.results(ru), "next": []string{}, "tasks": []any{}})
		return
	}
	tasks := make([]map[string]any, 0, len(snap.Interrupts))
	for _, i := range snap.Interrupts {
		tasks = append(tasks, map[string]any{"id": i.ID, "name": i.Node, "interrupts": []map[string]any{{"value": i.Value}}})
	}
	writeJSON(w, 200, map[string]any{"values": snap.Values, "next": snap.Next, "tasks": tasks, "checkpoint_id": snap.CheckpointID})
}
