// Package evals runs a target over a dataset and scores each output with
// evaluators: exact/contains/regex/JSON checks, embedding similarity, an
// LLM judge and agent trajectory matching. Everything runs offline with the
// fake model and embeddings; the report prints as text or JSON.
package evals

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Example is one dataset row: the target's inputs, the reference outputs
// the evaluators compare against, and free-form metadata.
type Example struct {
	ID       string         `json:"id"`
	Inputs   map[string]any `json:"inputs"`
	Outputs  map[string]any `json:"outputs,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// LoadJSONL reads one Example per line.
func LoadJSONL(path string) ([]Example, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadJSONL(f)
}

// ReadJSONL decodes a stream of JSON objects (one per line is the usual
// layout; any whitespace separation works).
func ReadJSONL(r io.Reader) ([]Example, error) {
	dec := json.NewDecoder(r)
	var out []Example
	for {
		var ex Example
		if err := dec.Decode(&ex); err == io.EOF {
			return out, nil
		} else if err != nil {
			return nil, fmt.Errorf("evals: jsonl example %d: %w", len(out)+1, err)
		}
		out = append(out, ex)
	}
}

// SaveJSONL writes one Example per line, replacing the file.
func SaveJSONL(path string, examples []Example) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := WriteJSONL(f, examples); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// WriteJSONL writes one Example per line.
func WriteJSONL(w io.Writer, examples []Example) error {
	enc := json.NewEncoder(w) // Encode appends the newline
	for i := range examples {
		if err := enc.Encode(examples[i]); err != nil {
			return err
		}
	}
	return nil
}
