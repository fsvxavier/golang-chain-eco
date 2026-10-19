// Package server is a Model Context Protocol server: it exposes tools.Tool
// values (and prompts) to any MCP client such as VS Code, Cursor or Claude
// Desktop, over stdio or HTTP. It is the counterpart of the mcp client
// package; the same tools an agent calls in-process can be served to editors.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

const protocolVersion = "2025-06-18"

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// PromptArg describes one argument of a prompt.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one message of a rendered prompt.
type PromptMessage struct {
	Role string // "user" or "assistant"
	Text string
}

// Prompt is a reusable workflow template shown to the user by the client.
type Prompt struct {
	Name        string
	Description string
	Arguments   []PromptArg
	Get         func(ctx context.Context, args map[string]string) ([]PromptMessage, error)
}

// CallHook observes every tool call (name, arguments, duration, error); use
// it for metrics.
type CallHook func(name string, args map[string]any, d time.Duration, err error)

// Server serves tools and prompts.
type Server struct {
	Name, Version string
	// Instructions are sent to the client at initialize.
	Instructions string
	// OnCall, if set, is invoked after each tools/call.
	OnCall CallHook

	mu      sync.RWMutex
	tools   map[string]tools.Tool
	prompts map[string]Prompt
}

// New returns an empty server.
func New(name, version string) *Server {
	return &Server{Name: name, Version: version, tools: map[string]tools.Tool{}, prompts: map[string]Prompt{}}
}

// AddTool registers tools (a later tool with the same name replaces it).
func (s *Server) AddTool(ts ...tools.Tool) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range ts {
		s.tools[t.Name()] = t
	}
	return s
}

// AddPrompt registers a prompt.
func (s *Server) AddPrompt(p Prompt) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[p.Name] = p
	return s
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Handle processes one JSON-RPC message and returns the response bytes, or nil
// for a notification.
func (s *Server) Handle(ctx context.Context, raw []byte) []byte {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return mustJSON(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeParse, "parse error"}})
	}
	if req.Method == "" {
		return mustJSON(response{JSONRPC: "2.0", ID: nullID(req.ID), Error: &rpcError{codeInvalidRequest, "missing method"}})
	}
	if len(req.ID) == 0 { // notification
		return nil
	}
	res, rerr := s.dispatch(ctx, req)
	out := response{JSONRPC: "2.0", ID: req.ID}
	if rerr != nil {
		out.Error = rerr
	} else {
		out.Result = res
	}
	return mustJSON(out)
}

func nullID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32603, err.Error()}})
	}
	return b
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		caps := map[string]any{}
		s.mu.RLock()
		if len(s.tools) > 0 {
			caps["tools"] = map[string]any{}
		}
		if len(s.prompts) > 0 {
			caps["prompts"] = map[string]any{}
		}
		s.mu.RUnlock()
		res := map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    caps,
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		s.mu.RLock()
		names := make([]string, 0, len(s.tools))
		for n := range s.tools {
			names = append(names, n)
		}
		sort.Strings(names)
		list := make([]map[string]any, 0, len(names))
		for _, n := range names {
			d := tools.Define(s.tools[n])
			list = append(list, map[string]any{"name": d.Name, "description": d.Description, "inputSchema": d.Parameters})
		}
		s.mu.RUnlock()
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, err.Error()}
		}
		s.mu.RLock()
		t, ok := s.tools[p.Name]
		s.mu.RUnlock()
		if !ok {
			return nil, &rpcError{codeInvalidParams, fmt.Sprintf("unknown tool %q", p.Name)}
		}
		return s.callTool(ctx, t, p.Arguments), nil
	case "prompts/list":
		s.mu.RLock()
		list := make([]map[string]any, 0, len(s.prompts))
		for _, p := range s.prompts {
			list = append(list, map[string]any{"name": p.Name, "description": p.Description, "arguments": p.Arguments})
		}
		s.mu.RUnlock()
		sort.Slice(list, func(i, j int) bool { return list[i]["name"].(string) < list[j]["name"].(string) })
		return map[string]any{"prompts": list}, nil
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, err.Error()}
		}
		s.mu.RLock()
		pr, ok := s.prompts[p.Name]
		s.mu.RUnlock()
		if !ok {
			return nil, &rpcError{codeInvalidParams, fmt.Sprintf("unknown prompt %q", p.Name)}
		}
		for _, a := range pr.Arguments {
			if a.Required && p.Arguments[a.Name] == "" {
				return nil, &rpcError{codeInvalidParams, fmt.Sprintf("missing required argument %q", a.Name)}
			}
		}
		msgs, err := pr.Get(ctx, p.Arguments)
		if err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		out := make([]map[string]any, len(msgs))
		for i, m := range msgs {
			out[i] = map[string]any{"role": m.Role, "content": map[string]any{"type": "text", "text": m.Text}}
		}
		return map[string]any{"description": pr.Description, "messages": out}, nil
	}
	return nil, &rpcError{codeMethodNotFound, "method not found: " + req.Method}
}

// callTool runs t. Tool failures are results with isError set (so the model
// can read them), never protocol errors.
func (s *Server) callTool(ctx context.Context, t tools.Tool, args map[string]any) map[string]any {
	start := time.Now()
	out, err := t.Call(ctx, args)
	if s.OnCall != nil {
		s.OnCall(t.Name(), args, time.Since(start), err)
	}
	if err != nil {
		return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": err.Error()}}}
	}
	res := map[string]any{}
	if ca, ok := out.(tools.ContentAndArtifact); ok {
		res["content"] = []map[string]any{{"type": "text", "text": tools.Stringify(ca.Content)}}
		if m, ok := ca.Artifact.(map[string]any); ok {
			res["structuredContent"] = m
		}
		return res
	}
	res["content"] = []map[string]any{{"type": "text", "text": tools.Stringify(out)}}
	return res
}

// ServeStdio reads newline-delimited JSON-RPC from r and writes responses to
// w until r is closed or ctx is done.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var wmu sync.Mutex
	var wg sync.WaitGroup
	for sc.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out := s.Handle(ctx, line); out != nil {
				wmu.Lock()
				defer wmu.Unlock()
				_, _ = w.Write(append(out, '\n'))
			}
		}()
	}
	wg.Wait()
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return ctx.Err()
}

// Handler serves JSON-RPC over HTTP POST (the request/response subset of the
// streamable HTTP transport); notifications get 202.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := s.Handle(r.Context(), body)
		if out == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	})
}
