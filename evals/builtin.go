package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// pair returns the target's value and the reference value for field. A
// missing reference is a configuration error; a missing target value is
// just nil (the comparison then fails).
func pair(run Run, ex Example, field string) (got, want any, err error) {
	want, ok := ex.Outputs[field]
	if !ok {
		return nil, nil, fmt.Errorf("evals: example %q has no outputs[%q]", ex.ID, field)
	}
	return run.Outputs[field], want, nil
}

// text renders a value for text comparison: strings as-is, anything else as JSON.
func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func pass(key string, ok bool, comment string) Result {
	r := Result{Key: key, Comment: comment}
	if ok {
		r.Score = 1
	}
	return r
}

// ExactMatch passes when outputs[field] equals the example's outputs[field]
// (strings are compared after trimming surrounding whitespace).
func ExactMatch(field string) Evaluator {
	return EvaluatorFunc(func(_ context.Context, run Run, ex Example) (Result, error) {
		got, want, err := pair(run, ex, field)
		if err != nil {
			return Result{}, err
		}
		gs, gok := got.(string)
		ws, wok := want.(string)
		var ok bool
		if gok && wok {
			ok = strings.TrimSpace(gs) == strings.TrimSpace(ws)
		} else {
			ok = reflect.DeepEqual(got, want)
		}
		return pass("exact_match:"+field, ok, ""), nil
	})
}

// Contains passes when outputs[field] contains the example's outputs[field] as a substring.
func Contains(field string) Evaluator {
	return EvaluatorFunc(func(_ context.Context, run Run, ex Example) (Result, error) {
		got, want, err := pair(run, ex, field)
		if err != nil {
			return Result{}, err
		}
		return pass("contains:"+field, strings.Contains(text(got), text(want)), ""), nil
	})
}

// RegexMatch passes when outputs[field] matches re.
func RegexMatch(field string, re *regexp.Regexp) Evaluator {
	return EvaluatorFunc(func(_ context.Context, run Run, _ Example) (Result, error) {
		return pass("regex:"+field, re.MatchString(text(run.Outputs[field])), re.String()), nil
	})
}

// JSONEqual passes when outputs[field] and the example's outputs[field] are
// the same JSON value (key order and whitespace ignored). Strings holding
// JSON are parsed first.
func JSONEqual(field string) Evaluator {
	return EvaluatorFunc(func(_ context.Context, run Run, ex Example) (Result, error) {
		got, want, err := pair(run, ex, field)
		if err != nil {
			return Result{}, err
		}
		g, gerr := normJSON(got)
		w, werr := normJSON(want)
		if gerr != nil || werr != nil {
			return pass("json_equal:"+field, false, "invalid JSON"), nil
		}
		return pass("json_equal:"+field, reflect.DeepEqual(g, w), ""), nil
	})
}

func normJSON(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if s, ok := v.(string); ok {
		b = []byte(s)
	}
	var out any
	err = json.Unmarshal(b, &out)
	return out, err
}

// EmbeddingDistance compares outputs[field] with the reference by cosine
// similarity of their embeddings. Score is 1 when similarity >= threshold,
// else 0; the comment carries the similarity.
func EmbeddingDistance(e embeddings.Embedder, field string, threshold float64) Evaluator {
	return EvaluatorFunc(func(ctx context.Context, run Run, ex Example) (Result, error) {
		got, want, err := pair(run, ex, field)
		if err != nil {
			return Result{}, err
		}
		vs, err := e.EmbedDocuments(ctx, []string{text(got), text(want)})
		if err != nil {
			return Result{}, err
		}
		sim := embeddings.Cosine(vs[0], vs[1])
		r := pass("embedding:"+field, sim >= threshold, fmt.Sprintf("cosine=%.3f threshold=%.3f", sim, threshold))
		return r, nil
	})
}

// LLMJudge asks model to grade the whole output against the rubric. The
// judge answers {"score": 0..1, "reasoning": "..."} (structured output);
// the score is clamped to [0,1] and the reasoning becomes the comment.
func LLMJudge(model models.ChatModel, rubric string) Evaluator {
	type verdict struct {
		Score     float64 `json:"score" description:"Score from 0 (fails the rubric) to 1 (fully meets it)"`
		Reasoning string  `json:"reasoning" description:"Short justification"`
	}
	judge := models.Structured[verdict](model, models.MethodJSONSchema)
	return EvaluatorFunc(func(ctx context.Context, run Run, ex Example) (Result, error) {
		var human strings.Builder
		fmt.Fprintf(&human, "Input:\n%s\n", text(ex.Inputs))
		if ex.Outputs != nil {
			fmt.Fprintf(&human, "\nReference output:\n%s\n", text(ex.Outputs))
		}
		fmt.Fprintf(&human, "\nCandidate output:\n%s", text(run.Outputs))
		msgs := []messages.Message{
			messages.System(rubric + "\n\nReply only with JSON: {\"score\": number between 0 and 1, \"reasoning\": string}."),
			messages.Human(human.String()),
		}
		v, err := judge.Invoke(ctx, msgs)
		if err != nil {
			return Result{}, fmt.Errorf("evals: llm judge: %w", err)
		}
		return Result{Key: "llm_judge", Score: min(max(v.Score, 0), 1), Comment: v.Reasoning}, nil
	})
}
