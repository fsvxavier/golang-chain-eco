package agentprotocol

import (
	"encoding/json"
	"time"
)

// Wire types of Agent Protocol v0.1.6 (github.com/langchain-ai/agent-protocol).
// JSON field names are the spec's.

// Run statuses.
const (
	RunPending     = "pending"
	RunError       = "error"
	RunSuccess     = "success"
	RunTimeout     = "timeout"
	RunInterrupted = "interrupted"
)

// Thread statuses.
const (
	ThreadIdle        = "idle"
	ThreadBusy        = "busy"
	ThreadInterrupted = "interrupted"
	ThreadError       = "error"
)

// Capabilities advertises optional protocol features of an agent.
type Capabilities struct {
	Messages  bool `json:"ap.io.messages,omitempty"`
	Streaming bool `json:"ap.io.streaming,omitempty"`
}

// Agent is the introspection record of GET /agents/{id}.
type Agent struct {
	AgentID      string         `json:"agent_id"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Capabilities Capabilities   `json:"capabilities"`
}

// AgentSchema is the JSON Schema bundle of GET /agents/{id}/schemas.
type AgentSchema struct {
	AgentID      string         `json:"agent_id"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`
	StateSchema  map[string]any `json:"state_schema,omitempty"`
	ConfigSchema map[string]any `json:"config_schema,omitempty"`
}

// ContentBlock is one block of structured message content.
type ContentBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Message is the protocol's chat message. Content is a string or []ContentBlock.
type Message struct {
	Role     string         `json:"role"`
	Content  any            `json:"content"`
	ID       string         `json:"id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ThreadCreate is the body of POST /threads.
type ThreadCreate struct {
	ThreadID string         `json:"thread_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	IfExists string         `json:"if_exists,omitempty"` // raise | do_nothing
}

// Thread is the protocol thread resource.
type Thread struct {
	ThreadID  string         `json:"thread_id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Metadata  map[string]any `json:"metadata"`
	Status    string         `json:"status"`
	Values    map[string]any `json:"values,omitempty"`
	Messages  []Message      `json:"messages,omitempty"`
}

// ThreadCheckpoint names one revision of a thread.
type ThreadCheckpoint struct {
	CheckpointID string `json:"checkpoint_id"`
}

// ThreadState is one revision of a thread's state.
type ThreadState struct {
	Checkpoint ThreadCheckpoint `json:"checkpoint"`
	Values     map[string]any   `json:"values"`
	Messages   []Message        `json:"messages,omitempty"`
	Metadata   map[string]any   `json:"metadata,omitempty"`
}

// ThreadPatch is the body of PATCH /threads/{id}.
type ThreadPatch struct {
	Checkpoint *ThreadCheckpoint `json:"checkpoint,omitempty"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	Values     map[string]any    `json:"values,omitempty"`
	Messages   []Message         `json:"messages,omitempty"`
}

// ThreadSearchRequest is the body of POST /threads/search.
type ThreadSearchRequest struct {
	Metadata map[string]any `json:"metadata,omitempty"`
	Values   map[string]any `json:"values,omitempty"`
	Status   string         `json:"status,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	Offset   int            `json:"offset,omitempty"`
}

// RunConfig is the per-run configuration.
type RunConfig struct {
	Tags           []string       `json:"tags,omitempty"`
	RecursionLimit int            `json:"recursion_limit,omitempty"`
	Configurable   map[string]any `json:"configurable,omitempty"`
}

// RunCreate is the body of POST /runs, /runs/wait and /runs/stream.
type RunCreate struct {
	ThreadID     string          `json:"thread_id,omitempty"`
	AgentID      string          `json:"agent_id,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Messages     []Message       `json:"messages,omitempty"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
	Config       *RunConfig      `json:"config,omitempty"`
	Webhook      string          `json:"webhook,omitempty"`
	OnCompletion string          `json:"on_completion,omitempty"` // delete | keep
	OnDisconnect string          `json:"on_disconnect,omitempty"` // cancel | continue
	IfNotExists  string          `json:"if_not_exists,omitempty"` // create | reject
	StreamMode   StreamModes     `json:"stream_mode,omitempty"`
}

// StreamModes accepts a string or an array, as the spec allows.
type StreamModes []string

// UnmarshalJSON implements json.Unmarshaler.
func (s *StreamModes) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*s = StreamModes{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

// Run is the protocol run resource.
type Run struct {
	RunID     string         `json:"run_id"`
	ThreadID  string         `json:"thread_id,omitempty"`
	AgentID   string         `json:"agent_id,omitempty"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Error     string         `json:"error,omitempty"`
}

// RunSearchRequest is the body of POST /runs/search.
type RunSearchRequest struct {
	Metadata map[string]any `json:"metadata,omitempty"`
	Status   string         `json:"status,omitempty"`
	ThreadID string         `json:"thread_id,omitempty"`
	AgentID  string         `json:"agent_id,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	Offset   int            `json:"offset,omitempty"`
}

// RunWaitResponse is the result of waiting for a run.
type RunWaitResponse struct {
	Run      *Run           `json:"run,omitempty"`
	Values   map[string]any `json:"values,omitempty"`
	Messages []Message      `json:"messages,omitempty"`
}

// Item is a long-term store item.
type Item struct {
	Namespace []string       `json:"namespace"`
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// StorePutRequest is the body of PUT /store/items.
type StorePutRequest struct {
	Namespace []string       `json:"namespace"`
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
}

// StoreDeleteRequest is the body of DELETE /store/items.
type StoreDeleteRequest struct {
	Namespace []string `json:"namespace,omitempty"`
	Key       string   `json:"key"`
}

// StoreSearchRequest is the body of POST /store/items/search.
type StoreSearchRequest struct {
	NamespacePrefix []string       `json:"namespace_prefix,omitempty"`
	Filter          map[string]any `json:"filter,omitempty"`
	Limit           int            `json:"limit,omitempty"`
	Offset          int            `json:"offset,omitempty"`
}

// SearchItemsResponse is the result of a store search.
type SearchItemsResponse struct {
	Items []Item `json:"items"`
}

// StoreListNamespacesRequest is the body of POST /store/namespaces.
type StoreListNamespacesRequest struct {
	Prefix   []string `json:"prefix,omitempty"`
	Suffix   []string `json:"suffix,omitempty"`
	MaxDepth int      `json:"max_depth,omitempty"`
	Limit    int      `json:"limit,omitempty"`
	Offset   int      `json:"offset,omitempty"`
}

// ErrorResponse is the protocol error body.
type ErrorResponse struct {
	Code     string         `json:"code,omitempty"`
	Message  string         `json:"message,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (e *ErrorResponse) Error() string { return e.Code + ": " + e.Message }

// SearchAgentsRequest is the body of POST /agents/search.
type SearchAgentsRequest struct {
	Name     string         `json:"name,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	Offset   int            `json:"offset,omitempty"`
}
