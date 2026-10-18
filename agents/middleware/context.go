package middleware

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Summarization compresses old history into a summary message once the
// conversation exceeds MaxTokens (SummarizationMiddleware).
type Summarization struct {
	Model models.ChatModel
	// MaxTokens triggers summarization (default 4000).
	MaxTokens int
	// KeepMessages most recent messages are preserved (default 20).
	KeepMessages int
	Counter      messages.TokenCounter
	Prompt       string
}

const defaultSummaryPrompt = "Summarize the conversation below. Keep every fact, decision, name and open task needed to continue.\n\n%s"

func (Summarization) Name() string { return "summarization" }

func (s Summarization) BeforeModel(ctx context.Context, st graph.State) (any, error) {
	msgs, err := messages.Coerce(st["messages"])
	if err != nil {
		return nil, err
	}
	max, keep, count := s.MaxTokens, s.KeepMessages, s.Counter
	if max == 0 {
		max = 4000
	}
	if keep == 0 {
		keep = 20
	}
	if count == nil {
		count = messages.ApproxTokens
	}
	if count(msgs) <= max || len(msgs) <= keep {
		return nil, nil
	}
	cut := len(msgs) - keep
	// never start the kept window on a tool result: pull the cut back to
	// the AI message that issued the call
	for cut > 0 && msgs[cut].Role == messages.RoleTool {
		cut--
	}
	if cut <= 0 {
		return nil, nil
	}
	prompt := s.Prompt
	if prompt == "" {
		prompt = defaultSummaryPrompt
	}
	out, err := s.Model.Generate(ctx, []messages.Message{messages.Human(fmt.Sprintf(prompt, messages.Buffer(msgs[:cut])))})
	if err != nil {
		return nil, err
	}
	summary := messages.Human("Here is a summary of the conversation to date:\n\n" + out.Text())
	summary.Name = "summary"
	upd := []messages.Message{messages.Remove(messages.RemoveAll), summary}
	return graph.State{"messages": append(upd, msgs[cut:]...)}, nil
}

// ContextEditing clears the content of old tool results when the context is
// large (ContextEditingMiddleware / ClearToolUsesEdit). The edit only affects
// the model request, not stored history.
type ContextEditing struct {
	// TriggerTokens starts clearing (default 100000).
	TriggerTokens int
	// Keep most recent tool results untouched (default 3).
	Keep        int
	Placeholder string
	Counter     messages.TokenCounter
}

func (ContextEditing) Name() string { return "context_editing" }

func (c ContextEditing) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	trig, keep, ph, count := c.TriggerTokens, c.Keep, c.Placeholder, c.Counter
	if trig == 0 {
		trig = 100000
	}
	if keep == 0 {
		keep = 3
	}
	if ph == "" {
		ph = "[cleared]"
	}
	if count == nil {
		count = messages.ApproxTokens
	}
	if count(req.Messages) > trig {
		r := *req
		r.Messages = append([]messages.Message(nil), req.Messages...)
		seen := 0
		for i := len(r.Messages) - 1; i >= 0; i-- {
			if r.Messages[i].Role != messages.RoleTool {
				continue
			}
			if seen++; seen > keep {
				r.Messages[i].Content, r.Messages[i].Parts = ph, nil
			}
		}
		req = &r
	}
	return next(ctx, req)
}

// Todo is one planned step.
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed
}

// TodoList gives the agent a write_todos planning tool and a "todos" state
// key (TodoListMiddleware).
type TodoList struct{ SystemPrompt string }

const todoPrompt = "You have a write_todos tool to plan and track multi-step work. Use it for complex tasks; mark items completed as you finish them."

func (TodoList) Name() string { return "todo_list" }
func (TodoList) StateSchema() graph.Schema {
	return graph.Schema{"todos": channels.LastValue()}
}
func (TodoList) Tools() []tools.Tool {
	type args struct {
		Todos []Todo `json:"todos" description:"The full updated todo list"`
	}
	return []tools.Tool{tools.New("write_todos", "Create or update the todo list for the current task.",
		func(ctx context.Context, a args) (any, error) {
			m := messages.Tool(fmt.Sprintf("Updated todo list to %v", a.Todos), tools.CallID(ctx))
			m.Name = "write_todos"
			return graph.Command{Update: graph.State{"todos": a.Todos, "messages": []messages.Message{m}}}, nil
		})}
}
func (t TodoList) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	p := t.SystemPrompt
	if p == "" {
		p = todoPrompt
	}
	r := *req
	r.System = strings.TrimSpace(req.System + "\n\n" + p)
	return next(ctx, &r)
}

// PII strategies.
const (
	PIIRedact = "redact" // [REDACTED_EMAIL]
	PIIMask   = "mask"   // ****1234
	PIIBlock  = "block"  // error
)

// PIIKind pairs a name with a detector.
type PIIKind struct {
	Name    string
	Pattern *regexp.Regexp
}

// Built-in detectors.
var (
	PIIEmail      = PIIKind{"email", regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)}
	PIICreditCard = PIIKind{"credit_card", regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)}
	PIIIP         = PIIKind{"ip", regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)}
	PIIURL        = PIIKind{"url", regexp.MustCompile(`https?://[^\s]+`)}
)

// PIIError is returned by the block strategy.
type PIIError struct{ Kind string }

func (e *PIIError) Error() string { return "PII detected: " + e.Kind }

// PII scrubs personal data from user input before it reaches the model
// (PIIMiddleware). The stored history keeps the scrubbed text.
type PII struct {
	Kinds    []PIIKind
	Strategy string // default redact
}

func (PII) Name() string { return "pii" }

func (p PII) BeforeModel(_ context.Context, st graph.State) (any, error) {
	msgs, _ := messages.Coerce(st["messages"])
	var changed []messages.Message
	for _, m := range msgs {
		if m.Role != messages.RoleHuman && m.Role != messages.RoleTool {
			continue
		}
		text := m.Text()
		for _, k := range p.Kinds {
			var err error
			if text, err = p.scrub(k, text); err != nil {
				return nil, err
			}
		}
		if text != m.Text() {
			m.Content, m.Parts = text, nil
			changed = append(changed, m)
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	return graph.State{"messages": changed}, nil
}

func (p PII) scrub(k PIIKind, text string) (string, error) {
	if !k.Pattern.MatchString(text) {
		return text, nil
	}
	switch p.Strategy {
	case PIIBlock:
		return "", &PIIError{k.Name}
	case PIIMask:
		return k.Pattern.ReplaceAllStringFunc(text, func(s string) string {
			if len(s) <= 4 {
				return "****"
			}
			return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
		}), nil
	}
	return k.Pattern.ReplaceAllString(text, "[REDACTED_"+strings.ToUpper(k.Name)+"]"), nil
}

func init() { checkpoint.Register("[]middleware.Todo", []Todo{}) }
