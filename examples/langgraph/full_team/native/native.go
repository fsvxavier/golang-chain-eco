//go:build laya_native

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/fsvxavier/golang-chain-eco/compress"
	laya "github.com/metalagman/laya-go"
)

// openDecider opens a complete Laya bundle from dir and returns a Decider over
// it, plus a closer that releases the model and runtime.
func openDecider(ctx context.Context, dir string) (compress.Decider, func(), error) {
	if dir == "" {
		return nil, nil, errors.New("LAYA_BUNDLE_DIR is not set")
	}
	if os.Getenv("ORT_DISABLE_TELEMETRY") == "" {
		os.Setenv("ORT_DISABLE_TELEMETRY", "1")
	}
	rt, err := laya.NewRuntime(ctx, laya.RuntimeOptions{})
	if err != nil {
		return nil, nil, err
	}
	m, err := rt.OpenModelDir(ctx, dir, laya.ModelOptions{
		QueueCapacity: 8,
		// Opt-in truncation: long histories are shortened instead of failing with ErrInputTooLong.
		Truncation: laya.TruncateOverflow,
	})
	if err != nil {
		rt.Close(ctx)
		return nil, nil, err
	}
	closeFn := func() {
		_ = m.Close(ctx)
		_ = rt.Close(ctx)
	}
	return nativeDecider{model: m}, closeFn, nil
}

type nativeDecider struct{ model *laya.Model }

// Decide implements compress.Decider. Each question becomes a laya question,
// the batch is run in one Predict call, and results map back by id.
func (d nativeDecider) Decide(ctx context.Context, st compress.State, qs []compress.Question) ([]compress.Answer, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	state, err := laya.JSONState(raw)
	if err != nil {
		return nil, err
	}
	lq := make([]laya.Question, 0, len(qs))
	for _, q := range qs {
		switch q.Kind {
		case compress.KindNoul:
			nq, err := laya.NewNoulQuestion(laya.QuestionID(q.ID), q.Text, "", "")
			if err != nil {
				return nil, err
			}
			lq = append(lq, nq)
		case compress.KindChoice:
			crit := make([]laya.ChoiceCriterion, 0, len(q.Options))
			for _, o := range q.Options {
				crit = append(crit, laya.ChoiceCriterion{ID: laya.CriterionID(o), Description: o})
			}
			cq, err := laya.NewChoiceQuestion(laya.QuestionID(q.ID), q.Text, crit)
			if err != nil {
				return nil, err
			}
			lq = append(lq, cq)
		default:
			return nil, fmt.Errorf("laya: unsupported question kind %q for %q", q.Kind, q.ID)
		}
	}

	pred, err := d.model.Predict(ctx, state, lq)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]laya.Result, len(pred.Results()))
	for _, r := range pred.Results() {
		byID[string(r.QuestionID())] = r
	}

	out := make([]compress.Answer, 0, len(qs))
	for _, q := range qs {
		r, ok := byID[q.ID]
		if !ok {
			return nil, fmt.Errorf("laya: no result for %q", q.ID)
		}
		switch v := r.(type) {
		case laya.NoulResult:
			p := v.TrueProbability()
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, P: p, True: p >= 0.5})
		case laya.ChoiceResult:
			dist := make(map[string]float64, len(v.Probabilities()))
			for _, cp := range v.Probabilities() {
				dist[string(cp.CriterionID)] = cp.Probability
			}
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, Pick: string(v.Selected()), Dist: dist})
		default:
			return nil, fmt.Errorf("laya: unexpected result type %T for %q", r, q.ID)
		}
	}
	return out, nil
}
