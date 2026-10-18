// Package outputparsers converts model output into Go values
// (langchain_core.output_parsers). Each parser is a runnables.Runnable whose
// input is an AI message.
package outputparsers

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Str extracts the text of a message (StrOutputParser). It streams.
type Str struct{}

// Invoke implements runnables.Runnable.
func (Str) Invoke(_ context.Context, m messages.Message) (string, error) { return m.Text(), nil }

// Transform maps a chunk stream to a text stream.
func (Str) Transform(seq iter.Seq2[messages.Message, error]) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for c, err := range seq {
			if !yield(c.Text(), err) || err != nil {
				return
			}
		}
	}
}

// JSON parses the message text as JSON (JsonOutputParser), tolerating
// markdown fences and surrounding prose.
type JSON struct{}

// Invoke implements runnables.Runnable.
func (JSON) Invoke(_ context.Context, m messages.Message) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(models.ExtractJSON(m.Text())), &v); err != nil {
		return nil, fmt.Errorf("outputparsers: invalid json output: %w", err)
	}
	return v, nil
}

// Struct parses JSON output into T (PydanticOutputParser).
type Struct[T any] struct{}

// Invoke implements runnables.Runnable.
func (Struct[T]) Invoke(_ context.Context, m messages.Message) (T, error) {
	var v T
	err := json.Unmarshal([]byte(models.ExtractJSON(m.Text())), &v)
	return v, err
}

// FormatInstructions tells the model which JSON shape to produce.
func (Struct[T]) FormatInstructions() string {
	s := jsonschema.For[T]()
	b, _ := json.Marshal(s)
	return "The output should be formatted as a JSON instance that conforms to the JSON schema below.\n\n" +
		"Here is the output schema:\n```\n" + string(b) + "\n```"
}

// CommaList splits output on commas (CommaSeparatedListOutputParser).
type CommaList struct{}

// Invoke implements runnables.Runnable.
func (CommaList) Invoke(_ context.Context, m messages.Message) ([]string, error) {
	var out []string
	for _, p := range strings.Split(m.Text(), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// FormatInstructions for CommaList.
func (CommaList) FormatInstructions() string {
	return "Your response should be a list of comma separated values, eg: `foo, bar, baz`"
}

// ToolCalls returns the tool calls of an AI message (JsonOutputToolsParser).
type ToolCalls struct{}

// Invoke implements runnables.Runnable.
func (ToolCalls) Invoke(_ context.Context, m messages.Message) ([]messages.ToolCall, error) {
	return m.ToolCalls, nil
}

// ToolArgs decodes the arguments of the first call to the named tool into T
// (PydanticToolsParser with first_tool_only).
type ToolArgs[T any] struct{ Name string }

// Invoke implements runnables.Runnable.
func (p ToolArgs[T]) Invoke(_ context.Context, m messages.Message) (T, error) {
	var v T
	for _, tc := range m.ToolCalls {
		if p.Name == "" || tc.Name == p.Name {
			return v, tools.Decode(tc.Args, &v)
		}
	}
	return v, fmt.Errorf("outputparsers: no tool call named %q", p.Name)
}
