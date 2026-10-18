package messages

import (
	"fmt"
	"slices"
)

// Coerce converts supported values into a message list. Accepted inputs:
// Message, []Message, string (human message), map[string]any with
// "role"/"content" keys, []any of the above, and [2]string{role, content}
// tuples (like ("user", "hi") in Python).
func Coerce(v any) ([]Message, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case Message:
		return []Message{x}, nil
	case *Message:
		return []Message{*x}, nil
	case []Message:
		return x, nil
	case string:
		return []Message{Human(x)}, nil
	case [2]string:
		return []Message{{Role: normalizeRole(x[0]), Content: x[1]}}, nil
	case map[string]any:
		role, _ := x["role"].(string)
		if role == "" {
			role, _ = x["type"].(string)
		}
		content, _ := x["content"].(string)
		m := Message{Role: normalizeRole(role), Content: content}
		m.ID, _ = x["id"].(string)
		m.Name, _ = x["name"].(string)
		m.ToolCallID, _ = x["tool_call_id"].(string)
		return []Message{m}, nil
	case []any:
		var out []Message
		for _, e := range x {
			ms, err := Coerce(e)
			if err != nil {
				return nil, err
			}
			out = append(out, ms...)
		}
		return out, nil
	}
	return nil, fmt.Errorf("messages: cannot coerce %T to messages", v)
}

func normalizeRole(r string) Role {
	switch r {
	case "user", "human":
		return RoleHuman
	case "assistant", "ai":
		return RoleAI
	case "system", "developer":
		return RoleSystem
	case "tool", "function":
		return RoleTool
	case "remove":
		return RoleRemove
	}
	return Role(r)
}

// AddMessages is the LangGraph add_messages reducer. Messages in right with
// an ID already present in left replace it; RemoveMessages delete by ID;
// Remove(RemoveAll) discards everything before it. Missing IDs are assigned.
func AddMessages(left, right []Message) ([]Message, error) {
	merged := make([]Message, len(left))
	copy(merged, left)
	for i := range merged {
		if merged[i].ID == "" {
			merged[i].ID = NewID()
		}
	}
	incoming := make([]Message, len(right))
	copy(incoming, right)
	removeAll := -1
	for i := range incoming {
		if incoming[i].ID == "" {
			incoming[i].ID = NewID()
		}
		if incoming[i].Role == RoleRemove && incoming[i].ID == RemoveAll {
			removeAll = i
		}
	}
	if removeAll >= 0 {
		return slices.Clone(incoming[removeAll+1:]), nil
	}
	byID := make(map[string]int, len(merged))
	for i, m := range merged {
		byID[m.ID] = i
	}
	remove := map[string]bool{}
	for _, m := range incoming {
		if idx, ok := byID[m.ID]; ok {
			if m.Role == RoleRemove {
				remove[m.ID] = true
			} else {
				delete(remove, m.ID)
				merged[idx] = m
			}
			continue
		}
		if m.Role == RoleRemove {
			return nil, fmt.Errorf("attempting to delete a message with an ID that doesn't exist (%q)", m.ID)
		}
		byID[m.ID] = len(merged)
		merged = append(merged, m)
	}
	if len(remove) == 0 {
		return merged, nil
	}
	out := merged[:0]
	for _, m := range merged {
		if !remove[m.ID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// Filter keeps messages whose role is in include (all when empty) and whose
// role is not in exclude (filter_messages).
func Filter(msgs []Message, include, exclude []Role) []Message {
	var out []Message
	for _, m := range msgs {
		if len(include) > 0 && !slices.Contains(include, m.Role) {
			continue
		}
		if slices.Contains(exclude, m.Role) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// TokenCounter estimates the token count of a message list.
type TokenCounter func([]Message) int

// ApproxTokens is a cheap counter: ~4 characters per token plus overhead.
func ApproxTokens(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += 3 + len(m.Text())/4
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(fmt.Sprint(tc.Args))/4
		}
	}
	return n
}

// TrimOptions configures Trim (trim_messages).
type TrimOptions struct {
	MaxTokens     int
	Counter       TokenCounter // defaults to ApproxTokens
	Strategy      string       // "last" (default) or "first"
	IncludeSystem bool         // keep a leading system message when Strategy=="last"
	StartOn       Role         // after trimming, drop leading messages until this role
}

// Trim drops messages until the list fits MaxTokens.
func Trim(msgs []Message, o TrimOptions) []Message {
	count := o.Counter
	if count == nil {
		count = ApproxTokens
	}
	if count(msgs) <= o.MaxTokens {
		return slices.Clone(msgs)
	}
	if o.Strategy == "first" {
		var out []Message
		for _, m := range msgs {
			if count(append(out, m)) > o.MaxTokens {
				break
			}
			out = append(out, m)
		}
		return out
	}
	var system []Message
	rest := msgs
	if o.IncludeSystem && len(msgs) > 0 && msgs[0].Role == RoleSystem {
		system, rest = msgs[:1], msgs[1:]
	}
	var tail []Message
	for i := len(rest) - 1; i >= 0; i-- {
		cand := append([]Message{rest[i]}, tail...)
		if count(append(slices.Clone(system), cand...)) > o.MaxTokens {
			break
		}
		tail = cand
	}
	if o.StartOn != "" {
		for len(tail) > 0 && tail[0].Role != o.StartOn {
			tail = tail[1:]
		}
	}
	return append(slices.Clone(system), tail...)
}

// MergeRuns joins consecutive messages of the same role (merge_message_runs).
func MergeRuns(msgs []Message) []Message {
	var out []Message
	for _, m := range msgs {
		if n := len(out); n > 0 && out[n-1].Role == m.Role && m.Role != RoleTool {
			prev := &out[n-1]
			if prev.Content != "" && m.Content != "" {
				prev.Content += "\n"
			}
			prev.Content += m.Content
			prev.ToolCalls = append(prev.ToolCalls, m.ToolCalls...)
			continue
		}
		out = append(out, m.Clone())
	}
	return out
}
