// Package messages defines the chat message model shared by every component of
// golang-chain-eco (the Go port of langchain_core.messages).
//
// A single Message struct covers the Python classes SystemMessage,
// HumanMessage, AIMessage, ToolMessage and RemoveMessage; the Role field
// selects which one it is. Streaming chunks (AIMessageChunk) are also plain
// Messages that carry ToolCallChunks and are merged with Merge.
package messages

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Role identifies the kind of message.
type Role string

const (
	RoleSystem Role = "system"
	RoleHuman  Role = "human"
	RoleAI     Role = "ai"
	RoleTool   Role = "tool"
	// RoleRemove marks a RemoveMessage: when passed through AddMessages it
	// deletes the message with the same ID.
	RoleRemove Role = "remove"
)

// RemoveAll is the special ID that, used in a RemoveMessage, clears the whole
// message list (REMOVE_ALL_MESSAGES in LangGraph).
const RemoveAll = "__remove_all__"

// ContentPart is one block of multimodal content.
type ContentPart struct {
	Type     string         `json:"type"` // "text", "image_url", "image", "file", ...
	Text     string         `json:"text,omitempty"`
	URL      string         `json:"url,omitempty"`
	Data     string         `json:"data,omitempty"` // base64 payload
	MimeType string         `json:"mime_type,omitempty"`
	Extra    map[string]any `json:"extra,omitempty"` // provider specific (e.g. cache_control)
}

