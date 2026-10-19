package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

func ex(out map[string]any) Example {
	return Example{ID: "e", Inputs: map[string]any{"q": "x"}, Outputs: out}
}

func score(t *testing.T, e Evaluator, run Run, want Example) Result {
	t.Helper()
	r, err := e.Evaluate(context.Background(), run, want)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestJSONLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ds.jsonl")
	in := []Example{
		{ID: "a", Inputs: map[string]any{"q": "hi"}, Outputs: map[string]any{"answer": "hello"}, Metadata: map[string]any{"tag": "x"}},
		{ID: "b", Inputs: map[string]any{"q": "bye"}},
	}
	if err := SaveJSONL(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(in)
	b, _ := json.Marshal(out)
	if string(a) != string(b) {
		t.Fatalf("round trip changed data:\n%s\n%s", a, b)
	}
	var buf bytes.Buffer
	_ = WriteJSONL(&buf, in)
	if n := strings.Count(buf.String(), "\n"); n != 2 {
		t.Fatalf("want one line per example, got %d newlines", n)
	}
}

func TestExactContainsRegex(t *testing.T) {
	e := ex(map[string]any{"answer": "Paris"})
	if r := score(t, ExactMatch("answer"), Run{Outputs: map[string]any{"answer": " Paris\n"}}, e); r.Score != 1 {
		t.Fatalf("exact trimmed: %+v", r)
	}
	if r := score(t, ExactMatch("answer"), Run{Outputs: map[string]any{"answer": "paris"}}, e); r.Score != 0 {
		t.Fatalf("exact should be case sensitive: %+v", r)
	}
	if r := score(t, Contains("answer"), Run{Outputs: map[string]any{"answer": "It is Paris, I think"}}, e); r.Score != 1 {
		t.Fatalf("contains: %+v", r)
	}
	if r := score(t, RegexMatch("answer", regexp.MustCompile(`^P\w+$`)), Run{Outputs: map[string]any{"answer": "Paris"}}, e); r.Score != 1 {
		t.Fatalf("regex: %+v", r)
	}
	if _, err := ExactMatch("missing").Evaluate(context.Background(), Run{}, e); err == nil {
		t.Fatal("missing reference should be an error")
	}
}

func TestJSONEqual(t *testing.T) {
	e := ex(map[string]any{"data": `{"a":1,"b":[1,2]}`})
	r := score(t, JSONEqual("data"), Run{Outputs: map[string]any{"data": `{ "b": [1, 2], "a": 1 }`}}, e)
	if r.Score != 1 {
		t.Fatalf("json equal ignoring order/whitespace: %+v", r)
	}
	r = score(t, JSONEqual("data"), Run{Outputs: map[string]any{"data": `{"a":2}`}}, e)
	if r.Score != 0 {
		t.Fatalf("json differs: %+v", r)
	}
}

func TestEmbeddingDistance(t *testing.T) {
	e := ex(map[string]any{"answer": "the cat sat on the mat"})
	ev := EmbeddingDistance(embeddings.Fake{}, "answer", 0.9)
	if r := score(t, ev, Run{Outputs: map[string]any{"answer": "the cat sat on the mat"}}, e); r.Score != 1 {
		t.Fatalf("identical text should pass: %+v", r)
	}
	if r := score(t, ev, Run{Outputs: map[string]any{"answer": "stock prices fell sharply today"}}, e); r.Score != 0 {
		t.Fatalf("unrelated text should fail: %+v", r)
	}
}

func TestLLMJudge(t *testing.T) {
	judge := models.NewFake(messages.AI("```json\n{\"score\": 1.7, \"reasoning\": \"complete answer\"}\n```"))
	r := score(t, LLMJudge(judge, "Is the answer complete?"), Run{Outputs: map[string]any{"answer": "full"}}, ex(nil))
	if r.Key != "llm_judge" || r.Score != 1 || r.Comment != "complete answer" {
		t.Fatalf("judge result: %+v", r)
	}
	if !strings.Contains(judge.Calls[0][0].Text(), "Is the answer complete?") {
		t.Fatal("rubric not sent to judge")
	}
	bad := models.NewFake(messages.AI("no idea"))
	if _, err := LLMJudge(bad, "r").Evaluate(context.Background(), Run{}, ex(nil)); err == nil {
		t.Fatal("unparseable judge output should error")
	}
}

func TestTrajectoryModes(t *testing.T) {
	ref := []any{"search", "read", "answer"}
	cases := []struct {
		mode TrajectoryMode
		got  []string
		want bool
	}{
		{Strict, []string{"search", "read", "answer"}, true},
		{Strict, []string{"read", "search", "answer"}, false},
		{Unordered, []string{"read", "answer", "search"}, true},
		{Unordered, []string{"read", "search"}, false},
		{Subset, []string{"read", "search"}, true},
		{Subset, []string{"search", "delete", "answer"}, false},
		{Superset, []string{"search", "x", "read", "answer"}, true},
		{Superset, []string{"search", "read"}, false},
		{Subset, nil, true},
		{Superset, nil, false},
	}
	for _, c := range cases {
		ev := TrajectoryMatch(c.mode, "tools")
		outs := make([]any, len(c.got))
		for i, g := range c.got {
			outs[i] = g
		}
		r := score(t, ev, Run{Outputs: map[string]any{"tools": outs}}, ex(map[string]any{"tools": ref}))
		if (r.Score == 1) != c.want {
			t.Errorf("%s %v: got score %v, want pass=%v (%s)", c.mode, c.got, r.Score, c.want, r.Comment)
		}
	}
}

func TestToolNames(t *testing.T) {
	msgs := []messages.Message{messages.Human("q"), messages.AI("", messages.ToolCall{Name: "a"}), messages.AI("", messages.ToolCall{Name: "b"}, messages.ToolCall{Name: "c"})}
	if got := strings.Join(ToolNames(msgs), ","); got != "a,b,c" {
		t.Fatalf("ToolNames = %s", got)
	}
}

func TestEvaluateOrderAndSummary(t *testing.T) {
	var dataset []Example
	for i := range 40 {
		dataset = append(dataset, Example{ID: string(rune('A'+i%26)) + string(rune('a'+i/26)), Inputs: map[string]any{"n": i}, Outputs: map[string]any{"n": i}})
	}
	target := func(_ context.Context, in map[string]any) (map[string]any, error) {
		// finish out of order on purpose
		time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
		return map[string]any{"n": in["n"]}, nil
	}
	evs := []Evaluator{
		EvaluatorFunc(func(_ context.Context, run Run, ex Example) (Result, error) {
			return Result{Key: "match", Score: map[bool]float64{true: 1, false: 0}[run.Outputs["n"] == ex.Outputs["n"]]}, nil
		}),
		EvaluatorFunc(func(_ context.Context, run Run, ex Example) (Result, error) {
			return Result{Key: "half", Score: 0.5}, nil
		}),
	}
	rep, err := Evaluate(context.Background(), target, dataset, evs, WithConcurrency(8))
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rep.Results {
		if r.ExampleID != dataset[i].ID {
			t.Fatalf("result %d is %s, want %s", i, r.ExampleID, dataset[i].ID)
		}
	}
	if rep.Summary["match"] != 1 || rep.Summary["half"] != 0.5 {
		t.Fatalf("summary = %v", rep.Summary)
	}
}

func TestEvaluateErrorsAndTimeout(t *testing.T) {
	target := func(ctx context.Context, in map[string]any) (map[string]any, error) {
		name := in["name"].(string)
		switch name {
		case "boom":
			return nil, errors.New("boom")
		case "slow":
			<-ctx.Done() // blocks until the per-example timeout fires
			return nil, ctx.Err()
		}
		return map[string]any{"ok": true}, nil
	}
	dataset := []Example{
		{ID: "boom", Inputs: map[string]any{"name": "boom"}},
		{ID: "slow", Inputs: map[string]any{"name": "slow"}},
		{ID: "ok", Inputs: map[string]any{"name": "ok"}},
	}
	rep, err := Evaluate(context.Background(), target, dataset,
		[]Evaluator{EvaluatorFunc(func(_ context.Context, run Run, _ Example) (Result, error) {
			s := 0.0
			if run.Err == nil {
				s = 1
			}
			return Result{Key: "succeeded", Score: s}, nil
		})},
		WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Error != "boom" || rep.Results[1].Error == "" || rep.Results[2].Error != "" {
		t.Fatalf("errors not recorded per example: %+v", rep.Results)
	}
	if rep.Summary["succeeded"] != 1.0/3 {
		t.Fatalf("summary = %v", rep.Summary)
	}
}

func TestEvaluatorErrorRecorded(t *testing.T) {
	bad := EvaluatorFunc(func(context.Context, Run, Example) (Result, error) { return Result{}, errors.New("judge down") })
	rep, err := Evaluate(context.Background(), func(context.Context, map[string]any) (map[string]any, error) { return nil, nil },
		[]Example{{ID: "x"}}, []Evaluator{bad})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results[0].EvalErrors) != 1 || len(rep.Summary) != 0 {
		t.Fatalf("eval error should be recorded, not scored: %+v", rep)
	}
}

func TestReportWriters(t *testing.T) {
	rep, _ := Evaluate(context.Background(), func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"answer": "Paris"}, nil
	}, []Example{{ID: "capital", Outputs: map[string]any{"answer": "Paris"}}},
		[]Evaluator{ExactMatch("answer")})

	var txt bytes.Buffer
	if err := WriteText(&txt, rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt.String(), "capital") || !strings.Contains(txt.String(), "MEAN") {
		t.Fatalf("text report:\n%s", txt.String())
	}
	var js bytes.Buffer
	if err := WriteJSON(&js, rep); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(js.Bytes(), &back); err != nil || back.Summary["exact_match:answer"] != 1 {
		t.Fatalf("json report: %v %s", err, js.String())
	}
}
