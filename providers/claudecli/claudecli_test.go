package claudecli

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

func fake(result string, got *struct {
	args  []string
	stdin string
	env   []string
}) *Chat {
	return &Chat{Model: "haiku", Run: func(_ context.Context, _ string, args []string, stdin string, env []string, _ string) ([]byte, error) {
		if got != nil {
			got.args, got.stdin, got.env = args, stdin, env
		}
		return []byte(`{"is_error":false,"result":` + quote(result) + `,"usage":{"input_tokens":2,"cache_read_input_tokens":10,"cache_creation_input_tokens":5,"output_tokens":3}}`), nil
	}}
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

func TestPlainAnswerAndUsage(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	var got struct {
		args  []string
		stdin string
		env   []string
	}
	m, err := fake("hello", &got).Generate(context.Background(), []messages.Message{messages.System("be brief"), messages.Human("hi")})
	if err != nil || m.Content != "hello" {
		t.Fatalf("%v %+v", err, m)
	}
	if m.Usage.InputTokens != 17 || m.Usage.TotalTokens != 20 || m.Usage.CacheReadTokens != 10 {
		t.Fatalf("usage %+v", m.Usage)
	}
	joined := strings.Join(got.args, " ")
	for _, want := range []string{"--tools  --strict-mcp-config", "--model haiku", "--system-prompt be brief"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q lack %q", joined, want)
		}
	}
	if !strings.Contains(got.stdin, "[user]\nhi") {
		t.Errorf("stdin %q", got.stdin)
	}
	for _, kv := range got.env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY") {
			t.Fatal("the API key reached the CLI")
		}
	}
}

func TestToolCallsAreParsed(t *testing.T) {
	ts := []tools.Definition{{Name: "read", Description: "d", Parameters: map[string]any{"type": "object"}}}
	var got struct {
		args  []string
		stdin string
		env   []string
	}
	c := fake("```json\n{\"tool_calls\":[{\"name\":\"read\",\"args\":{\"path\":\"a\"}}]}\n```", &got)
	m, err := c.Generate(context.Background(), []messages.Message{messages.System("s"), messages.Human("go")}, func(o *models.Options) { o.Tools = ts })
	if err != nil || len(m.ToolCalls) != 1 || m.ToolCalls[0].Name != "read" || m.ToolCalls[0].Args["path"] != "a" || m.ToolCalls[0].ID == "" {
		t.Fatalf("%v %+v", err, m)
	}
	if !strings.Contains(strings.Join(got.args, " "), `"name":"read"`) {
		t.Error("the tool schema is not in the system prompt")
	}
	// plain text is not a tool call
	m, _ = fake("done", nil).Generate(context.Background(), []messages.Message{messages.Human("x")}, func(o *models.Options) { o.Tools = ts })
	if len(m.ToolCalls) != 0 || m.Content != "done" {
		t.Fatalf("%+v", m)
	}
}

func TestCLIErrorIsAnError(t *testing.T) {
	c := &Chat{Run: func(context.Context, string, []string, string, []string, string) ([]byte, error) {
		return []byte(`{"is_error":true,"result":"Credit balance is too low"}`), nil
	}}
	if _, err := c.Generate(context.Background(), []messages.Message{messages.Human("x")}); err == nil || !strings.Contains(err.Error(), "Credit") {
		t.Fatalf("%v", err)
	}
}

func TestObserverSeesStartAndEnd(t *testing.T) {
	var evs []Event
	c := fake("hi", nil)
	c.Persist = true
	c.Observer = func(e Event) { evs = append(evs, e) }
	if _, err := c.Generate(context.Background(), []messages.Message{messages.Human("x")}); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Done || !evs[1].Done || evs[0].ID != evs[1].ID || len(evs[0].Session) != 36 || evs[1].Usage == nil {
		t.Fatalf("%+v", evs)
	}
}

func TestInvokeXMLFormIsParsed(t *testing.T) {
	ts := []tools.Definition{{Name: "backend_read", Parameters: map[string]any{"properties": map[string]any{"path": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer"}}}}}
	text := "Vou ler.\n<invoke name=\"backend_read\">\n<parameter name=\"path\">/a/b</parameter>\n<parameter name=\"offset\">5</parameter>\n</invoke>\n<invoke name=\"backend_read\">\n<parameter name=\"path\">/a/b</parameter>\n<parameter name=\"offset\">5</parameter>\n</invoke>"
	m, err := fake(text, nil).Generate(context.Background(), []messages.Message{messages.Human("x")}, func(o *models.Options) { o.Tools = ts })
	if err != nil || len(m.ToolCalls) != 1 || m.ToolCalls[0].Args["path"] != "/a/b" || m.ToolCalls[0].Args["offset"] != float64(5) || m.Content != "Vou ler." {
		t.Fatalf("%v %+v", err, m)
	}
}