// ToolCall is a request from the model to run a tool.
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// ToolCallChunk is a partial tool call produced while streaming.
type ToolCallChunk struct {
	Index int    `json:"index"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Args  string `json:"args,omitempty"` // raw, possibly incomplete JSON
}

// InvalidToolCall is a tool call whose arguments could not be parsed.
type InvalidToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Args  string `json:"args"`
	Error string `json:"error"`
}

// Usage holds token accounting returned by providers.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Add returns the element-wise sum of two usages (either may be nil).
func (u *Usage) Add(o *Usage) *Usage {
	if u == nil {
		return o
	}
	if o == nil {
		return u
	}
	return &Usage{u.InputTokens + o.InputTokens, u.OutputTokens + o.OutputTokens, u.TotalTokens + o.TotalTokens}
}

// Message is a chat message of any role.
type Message struct {
	ID               string            `json:"id,omitempty"`
	Role             Role              `json:"role"`
	Content          string            `json:"content"`
	Parts            []ContentPart     `json:"parts,omitempty"`
	Name             string            `json:"name,omitempty"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallChunks   []ToolCallChunk   `json:"tool_call_chunks,omitempty"`
	InvalidToolCalls []InvalidToolCall `json:"invalid_tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	// Status of a tool message: "success" (default) or "error".
	Status           string         `json:"status,omitempty"`
	Artifact         any            `json:"artifact,omitempty"`
	Usage            *Usage         `json:"usage,omitempty"`
	ResponseMetadata map[string]any `json:"response_metadata,omitempty"`
	AdditionalKwargs map[string]any `json:"additional_kwargs,omitempty"`
}

// System builds a system message.
func System(content string) Message { return Message{Role: RoleSystem, Content: content} }

// Human builds a human (user) message.
func Human(content string) Message { return Message{Role: RoleHuman, Content: content} }

// AI builds an assistant message, optionally with tool calls.
func AI(content string, calls ...ToolCall) Message {
	return Message{Role: RoleAI, Content: content, ToolCalls: calls}
}

// Tool builds a tool result message answering toolCallID.
func Tool(content, toolCallID string) Message {
	return Message{Role: RoleTool, Content: content, ToolCallID: toolCallID}
}

// Remove builds a RemoveMessage for id (use RemoveAll to clear everything).
func Remove(id string) Message { return Message{Role: RoleRemove, ID: id} }

// NewID returns a random message identifier.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Text returns the textual content, joining text parts when Content is empty.
func (m Message) Text() string {
	if m.Content != "" || len(m.Parts) == 0 {
		return m.Content
	}
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// Type returns the LangChain message type name.
func (m Message) Type() string { return string(m.Role) }

// String renders the message the way pretty_print does.
func (m Message) String() string {
	var sb strings.Builder
	title := map[Role]string{RoleSystem: "System Message", RoleHuman: "Human Message", RoleAI: "Ai Message", RoleTool: "Tool Message", RoleRemove: "Remove Message"}[m.Role]
	pad := strings.Repeat("=", (80-len(title)-2)/2)
	fmt.Fprintf(&sb, "%s %s %s\n", pad, title, pad)
	if m.Name != "" {
		fmt.Fprintf(&sb, "Name: %s\n", m.Name)
	}
	if m.Text() != "" {
		fmt.Fprintf(&sb, "\n%s\n", m.Text())
	}
	if len(m.ToolCalls) > 0 {
		sb.WriteString("Tool Calls:\n")
		for _, tc := range m.ToolCalls {
			args, _ := json.Marshal(tc.Args)
			fmt.Fprintf(&sb, "  %s (%s)\n Call ID: %s\n  Args: %s\n", tc.Name, tc.ID, tc.ID, args)
		}
	}
	return sb.String()
}

// Merge concatenates a streaming chunk onto m (AIMessageChunk.__add__).
// Content is appended, tool call chunks are merged by index and metadata is
// overlaid. Call Finalize to turn accumulated chunks into ToolCalls.
func (m Message) Merge(c Message) Message {
	if m.Role == "" {
		m.Role = c.Role
	}
	if m.ID == "" {
		m.ID = c.ID
	}
	m.Content += c.Content
	m.Parts = append(m.Parts, c.Parts...)
	for _, tc := range c.ToolCallChunks {
		found := false
		for i := range m.ToolCallChunks {
			if m.ToolCallChunks[i].Index == tc.Index {
				if tc.ID != "" {
					m.ToolCallChunks[i].ID = tc.ID
				}
				m.ToolCallChunks[i].Name += tc.Name
				m.ToolCallChunks[i].Args += tc.Args
				found = true
				break
			}
		}
		if !found {
			m.ToolCallChunks = append(m.ToolCallChunks, tc)
		}
	}
	m.ToolCalls = append(m.ToolCalls, c.ToolCalls...)
	m.Usage = m.Usage.Add(c.Usage)
	if len(c.ResponseMetadata) > 0 {
		if m.ResponseMetadata == nil {
			m.ResponseMetadata = map[string]any{}
		}
		for k, v := range c.ResponseMetadata {
			m.ResponseMetadata[k] = v
		}
	}
	return m
}

// Finalize converts accumulated ToolCallChunks into ToolCalls (or
// InvalidToolCalls when the JSON does not parse).
func (m Message) Finalize() Message {
	for _, c := range m.ToolCallChunks {
		args := map[string]any{}
		if strings.TrimSpace(c.Args) != "" {
			if err := json.Unmarshal([]byte(c.Args), &args); err != nil {
				m.InvalidToolCalls = append(m.InvalidToolCalls, InvalidToolCall{ID: c.ID, Name: c.Name, Args: c.Args, Error: err.Error()})
				continue
			}
		}
		m.ToolCalls = append(m.ToolCalls, ToolCall{ID: c.ID, Name: c.Name, Args: args})
	}
	m.ToolCallChunks = nil
	return m
}

// Clone returns a deep-enough copy (slices are copied, maps are shared).
func (m Message) Clone() Message {
	m.Parts = append([]ContentPart(nil), m.Parts...)
	m.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
	m.ToolCallChunks = append([]ToolCallChunk(nil), m.ToolCallChunks...)
	return m
}

// Last returns the last message and true, or false when msgs is empty.
func Last(msgs []Message) (Message, bool) {
	if len(msgs) == 0 {
		return Message{}, false
	}
	return msgs[len(msgs)-1], true
}

// LastOfRole returns the most recent message with the given role.
func LastOfRole(msgs []Message, r Role) (Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == r {
			return msgs[i], true
		}
	}
	return Message{}, false
}

// Buffer renders messages as "Role: content" lines (get_buffer_string).
func Buffer(msgs []Message) string {
	prefix := map[Role]string{RoleSystem: "System", RoleHuman: "Human", RoleAI: "AI", RoleTool: "Tool"}
	var lines []string
	for _, m := range msgs {
		lines = append(lines, prefix[m.Role]+": "+m.Text())
	}
	return strings.Join(lines, "\n")
}
