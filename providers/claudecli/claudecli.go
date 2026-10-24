// Package claudecli is a chat model that answers through the Claude Code CLI
// (`claude -p`) instead of the Messages API, so the calls are billed to the
// Claude subscription the CLI is logged in with, not to API credit.
//
// One model call is one CLI run with the built-in tools, MCP servers, skills,
// hooks and project memory switched off: the CLI is only the transport. Tool
// use is emulated in the prompt, because the CLI would run its own tools: the
// tool schemas go in the system prompt and the model answers with a JSON
// object to call them. That costs a small fixed overhead per call (the CLI's
// own prompt, about 800 tokens) and is slower than the API; use it when the
// subscription is the budget. ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN and
// ANTHROPIC_BASE_URL are removed from the child's environment, so it can never
// fall back to the API by accident (set KeepAPIEnv to allow that).
//
// Check the terms of your Claude plan before running it unattended: it is meant
// for your own interactive use.
package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// Chat is a chat model backed by the claude CLI.
type Chat struct {
	Model      string        // model id or alias passed to --model, e.g. "claude-sonnet-5-5" or "sonnet"
	Bin        string        // default $CLAUDE_BIN, else "claude"
	Timeout    time.Duration // per call; default 5 minutes
	Effort     string        // optional --effort level
	KeepAPIEnv bool          // keep ANTHROPIC_* credentials in the child's environment
	Defaults   []models.Option
	// Run executes the CLI with the prompt on stdin and returns its stdout.
	// Nil runs the real binary; tests replace it.
	Run func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error)
}

// New creates a claude CLI chat model.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

// ModelID implements models.ChatModel.
func (c *Chat) ModelID() string { return "claude-cli:" + c.Model }

// slots bounds the CLI processes running at once, so a team of workers does not
// trip the plan's rate limits. CLAUDE_CLI_CONCURRENCY changes it (default 3).
var slots = func() chan struct{} {
	n, _ := strconv.Atoi(os.Getenv("CLAUDE_CLI_CONCURRENCY"))
	if n <= 0 {
		n = 3
	}
	return make(chan struct{}, n)
}()

const maxArg = 100 << 10 // a command-line argument is limited to 128 KiB on Linux

type cliResult struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Subtype string `json:"subtype"`
	Usage   struct {
		In     int `json:"input_tokens"`
		Out    int `json:"output_tokens"`
		Read   int `json:"cache_read_input_tokens"`
		Create int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

var ids atomic.Int64

// Generate implements models.ChatModel.
func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		system, prompt := render(msgs, o)
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-ctx.Done():
			return messages.Message{}, ctx.Err()
		}
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = 5 * time.Minute
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		model := c.Model
		if o.Model != "" {
			model = o.Model
		}
		args := []string{"-p", "--output-format", "json", "--tools", "", "--strict-mcp-config",
			"--disable-slash-commands", "--no-session-persistence", "--setting-sources", ""}
		if model != "" {
			args = append(args, "--model", model)
		}
		if c.Effort != "" {
			args = append(args, "--effort", c.Effort)
		}
		if len(system) <= maxArg {
			args = append(args, "--system-prompt", system)
		} else {
			prompt = "# System instructions\n" + system + "\n\n" + prompt
			args = append(args, "--system-prompt", "Follow the system instructions given at the start of the user message.")
		}
		bin := c.Bin
		if bin == "" {
			bin = os.Getenv("CLAUDE_BIN")
		}
		if bin == "" {
			bin = "claude"
		}
		run := c.Run
		if run == nil {
			run = execRun
		}
		out, err := run(ctx, bin, args, prompt, c.env())
		if err != nil {
			return messages.Message{}, fmt.Errorf("claudecli: %w", err)
		}
		var r cliResult
		if err := json.Unmarshal(out, &r); err != nil {
			return messages.Message{}, fmt.Errorf("claudecli: unreadable output: %w", err)
		}
		if r.IsError {
			return messages.Message{}, fmt.Errorf("claudecli: %s", strings.TrimSpace(r.Result))
		}
		in := r.Usage.In + r.Usage.Read + r.Usage.Create
		msg := messages.Message{Role: messages.RoleAI, Usage: &messages.Usage{
			InputTokens: in, OutputTokens: r.Usage.Out, TotalTokens: in + r.Usage.Out,
			CacheReadTokens: r.Usage.Read, CacheCreationTokens: r.Usage.Create,
		}}
		return parse(msg, r.Result, o), nil
	})
}

