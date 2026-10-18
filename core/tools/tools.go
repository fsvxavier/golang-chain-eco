// Package tools defines the Tool abstraction (langchain_core.tools): a named
// function with a JSON Schema for its arguments that a chat model can call.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
)

// Tool is something an agent can call.
type Tool interface {
	Name() string
	Description() string
	// Schema describes the arguments object.
	Schema() *jsonschema.Schema
	// Call runs the tool. The result may be a string, any JSON-serialisable
	// value, a ContentAndArtifact, or a graph Command (handled by ToolNode).
	Call(ctx context.Context, args map[string]any) (any, error)
}

// ReturnDirect is implemented by tools whose result should end the agent loop.
type ReturnDirect interface{ ReturnDirect() bool }

// Definition is the provider-neutral description sent to chat models.
type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Define converts a tool to its Definition.
func Define(t Tool) Definition {
	s := t.Schema()
	if s == nil {
		s = &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
	}
	p := s.Map()
	delete(p, "title")
	if _, ok := p["properties"]; !ok {
		p["properties"] = map[string]any{}
	}
	return Definition{Name: t.Name(), Description: t.Description(), Parameters: p}
}

// DefineAll converts several tools.
func DefineAll(ts []Tool) []Definition {
	out := make([]Definition, len(ts))
	for i, t := range ts {
		out[i] = Define(t)
	}
	return out
}

// ContentAndArtifact lets a tool return content for the model plus an
// artifact kept on the ToolMessage but not sent to the model.
type ContentAndArtifact struct {
	Content  any
	Artifact any
}

// Error is a tool failure whose message should be shown to the model
// instead of aborting the run (ToolException).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Errorf builds a tool Error.
func Errorf(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

// IsToolError reports whether err is (or wraps) a tool Error.
func IsToolError(err error) bool {
	var te *Error
	return errors.As(err, &te)
}

// Option customises a function tool.
type Option func(*funcTool)

// WithReturnDirect marks the tool as return_direct.
func WithReturnDirect() Option { return func(f *funcTool) { f.direct = true } }

// WithSchema overrides the reflected schema.
func WithSchema(s *jsonschema.Schema) Option { return func(f *funcTool) { f.schema = s } }

type funcTool struct {
	name, desc string
	schema     *jsonschema.Schema
	direct     bool
	fn         func(ctx context.Context, args map[string]any) (any, error)
}

func (f *funcTool) Name() string               { return f.name }
func (f *funcTool) Description() string        { return f.desc }
func (f *funcTool) Schema() *jsonschema.Schema { return f.schema }
func (f *funcTool) ReturnDirect() bool         { return f.direct }
func (f *funcTool) Call(ctx context.Context, args map[string]any) (out any, err error) {
	ctx, end := callbacks.Start(ctx, callbacks.ToolStart, f.name, args)
	defer func() { end(out, err) }()
	if args == nil {
		args = map[string]any{}
	}
	if verr := jsonschema.Validate(f.schema, args); verr != nil {
		return nil, &Error{Msg: fmt.Sprintf("invalid arguments for tool %s: %v", f.name, verr)}
	}
	return f.fn(ctx, args)
}

// New creates a tool from a typed function (the @tool decorator). The
// argument schema is reflected from T, which should be a struct.
func New[T any](name, description string, fn func(ctx context.Context, args T) (any, error), opts ...Option) Tool {
	t := &funcTool{name: name, desc: description, schema: jsonschema.For[T]()}
	t.schema.Title = ""
	t.fn = func(ctx context.Context, args map[string]any) (any, error) {
		var v T
		if err := Decode(args, &v); err != nil {
			return nil, &Error{Msg: fmt.Sprintf("invalid arguments for tool %s: %v", name, err)}
		}
		return fn(ctx, v)
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

// NewRaw creates a tool from an untyped function and an explicit schema.
func NewRaw(name, description string, schema *jsonschema.Schema, fn func(ctx context.Context, args map[string]any) (any, error), opts ...Option) Tool {
	t := &funcTool{name: name, desc: description, schema: schema, fn: fn}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Decode converts a JSON-ish map into v via a JSON round trip.
func Decode(args map[string]any, v any) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Stringify renders a tool result as message content.
func Stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case []byte:
		return string(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

type callIDKey struct{}

// WithCallID records the tool call ID being executed (InjectedToolCallId).
func WithCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callIDKey{}, id)
}

// CallID returns the current tool call ID, if any.
func CallID(ctx context.Context) string {
	id, _ := ctx.Value(callIDKey{}).(string)
	return id
}

// Find returns the tool with the given name.
func Find(ts []Tool, name string) (Tool, bool) {
	for _, t := range ts {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}
