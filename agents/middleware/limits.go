// Package middleware contains ready-made agents.Middleware implementations
// (langchain.agents.middleware).
package middleware

import (
	"context"
	"fmt"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// LimitError is returned when ExitBehavior is "error".
type LimitError struct{ Msg string }

func (e *LimitError) Error() string { return e.Msg }

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

// ModelCallLimit caps model calls per thread and/or per run
// (ModelCallLimitMiddleware). Zero limits are disabled.
type ModelCallLimit struct {
	ThreadLimit, RunLimit int
	// ExitBehavior is "end" (default, adds an AI message) or "error".
	ExitBehavior string
}

func (ModelCallLimit) Name() string { return "model_call_limit" }
func (ModelCallLimit) StateSchema() graph.Schema {
	return graph.Schema{"thread_model_call_count": graph.Sum(), "run_model_call_count": channels.LastValue()}
}
func (m ModelCallLimit) BeforeAgent(context.Context, graph.State) (any, error) {
	return graph.State{"run_model_call_count": 0}, nil
}
func (m ModelCallLimit) BeforeModel(_ context.Context, st graph.State) (any, error) {
	t, r := asInt(st["thread_model_call_count"]), asInt(st["run_model_call_count"])
	if (m.ThreadLimit > 0 && t >= m.ThreadLimit) || (m.RunLimit > 0 && r >= m.RunLimit) {
		msg := fmt.Sprintf("Model call limits exceeded: thread %d/%d, run %d/%d", t, m.ThreadLimit, r, m.RunLimit)
		if m.ExitBehavior == "error" {
			return nil, &LimitError{msg}
		}
		return graph.State{"jump_to": agents.JumpEnd, "messages": []messages.Message{messages.AI(msg)}}, nil
	}
	return nil, nil
}
func (ModelCallLimit) AfterModel(_ context.Context, st graph.State) (any, error) {
	return graph.State{"thread_model_call_count": 1, "run_model_call_count": asInt(st["run_model_call_count"]) + 1}, nil
}

// ToolCallLimit caps tool executions per thread / run (ToolCallLimitMiddleware).
// Calls beyond the limit are answered with an error ToolMessage.
type ToolCallLimit struct {
	// ToolName restricts counting to one tool ("" = all tools).
	ToolName              string
	ThreadLimit, RunLimit int
	// ExitBehavior: "continue" (default; blocks the call), "error" or "end".
	ExitBehavior string
}

func (ToolCallLimit) Name() string { return "tool_call_limit" }
func (ToolCallLimit) StateSchema() graph.Schema {
	return graph.Schema{"thread_tool_call_count": graph.Sum(), "run_tool_call_count": channels.LastValue()}
}
func (ToolCallLimit) BeforeAgent(context.Context, graph.State) (any, error) {
	return graph.State{"run_tool_call_count": 0}, nil
}

// AfterModel counts the tool calls the model just requested and blocks the
// excess ones by replacing them with error tool messages.
func (m ToolCallLimit) AfterModel(_ context.Context, st graph.State) (any, error) {
	msgs, _ := messages.Coerce(st["messages"])
	last, ok := messages.LastOfRole(msgs, messages.RoleAI)
	if !ok || len(last.ToolCalls) == 0 {
		return nil, nil
	}
	t, r := asInt(st["thread_tool_call_count"]), asInt(st["run_tool_call_count"])
	var keep []messages.ToolCall
	var blocked []messages.Message
	counted := 0
	for _, c := range last.ToolCalls {
		if m.ToolName != "" && c.Name != m.ToolName {
			keep = append(keep, c)
			continue
		}
		if (m.ThreadLimit > 0 && t+counted >= m.ThreadLimit) || (m.RunLimit > 0 && r+counted >= m.RunLimit) {
			if m.ExitBehavior == "error" {
				return nil, &LimitError{fmt.Sprintf("tool call limit exceeded for %q", c.Name)}
			}
			e := messages.Tool(fmt.Sprintf("Tool call limit exceeded. Do not call %q again.", c.Name), c.ID)
			e.Name, e.Status = c.Name, "error"
			blocked = append(blocked, e)
			keep = append(keep, c)
			continue
		}
		counted++
		keep = append(keep, c)
	}
	upd := graph.State{"thread_tool_call_count": counted, "run_tool_call_count": r + counted}
	if len(blocked) > 0 {
		// answer blocked calls directly; allowed ones still run in ToolNode
		upd["messages"] = blocked
		if counted == 0 {
			upd["jump_to"] = agents.JumpModel
			if m.ExitBehavior == "end" {
				upd["jump_to"] = agents.JumpEnd
			}
		}
	}
	return upd, nil
}
