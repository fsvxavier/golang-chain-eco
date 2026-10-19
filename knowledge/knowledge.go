// Package knowledge defines the contract shared by the RAG, CAG and KAG
// answerers: one question in, one sourced answer out.
package knowledge

import (
	"context"
	"errors"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
)

// ErrEmptyQuestion is returned when a question has no text.
var ErrEmptyQuestion = errors.New("knowledge: empty question")

// Question is the input of an Answerer.
type Question struct {
	Text    string
	History []messages.Message
	Filter  vectorstores.Filter
	// Force selects a strategy: "" (auto), "rag", "cag" or "kag".
	Force string
}

// Source is a piece of evidence used to build an answer.
type Source struct {
	ID       string
	Content  string
	Metadata map[string]any
	Score    float64
}

// Step records one decision taken while answering.
type Step struct {
	Name   string
	Detail string
}

// Answer is the output of an Answerer.
type Answer struct {
	Text     string
	Sources  []Source
	Strategy string
	Usage    *messages.Usage
	Trace    []Step
}

// Answerer answers a question using some knowledge strategy.
type Answerer interface {
	Answer(ctx context.Context, q Question) (Answer, error)
}

// AsRunnable exposes an Answerer as a Runnable so it composes with other chains.
func AsRunnable(a Answerer) runnables.Runnable[Question, Answer] {
	return runnables.Lambda("knowledge.answer", a.Answer)
}

// SourcesFromDocs converts retrieved documents into Sources.
// Score is read from metadata["score"] when it is a float64.
func SourcesFromDocs(docs []documents.Document) []Source {
	out := make([]Source, 0, len(docs))
	for _, d := range docs {
		s := Source{ID: d.ID, Content: d.PageContent, Metadata: d.Metadata}
		if v, ok := d.Metadata["score"].(float64); ok {
			s.Score = v
		}
		out = append(out, s)
	}
	return out
}
