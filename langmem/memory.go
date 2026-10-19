package langmem

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
)

// Schema describes one kind of memory the extractor may write. Each schema is
// offered to the model as a tool (the way LangMem uses Pydantic models), so a
// manager can mix kinds: a UserProfile, Episodes, plain facts...
type Schema struct {
	// Name is the memory kind stored in the item ("Memory", "Episode"...).
	Name string
	// Description tells the model when to use this kind.
	Description string
	// Parameters is the JSON Schema of the content.
	Parameters *jsonschema.Schema
}

// SchemaFor reflects the schema of T (a struct), like passing a Pydantic
// model to create_memory_manager(schemas=[...]).
func SchemaFor[T any](name, description string) Schema {
	s := jsonschema.For[T]()
	s.Title = ""
	return Schema{Name: name, Description: description, Parameters: s}
}

// MemorySchema is the default kind: a free-text note, stored as
// {"content": "..."}.
var MemorySchema = SchemaFor[struct {
	Content string `json:"content" description:"The memory as a well-written, standalone episode/fact/note/preference/etc. Refer to the user's instructions for more information the preferred memory organization."`
}]("Memory", "Call this tool once for each new memory you want to record. Use multi-tool calling to record multiple new memories.")

// Episode is the episodic-memory schema: how a situation was handled and why
// it worked (from the LangMem docs).
type Episode struct {
	Observation string `json:"observation" description:"The situation and relevant context"`
	Thoughts    string `json:"thoughts" description:"Key considerations and reasoning process"`
	Action      string `json:"action" description:"What was done in response"`
	Result      string `json:"result" description:"What happened and why it worked"`
}

// EpisodeSchema is SchemaFor[Episode].
var EpisodeSchema = SchemaFor[Episode]("Episode", "An episode captures how to handle a specific situation, including the reasoning process and what made it successful.")

// Triple is a knowledge-graph style semantic fact.
type Triple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	Context   string `json:"context,omitempty"`
}

// TripleSchema is SchemaFor[Triple].
var TripleSchema = SchemaFor[Triple]("Triple", "A subject-predicate-object fact with optional context.")

// Extracted is a memory as seen by a Manager: input (existing memories) and
// output (ExtractedMemory). Content is a JSON-like value (usually a map)
// shaped by the schema named Kind.
type Extracted struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Content is {"content": "..."} for the default schema.
	Content any `json:"content"`
	// Deleted marks an existing memory the manager removed (the RemoveDoc of
	// LangMem). Content is then nil.
	Deleted bool `json:"deleted,omitempty"`
}

// Note builds a default-schema memory from text, for seeding Manager input.
func Note(id, text string) Extracted {
	return Extracted{ID: id, Kind: MemorySchema.Name, Content: map[string]any{"content": text}}
}

// Decode converts a memory content to T. For the default schema use
// Decode[struct{ Content string }] or Text.
func Decode[T any](content any) (T, error) {
	var v T
	b, err := json.Marshal(content)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, fmt.Errorf("langmem: cannot decode %s into %v: %w", b, reflect.TypeOf(v), err)
	}
	return v, nil
}

// Text returns the note text of a default-schema content (or its JSON
// otherwise).
func Text(content any) string {
	if m, ok := content.(map[string]any); ok {
		if s, ok := m["content"].(string); ok && len(m) == 1 {
			return s
		}
	}
	return TextOf(content)
}

// normalize coerces stored values to the {"content": map} convention: bare
// strings (written by the manage_memory tool) become {"content": s}.
func normalize(content any) any {
	if s, ok := content.(string); ok {
		return map[string]any{"content": s}
	}
	return content
}
