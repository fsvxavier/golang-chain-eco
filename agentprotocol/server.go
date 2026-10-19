// Package agentprotocol implements Agent Protocol
// (github.com/langchain-ai/agent-protocol, API v0.1.6) on top of compiled
// graphs: the REST API (agents, threads, runs, store) plus the thread-centric
// Agent Streaming Protocol (channels, namespaces, replay) over SSE and
// WebSocket. A graph served here can be driven by any Agent Protocol client,
// including this package's Client and LangGraph's SDKs.
package agentprotocol

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// AgentSpec registers a graph as an agent.
type AgentSpec struct {
	ID          string
	Name        string
	Description string
	Metadata    map[string]any
	Graph       *graph.Compiled
	// Schemas are optional JSON Schemas; sensible chat defaults are served
	// when nil.
	InputSchema, OutputSchema, StateSchema, ConfigSchema map[string]any
}

// Options configures a Server.
type Options struct {
	Agents []AgentSpec
	// Store backs /store/*; default is an in-memory store.
	Store store.Store
	// RunStore keeps the durable record of runs, so finished runs survive a
	// restart. Default is in memory (lost on restart). Event buffers are always
	// in memory.
	RunStore RunStore
	// APIKey, when set, is required as "x-api-key" or "Authorization: Bearer".
	APIKey string
	// ReplayBuffer is how many streaming events are kept per thread for
	// reconnection (default 2000).
	ReplayBuffer int
	// HTTP is used for run webhooks.
	HTTP *http.Client
}

// Server is an http.Handler implementing the protocol.
type Server struct {
	opts   Options
	agents map[string]*AgentSpec
	order  []string
	mux    *http.ServeMux

	mu      sync.Mutex
	threads map[string]*threadRec
	runs    map[string]*runRec
}

type threadRec struct {
	id       string
	created  time.Time
	updated  time.Time
	metadata map[string]any
	agentID  string
	status   string
	active   *runRec
	queue    []queuedRun // inputs received while a run was active
	hub      *hub
	cpTimes  map[string]time.Time
	// memory carries streaming state across the thread's runs.
	memory *streamMemory
	// subgraphs holds the namespaces streamed by the thread's runs, by key.
	subgraphs map[string][]string
}

// noteSubgraph records a namespace seen in a run and its ancestors (caller
// holds Server.mu).
func (th *threadRec) noteSubgraph(ns []string) {
	if th.subgraphs == nil {
		th.subgraphs = map[string][]string{}
	}
	for i := 1; i <= len(ns); i++ {
		th.subgraphs[nsKey(ns[:i])] = ns[:i]
	}
}

type queuedRun struct {
	body  RunCreate
	input graph.State
	cmd   *graph.Command
}

type runRec struct {
	Run
	threadID  string
	cancel    context.CancelFunc
	done      chan struct{}
	values    graph.State
	ephemeral bool
	// rollbackTo is the checkpoint before the run, for cancel(action=rollback).
	rollbackTo string
	// events is the per-run record replayed by GET /runs/{id}/stream.
	evMu     sync.Mutex
	cond     *sync.Cond
	events   []runEvent
	finished bool
}

type runEvent struct {
	Event string
	Data  any
}

