package agentprotocol

import (
	"encoding/json"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// roleOf maps protocol/LangChain role spellings to a graph role.
func roleOf(r string) messages.Role {
	switch strings.ToLower(r) {
	case "user", "human":
		return messages.RoleHuman
	case "assistant", "ai":
		return messages.RoleAI
	case "system":
		return messages.RoleSystem
	case "tool":
		return messages.RoleTool
	}
	return messages.Role(r)
}

// contentText flattens string or block content to text.
func contentText(c any) string {
	switch x := c.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, b := range x {
			if m, ok := b.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "")
	case []ContentBlock:
		var parts []string
		for _, b := range x {
			parts = append(parts, b.Text)
		}
		return strings.Join(parts, "")
	}
	return ""
}

// toGraphMessages converts protocol messages. Non-text content blocks are
// reduced to their text; tool messages need metadata.tool_call_id.
func toGraphMessages(in []Message) []messages.Message {
	out := make([]messages.Message, 0, len(in))
	for _, m := range in {
		gm := messages.Message{ID: m.ID, Role: roleOf(m.Role), Content: contentText(m.Content)}
		if gm.Role == messages.RoleTool {
			gm.ToolCallID, _ = m.Metadata["tool_call_id"].(string)
		}
		if n, ok := m.Metadata["name"].(string); ok {
			gm.Name = n
		}
		out = append(out, gm)
	}
	return out
}

// fromGraphMessage converts one message to the protocol shape.
func fromGraphMessage(m messages.Message) Message {
	pm := Message{Role: string(m.Role), Content: m.Text(), ID: m.ID}
	md := map[string]any{}
	if m.Name != "" {
		md["name"] = m.Name
	}
	if m.ToolCallID != "" {
		md["tool_call_id"] = m.ToolCallID
	}
	if len(m.ToolCalls) > 0 {
		md["tool_calls"] = m.ToolCalls
	}
	if len(md) > 0 {
		pm.Metadata = md
	}
	return pm
}

func fromGraphMessages(ms []messages.Message) []Message {
	out := make([]Message, 0, len(ms))
	for _, m := range ms {
		if m.Role == messages.RoleRemove {
			continue
		}
		out = append(out, fromGraphMessage(m))
	}
	return out
}

// jsonState makes a graph state safe and idiomatic to serialise: message
// lists become protocol messages and interrupts become plain objects.
func jsonState(s graph.State) map[string]any {
	out := make(map[string]any, len(s))
	for k, v := range s {
		switch x := v.(type) {
		case []messages.Message:
			out[k] = fromGraphMessages(x)
		case []graph.InterruptInfo:
			list := make([]map[string]any, len(x))
			for i, it := range x {
				list[i] = map[string]any{"id": it.ID, "node": it.Node, "value": it.Value}
			}
			out[k] = list
		default:
			out[k] = v
		}
	}
	return out
}

// messagesOf extracts the protocol messages of a state's "messages" key.
func messagesOf(s graph.State) []Message {
	ms, err := messages.Coerce(s["messages"])
	if err != nil || len(ms) == 0 {
		return nil
	}
	return fromGraphMessages(ms)
}

// buildInput turns a run request body into graph input. A string becomes one
// human message, an array is a message list, an object is the state input;
// body.Messages are appended to the "messages" key.
func buildInput(raw json.RawMessage, extra []Message) (graph.State, error) {
	in := graph.State{}
	if len(raw) > 0 && string(raw) != "null" {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				in[k] = val
			}
		case string:
			in["messages"] = []messages.Message{messages.Human(x)}
		case []any:
			b, _ := json.Marshal(x)
			var ms []Message
			if err := json.Unmarshal(b, &ms); err != nil {
				return nil, err
			}
			in["messages"] = toGraphMessages(ms)
		default:
			in["input"] = v
		}
	}
	if len(extra) > 0 {
		cur, _ := messages.Coerce(in["messages"])
		in["messages"] = append(cur, toGraphMessages(extra)...)
	}
	return in, nil
}

func decodeMessages(v any) ([]Message, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var ms []Message
	return ms, json.Unmarshal(b, &ms)
}
