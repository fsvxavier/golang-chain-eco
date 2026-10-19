package server_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/mcp"
	"github.com/fsvxavier/golang-chain-eco/mcp/server"
)

type addArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

func newServer() *server.Server {
	s := server.New("test", "1.0")
	s.AddTool(
		tools.New("add", "adds numbers", func(_ context.Context, a addArgs) (any, error) { return a.A + a.B, nil }),
		tools.New("boom", "always fails", func(context.Context, struct{}) (any, error) { return nil, tools.Errorf("nope") }),
	)
	s.AddPrompt(server.Prompt{Name: "greet", Description: "greets", Arguments: []server.PromptArg{{Name: "who", Required: true}},
		Get: func(_ context.Context, a map[string]string) ([]server.PromptMessage, error) {
			return []server.PromptMessage{{Role: "user", Text: "hello " + a["who"]}}, nil
		}})
	return s
}

// The existing mcp client must be able to talk to the new server.
func TestClientOverPipes(t *testing.T) {
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = newServer().ServeStdio(ctx, sr, sw) }()

	c, err := mcp.Connect(ctx, mcp.NewPipeTransport(cw, cr))
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerName != "test" {
		t.Fatalf("server name %q", c.ServerName)
	}
	infos, err := c.ListTools(ctx)
	if err != nil || len(infos) != 2 || infos[0].Name != "add" {
		t.Fatalf("%+v %v", infos, err)
	}
	out, err := c.CallTool(ctx, "add", map[string]any{"a": 2, "b": 3})
	if err != nil || out != "5" {
		t.Fatalf("%v %v", out, err)
	}
	if _, err := c.CallTool(ctx, "boom", nil); !tools.IsToolError(err) {
		t.Fatalf("tool failure must reach the client as a tool error: %v", err)
	}
}

func TestHTTPAndProtocolErrors(t *testing.T) {
	ts := httptest.NewServer(newServer().Handler())
	defer ts.Close()
	c, err := mcp.Connect(context.Background(), mcp.HTTP(ts.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := c.CallTool(context.Background(), "add", map[string]any{"a": 1, "b": 1}); err != nil || out != "2" {
		t.Fatalf("%v %v", out, err)
	}
	s := newServer()
	for body, want := range map[string]string{
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`:                                  "method not found",
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ghost"}}`:  "unknown tool",
		`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"greet"}}`: "missing required argument",
		`not json`: "parse error",
		`{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":{"name":"greet","arguments":{"who":"Ana"}}}`: "hello Ana",
	} {
		if out := string(s.Handle(context.Background(), []byte(body))); !strings.Contains(out, want) {
			t.Errorf("%s -> %s (want %q)", body, out, want)
		}
	}
	if out := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); out != nil {
		t.Fatalf("notification must not be answered: %s", out)
	}
}

func TestOnCallHook(t *testing.T) {
	s := newServer()
	var got []string
	s.OnCall = func(name string, _ map[string]any, _ time.Duration, err error) {
		got = append(got, name+":"+map[bool]string{true: "err", false: "ok"}[err != nil])
	}
	s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom"}}`))
	s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":1,"b":2}}}`))
	if strings.Join(got, ",") != "boom:err,add:ok" {
		t.Fatal(got)
	}
}
