// Package compress shrinks the context sent to a model: it summarises the
// conversation, compresses old turns (caveman), and routes shell commands
// through rtk. A Decider answers the questions behind those choices; Guarded
// falls back to rules so a bad or slow decider never fails a run.
package compress

import (
	"context"
	"math"
)

// State is the JSON-object view of the conversation that deciders read.
type State map[string]any

// Kind is the answer type of a Question.
type Kind string

const (
	KindChoice Kind = "choice"
	KindNoul   Kind = "noul"
	KindScore  Kind = "score" // declared, unused for now
)

// Question asks a decider one thing about the State.
type Question struct {
	ID      string
	Kind    Kind
	Text    string
	Options []string
}

// Answer is a decider's reply to one Question.
//
// For KindNoul, P is P(true) and True is P >= 0.5. For KindChoice, Dist[Pick]
// is the top-class probability. Level is reserved and not set by the rules.
type Answer struct {
	ID    string
	Kind  Kind
	True  bool
	P     float64
	Pick  string
	Level int
	Dist  map[string]float64
}

// Confident reports whether the answer is sure enough to override the rules.
// For KindNoul the confidence is max(P, 1-P), so a confident "false" (P near
// 0) counts as much as a confident "true" (P near 1). For KindChoice it is
// Dist[Pick]. The value must lie in [0,1] (NaN never does) and be >= floor.
func (a Answer) Confident(floor float64) bool {
	var p float64
	if a.Kind == KindNoul {
		if !(a.P >= 0 && a.P <= 1) {
			return false
		}
		p = math.Max(a.P, 1-a.P)
	} else {
		v, ok := a.Dist[a.Pick]
		if !ok {
			return false
		}
		p = v
	}
	return p >= 0 && p <= 1 && p >= floor
}

// Decider answers a batch of questions about a State.
type Decider interface {
	Decide(ctx context.Context, st State, qs []Question) ([]Answer, error)
}

// FuncDecider adapts a plain function to Decider.
type FuncDecider func(context.Context, State, []Question) ([]Answer, error)

// Decide calls f.
func (f FuncDecider) Decide(ctx context.Context, st State, qs []Question) ([]Answer, error) {
	return f(ctx, st, qs)
}
