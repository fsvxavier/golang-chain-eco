package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Decision types a reviewer may answer with.
const (
	Approve = "approve"
	Edit    = "edit"
	Reject  = "reject"
)

// ToolPolicy configures review of one tool.
type ToolPolicy struct {
	// Allowed decisions; empty means all three.
	Allowed     []string
	Description string
}

// ActionRequest is one tool call awaiting review.
type ActionRequest struct {
	Name        string         `json:"name"`
	Args        map[string]any `json:"args"`
	Description string         `json:"description,omitempty"`
}

// HITLRequest is the interrupt payload.
type HITLRequest struct {
	ActionRequests []ActionRequest     `json:"action_requests"`
	ReviewConfigs  map[string][]string `json:"review_configs"` // tool -> allowed decisions
}

// HITLDecision answers one action request.
type HITLDecision struct {
	Type         string         `json:"type"`
	EditedAction *ActionRequest `json:"edited_action,omitempty"`
	Message      string         `json:"message,omitempty"`
}

// HITLResponse is the resume value: Command{Resume: HITLResponse{...}} (a
// plain []HITLDecision or the equivalent JSON-shaped value also works).
type HITLResponse struct {
	Decisions []HITLDecision `json:"decisions"`
}

// HumanInTheLoop pauses before sensitive tool calls run and lets a human
// approve, edit or reject them (HumanInTheLoopMiddleware). Requires a
// checkpointer.
type HumanInTheLoop struct {
	// Tools maps tool names to policies; unlisted tools run unreviewed.
	Tools map[string]ToolPolicy
}

func (HumanInTheLoop) Name() string { return "human_in_the_loop" }

func (h HumanInTheLoop) AfterModel(ctx context.Context, st graph.State) (any, error) {
	msgs, _ := messages.Coerce(st["messages"])
	last, ok := messages.LastOfRole(msgs, messages.RoleAI)
	if !ok || len(last.ToolCalls) == 0 {
		return nil, nil
	}
	var idx []int
	req := HITLRequest{ReviewConfigs: map[string][]string{}}
	for i, c := range last.ToolCalls {
		p, ok := h.Tools[c.Name]
		if !ok {
			continue
		}
		allowed := p.Allowed
		if len(allowed) == 0 {
			allowed = []string{Approve, Edit, Reject}
		}
		desc := p.Description
		if desc == "" {
			desc = fmt.Sprintf("Tool execution requires approval\n\nTool: %s\nArgs: %v", c.Name, c.Args)
		}
		idx = append(idx, i)
		req.ActionRequests = append(req.ActionRequests, ActionRequest{Name: c.Name, Args: c.Args, Description: desc})
		req.ReviewConfigs[c.Name] = allowed
	}
	if len(idx) == 0 {
		return nil, nil
	}
	decisions, err := decode(graph.Interrupt(ctx, req))
	if err != nil {
		return nil, err
	}
	if len(decisions) != len(idx) {
		return nil, fmt.Errorf("hitl: expected %d decisions, got %d", len(idx), len(decisions))
	}
	calls := slices.Clone(last.ToolCalls)
	var extra []messages.Message
	for k, d := range decisions {
		i := idx[k]
		c := calls[i]
		if allowed := req.ReviewConfigs[c.Name]; !slices.Contains(allowed, d.Type) {
			return nil, fmt.Errorf("hitl: decision %q not allowed for tool %q", d.Type, c.Name)
		}
		switch d.Type {
		case Approve:
		case Edit:
			if d.EditedAction == nil {
				return nil, fmt.Errorf("hitl: edit decision for %q lacks edited_action", c.Name)
			}
			calls[i] = messages.ToolCall{ID: c.ID, Name: d.EditedAction.Name, Args: d.EditedAction.Args}
		case Reject:
			text := d.Message
			if text == "" {
				text = fmt.Sprintf("User rejected the tool call for `%s` with id %s", c.Name, c.ID)
			}
			m := messages.Tool(text, c.ID)
			m.Name, m.Status = c.Name, "error"
			extra = append(extra, m)
		default:
			return nil, fmt.Errorf("hitl: unknown decision type %q", d.Type)
		}
	}
	upd := last
	upd.ToolCalls = calls
	out := graph.State{"messages": append([]messages.Message{upd}, extra...)}
	if len(extra) > 0 && len(extra) == len(calls) {
		out["jump_to"] = agents.JumpModel
	}
	return out, nil
}

func decode(v any) ([]HITLDecision, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var r HITLResponse
	if json.Unmarshal(b, &r) == nil && r.Decisions != nil {
		return r.Decisions, nil
	}
	var ds []HITLDecision
	if err := json.Unmarshal(b, &ds); err != nil {
		return nil, fmt.Errorf("hitl: cannot decode resume value: %w", err)
	}
	return ds, nil
}
