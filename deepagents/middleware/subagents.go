package middleware

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

// SubAgent is a compiled agent the main agent can delegate to via `task`.
type SubAgent struct {
	Name        string
	Description string
	Agent       *graph.Compiled
}

// Subagents adds the `task` tool: it runs a fresh subagent on a description
// and returns only its final answer, keeping the parent context small
// (SubAgentMiddleware). The virtual filesystem is shared with the subagent.
type Subagents struct{ Agents []SubAgent }

func (Subagents) Name() string { return "subagents" }

const taskDescription = `Launch an ephemeral subagent to handle a complex, multi-step task with isolated context.

Available agent types:
%s

Usage notes:
- Launch multiple subagents concurrently when tasks are independent.
- The subagent returns a single final message; it is not visible to the user.
- Provide a detailed, self-contained task description: subagents are stateless.`

func (s Subagents) Tools() []tools.Tool {
	byName := map[string]SubAgent{}
	var lines []string
	for _, a := range s.Agents {
		byName[a.Name] = a
		lines = append(lines, fmt.Sprintf("- %s: %s", a.Name, a.Description))
	}
	sort.Strings(lines)
	type args struct {
		Description  string `json:"description" description:"Detailed task for the subagent"`
		SubagentType string `json:"subagent_type" description:"Name of the subagent to use"`
	}
	return []tools.Tool{tools.New("task", fmt.Sprintf(taskDescription, strings.Join(lines, "\n")),
		func(ctx context.Context, a args) (any, error) {
			sub, ok := byName[a.SubagentType]
			if !ok {
				return nil, tools.Errorf("unknown subagent type %q; available: %s", a.SubagentType, strings.Join(names(s.Agents), ", "))
			}
			in := graph.State{"messages": []messages.Message{messages.Human(a.Description)}}
			if st := prebuilt.StateOf(ctx); st != nil {
				in["files"] = backends.FilesUpdateFrom(backends.FilesFromState(st))
			}
			out, err := sub.Agent.Invoke(ctx, in)
			if err != nil {
				return nil, tools.Errorf("subagent %s failed: %v", sub.Name, err)
			}
			if len(graph.Interrupts(out)) > 0 {
				return nil, tools.Errorf("subagent %s paused for input, which is unsupported inside task", sub.Name)
			}
			msgs, _ := messages.Coerce(out["messages"])
			last, ok := messages.LastOfRole(msgs, messages.RoleAI)
			if !ok {
				return nil, tools.Errorf("subagent %s returned no answer", sub.Name)
			}
			m := messages.Tool(strings.TrimSpace(last.Text()), tools.CallID(ctx))
			m.Name = "task"
			upd := graph.State{"messages": []messages.Message{m}}
			if f, ok := out["files"].(map[string]backends.FileData); ok {
				upd["files"] = backends.FilesUpdateFrom(f)
			}
			return graph.Command{Update: upd}, nil
		})}
}

func names(as []SubAgent) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Name
	}
	return out
}

func (s Subagents) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	r := *req
	r.System = strings.TrimSpace(req.System + "\n\n## `task` (subagent spawner)\nDelegate complex, independent or context-heavy work to subagents with the task tool; run independent ones in parallel.")
	return next(ctx, &r)
}