// New builds a server. At least one agent is required.
func New(o Options) (*Server, error) {
	if len(o.Agents) == 0 {
		return nil, errors.New("agentprotocol: at least one agent is required")
	}
	if o.Store == nil {
		o.Store = store.NewMemory()
	}
	if o.RunStore == nil {
		o.RunStore = NewMemoryRunStore()
	}
	if o.ReplayBuffer <= 0 {
		o.ReplayBuffer = 2000
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	s := &Server{opts: o, agents: map[string]*AgentSpec{}, threads: map[string]*threadRec{}, runs: map[string]*runRec{}}
	for i := range o.Agents {
		a := o.Agents[i]
		if a.ID == "" || a.Graph == nil {
			return nil, errors.New("agentprotocol: every agent needs an ID and a Graph")
		}
		if _, dup := s.agents[a.ID]; dup {
			return nil, fmt.Errorf("agentprotocol: duplicate agent %q", a.ID)
		}
		if a.Name == "" {
			a.Name = a.ID
		}
		s.agents[a.ID] = &a
		s.order = append(s.order, a.ID)
	}
	if err := recoverRuns(context.Background(), o.RunStore); err != nil {
		return nil, fmt.Errorf("agentprotocol: recover runs: %w", err)
	}
	s.mux = http.NewServeMux()
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("POST /agents/search", s.searchAgents)
	m.HandleFunc("GET /agents/{agent_id}", s.getAgent)
	m.HandleFunc("GET /agents/{agent_id}/schemas", s.getAgentSchemas)

	m.HandleFunc("POST /threads", s.createThread)
	m.HandleFunc("POST /threads/search", s.searchThreads)
	m.HandleFunc("GET /threads/{thread_id}", s.getThread)
	m.HandleFunc("DELETE /threads/{thread_id}", s.deleteThread)
	m.HandleFunc("PATCH /threads/{thread_id}", s.patchThread)
	m.HandleFunc("GET /threads/{thread_id}/history", s.threadHistory)
	m.HandleFunc("POST /threads/{thread_id}/copy", s.copyThread)

	m.HandleFunc("POST /runs", s.createRun)
	m.HandleFunc("POST /runs/wait", s.createAndWait)
	m.HandleFunc("POST /runs/stream", s.createAndStream)
	m.HandleFunc("POST /runs/search", s.searchRuns)
	m.HandleFunc("GET /runs/{run_id}", s.getRun)
	m.HandleFunc("DELETE /runs/{run_id}", s.deleteRun)
	m.HandleFunc("GET /runs/{run_id}/wait", s.waitRun)
	m.HandleFunc("GET /runs/{run_id}/stream", s.streamRun)
	m.HandleFunc("POST /runs/{run_id}/cancel", s.cancelRun)

	m.HandleFunc("POST /threads/{thread_id}/stream", s.threadSSE)
	m.HandleFunc("GET /threads/{thread_id}/stream", s.threadWS)
	m.HandleFunc("POST /threads/{thread_id}/commands", s.threadCommand)

	m.HandleFunc("PUT /store/items", s.storePut)
	m.HandleFunc("DELETE /store/items", s.storeDelete)
	m.HandleFunc("GET /store/items", s.storeGet)
	m.HandleFunc("POST /store/items/search", s.storeSearch)
	m.HandleFunc("POST /store/namespaces", s.storeNamespaces)

	m.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.opts.APIKey != "" && r.URL.Path != "/ok" && !s.authorized(r) {
		writeErr(w, 401, "unauthorized", "missing or invalid API key")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	key := r.Header.Get("x-api-key")
	if key == "" {
		key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(s.opts.APIKey)) == 1
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, ErrorResponse{Code: errCode, Message: msg})
}

func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// ------------------------------------------------------------------ agents

func (s *Server) agentInfo(a *AgentSpec) Agent {
	return Agent{AgentID: a.ID, Name: a.Name, Description: a.Description, Metadata: a.Metadata,
		Capabilities: Capabilities{Messages: true, Streaming: true}}
}

func matchMeta(have, want map[string]any) bool {
	for k, v := range want {
		if fmt.Sprint(have[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

func page[T any](items []T, limit, offset int) []T {
	if limit <= 0 {
		limit = 10
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := min(offset+limit, len(items))
	return items[offset:end]
}

func (s *Server) searchAgents(w http.ResponseWriter, r *http.Request) {
	var req SearchAgentsRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	out := []Agent{}
	for _, id := range s.order {
		a := s.agents[id]
		if req.Name != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(req.Name)) {
			continue
		}
		if !matchMeta(a.Metadata, req.Metadata) {
			continue
		}
		out = append(out, s.agentInfo(a))
	}
	writeJSON(w, 200, page(out, req.Limit, req.Offset))
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agents[r.PathValue("agent_id")]
	if a == nil {
		writeErr(w, 404, "not_found", "agent not found")
		return
	}
	writeJSON(w, 200, s.agentInfo(a))
}

func orSchema(v, dflt map[string]any) map[string]any {
	if v != nil {
		return v
	}
	return dflt
}

func (s *Server) getAgentSchemas(w http.ResponseWriter, r *http.Request) {
	a := s.agents[r.PathValue("agent_id")]
	if a == nil {
		writeErr(w, 404, "not_found", "agent not found")
		return
	}
	msg := jsonschema.For[Message]()
	msg.Title = ""
	msgs := map[string]any{"type": "array", "items": msg.Map()}
	chatIn := map[string]any{"type": "object", "properties": map[string]any{"messages": msgs}}
	writeJSON(w, 200, AgentSchema{
		AgentID:      a.ID,
		InputSchema:  orSchema(a.InputSchema, chatIn),
		OutputSchema: orSchema(a.OutputSchema, chatIn),
		StateSchema:  orSchema(a.StateSchema, chatIn),
		ConfigSchema: orSchema(a.ConfigSchema, map[string]any{"type": "object", "properties": map[string]any{
			"tags":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"recursion_limit": map[string]any{"type": "integer"}, "configurable": map[string]any{"type": "object"}}}),
	})
}

// defaultAgent resolves the agent for a request: explicit id, the thread's
// agent, or the only registered agent.
func (s *Server) defaultAgent(id string, th *threadRec) (*AgentSpec, error) {
	if id != "" {
		if a := s.agents[id]; a != nil {
			return a, nil
		}
		return nil, fmt.Errorf("agent %q not found", id)
	}
	if th != nil && th.agentID != "" {
		return s.agents[th.agentID], nil
	}
	if len(s.order) == 1 {
		return s.agents[s.order[0]], nil
	}
	return nil, errors.New("agent_id is required: several agents are registered")
}

func newID() string { return messages.NewID() }