// Stream implements models.ChatModel: the CLI answers whole, so it is one chunk.
func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	return func(yield func(messages.Message, error) bool) {
		yield(c.Generate(ctx, msgs, opts...))
	}
}

func (c *Chat) env() []string {
	env := os.Environ()
	if c.KeepAPIEnv {
		return env
	}
	out := env[:0:0]
	for _, kv := range env {
		switch k, _, _ := strings.Cut(kv, "="); k {
		case "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL":
		default:
			out = append(out, kv)
		}
	}
	return out
}

func execRun(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = env
	cmd.Dir = os.TempDir() // no project CLAUDE.md or settings are picked up
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return nil, fmt.Errorf("%s: %w: %s", bin, err, msg)
	}
	return stdout.Bytes(), nil
}

const toolProtocol = `

# Tools
You can call the tools below. To call one or more, reply with ONLY a JSON object, no prose and no code fence:
{"tool_calls":[{"name":"<tool>","args":{...}}]}
You will then receive the results and continue. To answer without calling a tool, reply with plain text.
Tools (JSON schema of each):
`

// render splits msgs into the system prompt (with the tool protocol) and the
// transcript sent on stdin.
func render(msgs []messages.Message, o models.Options) (system, prompt string) {
	var sys, p strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case messages.RoleSystem:
			sys.WriteString(m.Text() + "\n")
		case messages.RoleHuman:
			p.WriteString("[user]\n" + m.Text() + "\n\n")
		case messages.RoleAI:
			p.WriteString("[assistant]\n" + m.Text() + "\n")
			for _, tc := range m.ToolCalls {
				b, _ := json.Marshal(map[string]any{"id": tc.ID, "name": tc.Name, "args": tc.Args})
				p.WriteString("tool call: " + string(b) + "\n")
			}
			p.WriteString("\n")
		case messages.RoleTool:
			status := ""
			if m.Status == "error" {
				status = " (error)"
			}
			p.WriteString("[tool result " + m.ToolCallID + status + "]\n" + m.Text() + "\n\n")
		}
	}
	ts := o.Tools
	if rf := o.ResponseFormat; rf != nil {
		b, _ := json.Marshal(rf.Schema)
		sys.WriteString("\nReply with ONLY a JSON document that matches this JSON schema, no prose and no code fence:\n" + string(b) + "\n")
	}
	if len(ts) > 0 {
		sys.WriteString(toolProtocol)
		for _, t := range ts {
			b, _ := json.Marshal(map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters})
			sys.WriteString(string(b) + "\n")
		}
		if o.ToolChoice == "any" || o.ToolChoice == "required" {
			sys.WriteString("You must call a tool in this reply.\n")
		}
	}
	p.WriteString("[assistant]\n")
	return strings.TrimSpace(sys.String()), p.String()
}

// parse turns the CLI's text into the AI message: tool calls when the reply is
// the tool-call JSON, text otherwise.
func parse(msg messages.Message, text string, o models.Options) messages.Message {
	text = strings.TrimSpace(text)
	if len(o.Tools) > 0 {
		var env struct {
			ToolCalls []struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			} `json:"tool_calls"`
		}
		if json.Unmarshal([]byte(unfence(text)), &env) == nil && len(env.ToolCalls) > 0 {
			for _, tc := range env.ToolCalls {
				if tc.Args == nil {
					tc.Args = map[string]any{}
				}
				msg.ToolCalls = append(msg.ToolCalls, messages.ToolCall{ID: fmt.Sprintf("call_cli_%d", ids.Add(1)), Name: tc.Name, Args: tc.Args})
			}
			return msg
		}
	}
	if o.ResponseFormat != nil {
		text = unfence(text)
	}
	msg.Content = text
	return msg
}

// unfence removes a ```json fence the model added around its JSON.
func unfence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if _, rest, ok := strings.Cut(s, "\n"); ok {
		s = rest
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

var _ models.ChatModel = (*Chat)(nil)
