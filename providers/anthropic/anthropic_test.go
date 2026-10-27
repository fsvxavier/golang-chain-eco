package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/mcp"
	"github.com/fsvxavier/golang-chain-eco/providers/anthropic"
)

type args struct {
	City string `json:"city"`
}

func TestGenerate(t *testing.T) {
	var req map[string]any
	var key, ver string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ver = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&req)
		fmt.Fprint(w, `{"content":[{"type":"text","text":"Checking."},{"type":"tool_use","id":"tu1","name":"weather","input":{"city":"Rome"}}],"stop_reason":"tool_use","usage":{"input_tokens":3,"output_tokens":4}}`)
	}))
	defer srv.Close()
	m := &anthropic.Chat{Model: "claude-x", APIKey: "ak", BaseURL: srv.URL}
	w := tools.New("weather", "w", func(context.Context, args) (any, error) { return "", nil })
	hist := []messages.Message{
		messages.System("sys"), messages.Human("hi"),
		messages.AI("", messages.ToolCall{ID: "old", Name: "weather", Args: map[string]any{"city": "X"}}),
		messages.Tool("rain", "old"),
	}
	out, err := m.Generate(context.Background(), hist, models.WithTools(w))
	if err != nil {
		t.Fatal(err)
	}
	if key != "ak" || ver == "" || sysText(req) != "sys" || req["max_tokens"] == nil {
		t.Fatalf("request: %v key=%q ver=%q", req, key, ver)
	}
	ms := req["messages"].([]any)
	if len(ms) != 3 { // user, assistant(tool_use), user(tool_result)
		t.Fatalf("messages: %v", ms)
	}
	last := ms[2].(map[string]any)
	if last["role"] != "user" || last["content"].([]any)[0].(map[string]any)["type"] != "tool_result" {
		t.Fatalf("tool result must be a user tool_result block: %v", last)
	}
	if out.Content != "Checking." || len(out.ToolCalls) != 1 || out.ToolCalls[0].ID != "tu1" || out.ToolCalls[0].Args["city"] != "Rome" {
		t.Fatalf("%+v", out)
	}
	if out.Usage == nil || out.Usage.InputTokens != 3 || out.Usage.OutputTokens != 4 {
		t.Fatalf("%+v", out.Usage)
	}
}

func TestStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range [][2]string{
			{"message_start", `{"type":"message_start","message":{"usage":{"input_tokens":2}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi "}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"there"}}`},
			{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu2","name":"weather","input":{}}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Oslo\"}"}}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`},
			{"message_stop", `{"type":"message_stop"}`},
		} {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev[0], ev[1])
		}
	}))
	defer srv.Close()
	m := &anthropic.Chat{Model: "claude-x", APIKey: "ak", BaseURL: srv.URL}
	out, err := models.Collect(m.Stream(context.Background(), []messages.Message{messages.Human("hi")}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "Hi there" || len(out.ToolCalls) != 1 || out.ToolCalls[0].Args["city"] != "Oslo" {
		t.Fatalf("%+v", out)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
	}))
	defer srv.Close()
	m := &anthropic.Chat{Model: "claude-x", APIKey: "x", BaseURL: srv.URL}
	if _, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")}); err == nil {
		t.Fatal("expected error")
	}
}

func sysText(req map[string]any) string {
	bs, _ := req["system"].([]any)
	if len(bs) == 0 {
		return ""
	}
	b, _ := bs[0].(map[string]any)
	s, _ := b["text"].(string)
	return s
}

func TestCacheUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":4,"cache_read_input_tokens":30,"cache_creation_input_tokens":20}}`)
	}))
	defer srv.Close()
	m := &anthropic.Chat{Model: "claude-x", APIKey: "ak", BaseURL: srv.URL}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")})
	if err != nil {
		t.Fatal(err)
	}
	u := out.Usage
	if u == nil || u.CacheReadTokens != 30 || u.CacheCreationTokens != 20 || u.InputTokens != 55 {
		t.Fatalf("%+v", u)
	}
}

// A gateway is reached through ANTHROPIC_BASE_URL, with ANTHROPIC_AUTH_TOKEN as a
// Bearer token (no x-api-key when there is no API key), like Claude Code does.
func TestGatewayFromEnv(t *testing.T) {
	var path, auth, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, key = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-api-key")
		fmt.Fprint(w, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "gw-token")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := anthropic.New("claude-x").Generate(context.Background(), []messages.Message{messages.Human("hi")}); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/messages" || auth != "Bearer gw-token" || key != "" {
		t.Fatalf("path %q auth %q x-api-key %q", path, auth, key)
	}
}

// cache_control on a system part must reach the API, where it covers the tools too.
func TestSystemCacheControlReachesTheAPI(t *testing.T) {
	var req map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&req)
		fmt.Fprint(w, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	sys := messages.Message{Role: messages.RoleSystem, Parts: []messages.ContentPart{{Type: "text", Text: "skill", Extra: map[string]any{"cache_control": map[string]any{"type": "ephemeral"}}}}}
	m := &anthropic.Chat{Model: "claude-x", APIKey: "k", BaseURL: srv.URL}
	if _, err := m.Generate(context.Background(), []messages.Message{sys, messages.Human("hi")}); err != nil {
		t.Fatal(err)
	}
	b := req["system"].([]any)[0].(map[string]any)
	if b["text"] != "skill" || b["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("system block: %v", b)
	}
}

func TestMCPConnectorWithOAuthToken(t *testing.T) {
	var req map[string]any
	var beta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beta = r.Header.Get("anthropic-beta")
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range []string{
				`{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_use","id":"m1","name":"search","server_name":"docs","input":{}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":1}"}}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"mcp_tool_result","tool_use_id":"m1","content":[]}}`,
				`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"done"}}`,
			} {
				fmt.Fprintf(w, "data: %s\n\n", ev)
			}
			return
		}
		fmt.Fprint(w, `{"content":[{"type":"mcp_tool_use","id":"m1","name":"search","input":{}},{"type":"mcp_tool_result","tool_use_id":"m1","content":[]},{"type":"text","text":"done"}]}`)
	}))
	defer srv.Close()
	// a token already on disk: no login, no network
	file := t.TempDir() + "/tok.json"
	if err := os.WriteFile(file, []byte(`{"access_token":"oauth-tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &anthropic.Chat{Model: "claude-x", APIKey: "ak", BaseURL: srv.URL, MCPServers: []anthropic.MCPServer{
		{Name: "docs", URL: "https://mcp.example.com/mcp", OAuth: &mcp.OAuth{TokenFile: file}, AllowedTools: []string{"search"}},
	}}
	hist := []messages.Message{messages.Human("hi")}
	out, err := m.Generate(context.Background(), hist)
	if err != nil || out.Content != "done" || len(out.ToolCalls) != 0 {
		t.Fatalf("%v %+v", err, out)
	}
	srvs := req["mcp_servers"].([]any)[0].(map[string]any)
	ts := req["tools"].([]any)[0].(map[string]any)
	if beta == "" || srvs["authorization_token"] != "oauth-tok" || srvs["url"] != "https://mcp.example.com/mcp" ||
		ts["type"] != "mcp_toolset" || ts["mcp_server_name"] != "docs" || ts["configs"].(map[string]any)["search"] == nil {
		t.Fatalf("beta=%q req=%v", beta, req)
	}
	out, err = models.Collect(m.Stream(context.Background(), hist))
	if err != nil || strings.TrimSpace(out.Content) != "done" || len(out.ToolCalls) != 0 {
		t.Fatalf("stream: %v %+v", err, out)
	}
}
