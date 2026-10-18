// Package mcp is a Model Context Protocol client (langchain-mcp-adapters):
// it connects to MCP servers over stdio or streamable HTTP and exposes their
// tools as tools.Tool values usable by any agent.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

const protocolVersion = "2025-06-18"

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error returned by the server.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, e.Message) }

// Transport carries JSON-RPC messages.
type Transport interface {
	// Call sends a request and waits for its response.
	Call(ctx context.Context, id int64, req []byte) (rpcMsg, error)
	// Notify sends a notification (no response).
	Notify(ctx context.Context, msg []byte) error
	Close() error
}

// Client is a connected MCP session.
type Client struct {
	t      Transport
	nextID atomic.Int64
	// ServerName is reported by the server during initialize.
	ServerName string
}

// Connect performs the initialize handshake over t.
func Connect(ctx context.Context, t Transport) (*Client, error) {
	c := &Client{t: t}
	var res struct {
		ServerInfo struct{ Name string } `json:"serverInfo"`
	}
	err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "golang-chain-eco", "version": "0.1.0"},
	}, &res)
	if err != nil {
		t.Close()
		return nil, err
	}
	c.ServerName = res.ServerInfo.Name
	b, _ := json.Marshal(rpcMsg{JSONRPC: "2.0", Method: "notifications/initialized"})
	if err := t.Notify(ctx, b); err != nil {
		t.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) call(ctx context.Context, method string, params, out any) error {
	id := c.nextID.Add(1)
	b, err := json.Marshal(rpcMsg{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return err
	}
	resp, err := c.t.Call(ctx, id, b)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Close ends the session.
func (c *Client) Close() error { return c.t.Close() }

// ToolInfo is a tool advertised by the server.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListTools returns every tool of the server (following pagination).
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var all []ToolInfo
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var res struct {
			Tools      []ToolInfo `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
}

// CallTool invokes a tool and flattens its text content. Server-reported
// tool failures (isError) become tools.Error so agents show them to the model.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent any  `json:"structuredContent"`
		IsError           bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &res); err != nil {
		return nil, err
	}
	var parts []string
	for _, p := range res.Content {
		if p.Type == "text" {
			parts = append(parts, p.Text)
		} else {
			parts = append(parts, fmt.Sprintf("[%s content omitted]", p.Type))
		}
	}
	text := strings.Join(parts, "\n")
	if res.IsError {
		return nil, tools.Errorf("%s", text)
	}
	if res.StructuredContent != nil {
		return tools.ContentAndArtifact{Content: text, Artifact: res.StructuredContent}, nil
	}
	return text, nil
}

// Tools adapts the server's tools; prefix (e.g. "weather") is prepended as
// "<prefix>_<name>" to avoid collisions across servers.
func (c *Client) Tools(ctx context.Context, prefix string) ([]tools.Tool, error) {
	infos, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]tools.Tool, 0, len(infos))
	for _, ti := range infos {
		schema := &jsonschema.Schema{Type: "object"}
		if len(ti.InputSchema) > 0 {
			if err := json.Unmarshal(ti.InputSchema, schema); err != nil {
				return nil, fmt.Errorf("mcp: tool %q has an invalid schema: %w", ti.Name, err)
			}
		}
		name := ti.Name
		if prefix != "" {
			name = prefix + "_" + name
		}
		remote := ti.Name
		out = append(out, tools.NewRaw(name, ti.Description, schema, func(ctx context.Context, args map[string]any) (any, error) {
			return c.CallTool(ctx, remote, args)
		}))
	}
	return out, nil
}

// ---------------------------------------------------------------- stdio

type stdioTransport struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	mu   sync.Mutex // guards writes and pending
	wait map[int64]chan rpcMsg
	done chan struct{}
	err  error
}

// Stdio launches command and talks JSON-RPC over its stdin/stdout. The
// command runs with the privileges of this process; only launch servers you
// trust.
func Stdio(ctx context.Context, command string, args []string, env []string) (Transport, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if env != nil {
		cmd.Env = env
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return newPipeTransport(cmd, in, out), nil
}

// NewPipeTransport speaks line-delimited JSON-RPC over arbitrary streams.
func NewPipeTransport(w io.WriteCloser, r io.Reader) Transport { return newPipeTransport(nil, w, r) }

func newPipeTransport(cmd *exec.Cmd, w io.WriteCloser, r io.Reader) *stdioTransport {
	t := &stdioTransport{cmd: cmd, in: w, wait: map[int64]chan rpcMsg{}, done: make(chan struct{})}
	go func() {
		defer close(t.done)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var m rpcMsg
			if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == nil || m.Method != "" {
				continue // server notifications / requests are ignored
			}
			t.mu.Lock()
			ch := t.wait[*m.ID]
			delete(t.wait, *m.ID)
			t.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
		t.mu.Lock()
		t.err = errors.Join(sc.Err(), errors.New("mcp: server closed the connection"))
		t.mu.Unlock()
	}()
	return t
}

func (t *stdioTransport) write(b []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.in.Write(append(b, '\n'))
	return err
}

func (t *stdioTransport) Call(ctx context.Context, id int64, req []byte) (rpcMsg, error) {
	ch := make(chan rpcMsg, 1)
	t.mu.Lock()
	t.wait[id] = ch
	t.mu.Unlock()
	if err := t.write(req); err != nil {
		return rpcMsg{}, err
	}
	select {
	case m := <-ch:
		return m, nil
	case <-t.done:
		t.mu.Lock()
		defer t.mu.Unlock()
		return rpcMsg{}, t.err
	case <-ctx.Done():
		return rpcMsg{}, ctx.Err()
	}
}

func (t *stdioTransport) Notify(_ context.Context, b []byte) error { return t.write(b) }

func (t *stdioTransport) Close() error {
	t.in.Close()
	if t.cmd != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	return nil
}

// ---------------------------------------------------------------- http

type httpTransport struct {
	url     string
	headers map[string]string
	hc      *http.Client
	mu      sync.Mutex
	session string
}

// HTTP connects to a streamable-HTTP MCP endpoint.
func HTTP(url string, headers map[string]string) Transport {
	return &httpTransport{url: url, headers: headers, hc: http.DefaultClient}
}

func (t *httpTransport) post(ctx context.Context, b []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	if t.session != "" {
		req.Header.Set("Mcp-Session-Id", t.session)
	}
	t.mu.Unlock()
	resp, err := t.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		t.mu.Lock()
		t.session = s
		t.mu.Unlock()
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("mcp: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (t *httpTransport) Call(ctx context.Context, id int64, req []byte) (rpcMsg, error) {
	resp, err := t.post(ctx, req)
	if err != nil {
		return rpcMsg{}, err
	}
	defer resp.Body.Close()
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data:")
			if !ok {
				continue
			}
			var m rpcMsg
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &m) == nil && m.ID != nil && *m.ID == id && m.Method == "" {
				return m, nil
			}
		}
		return rpcMsg{}, errors.Join(sc.Err(), errors.New("mcp: stream ended without a response"))
	}
	var m rpcMsg
	err = json.NewDecoder(resp.Body).Decode(&m)
	return m, err
}

func (t *httpTransport) Notify(ctx context.Context, b []byte) error {
	resp, err := t.post(ctx, b)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (t *httpTransport) Close() error { return nil }

// ---------------------------------------------------------------- multi

// ServerConfig describes one MCP server for LoadTools.
type ServerConfig struct {
	// stdio
	Command string
	Args    []string
	Env     []string
	// streamable http
	URL     string
	Headers map[string]string
}

// LoadTools connects to every configured server and returns all their tools
// with names prefixed by the server key (MultiServerMCPClient.get_tools).
// The returned closer ends all sessions.
func LoadTools(ctx context.Context, servers map[string]ServerConfig) ([]tools.Tool, func() error, error) {
	var clients []*Client
	closeAll := func() error {
		var errs []error
		for _, c := range clients {
			errs = append(errs, c.Close())
		}
		return errors.Join(errs...)
	}
	var all []tools.Tool
	for name, cfg := range servers {
		var t Transport
		var err error
		switch {
		case cfg.URL != "":
			t = HTTP(cfg.URL, cfg.Headers)
		case cfg.Command != "":
			t, err = Stdio(ctx, cfg.Command, cfg.Args, cfg.Env)
		default:
			err = errors.New("need Command or URL")
		}
		if err != nil {
			_ = closeAll()
			return nil, nil, fmt.Errorf("mcp server %q: %w", name, err)
		}
		c, err := Connect(ctx, t)
		if err != nil {
			_ = closeAll()
			return nil, nil, fmt.Errorf("mcp server %q: %w", name, err)
		}
		clients = append(clients, c)
		ts, err := c.Tools(ctx, name)
		if err != nil {
			_ = closeAll()
			return nil, nil, fmt.Errorf("mcp server %q: %w", name, err)
		}
		all = append(all, ts...)
	}
	return all, closeAll, nil
}
