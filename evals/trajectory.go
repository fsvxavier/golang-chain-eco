package evals

import (
	"context"
	"fmt"
	"slices"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// TrajectoryMode says how an agent's tool-call sequence is compared with the
// reference (same semantics as agentevals).
type TrajectoryMode string

const (
	// Strict: same tools, same order, nothing extra.
	Strict TrajectoryMode = "strict"
	// Unordered: same tools with the same counts, any order.
	Unordered TrajectoryMode = "unordered"
	// Subset: every tool call made is in the reference (no extra calls).
	Subset TrajectoryMode = "subset"
	// Superset: every reference tool call was made (extra calls allowed).
	Superset TrajectoryMode = "superset"
)

// ToolNames returns the tool names called by the AI messages, in order. Targets
// use it to put the trajectory into their outputs.
func ToolNames(msgs []messages.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			out = append(out, c.Name)
		}
	}
	return out
}

// TrajectoryMatch compares the tool-call names in outputs[field] (a []string,
// or []any of strings) with the example's outputs[field] using mode.
func TrajectoryMatch(mode TrajectoryMode, field string) Evaluator {
	return EvaluatorFunc(func(_ context.Context, run Run, ex Example) (Result, error) {
		got, want, err := pair(run, ex, field)
		if err != nil {
			return Result{}, err
		}
		g, gok := toStrings(got)
		if got == nil { // target failed or produced no trajectory
			g, gok = nil, true
		}
		w, wok := toStrings(want)
		if !gok || !wok {
			return Result{}, fmt.Errorf("evals: trajectory %q must be a list of tool names", field)
		}
		ok, err := trajectoryOK(mode, g, w)
		if err != nil {
			return Result{}, err
		}
		return pass("trajectory:"+string(mode), ok, fmt.Sprintf("got=%v want=%v", g, w)), nil
	})
}

func trajectoryOK(mode TrajectoryMode, got, want []string) (bool, error) {
	switch mode {
	case Strict:
		return slices.Equal(got, want), nil
	case Unordered:
		return sameCounts(got, want), nil
	case Subset:
		return contained(got, want), nil
	case Superset:
		return contained(want, got), nil
	}
	return false, fmt.Errorf("evals: unknown trajectory mode %q", mode)
}

// contained reports whether every name in a occurs in b at least as often.
func contained(a, b []string) bool {
	counts := map[string]int{}
	for _, n := range b {
		counts[n]++
	}
	for _, n := range a {
		if counts[n] == 0 {
			return false
		}
		counts[n]--
	}
	return true
}

func sameCounts(a, b []string) bool { return contained(a, b) && contained(b, a) }

func toStrings(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}
