package compress

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

func TestAnswerConfident(t *testing.T) {
	a := Answer{ID: QCompressHistory, Kind: KindNoul, P: 0.8, True: true}
	if !a.Confident(0.7) {
		t.Fatal("P=0.8 should be confident at floor 0.7")
	}
	a.P = 0.6
	if a.Confident(0.7) {
		t.Fatal("P=0.6 should not be confident at floor 0.7")
	}
	a.P = math.NaN()
	if a.Confident(0.7) {
		t.Fatal("NaN should not be confident")
	}
	a.P = 1.5
	if a.Confident(0.7) {
		t.Fatal("P>1 should not be confident")
	}
	a.P = -0.1
	if a.Confident(0.7) {
		t.Fatal("P<0 should not be confident")
	}
	a.P = 0.1
	if !a.Confident(0.7) {
		t.Fatal("P=0.1 (confident false) should be confident at floor 0.7")
	}
	a.P = 0.5
	if a.Confident(0.7) {
		t.Fatal("P=0.5 should not be confident at floor 0.7")
	}

	c := Answer{ID: QLevel, Kind: KindChoice, Pick: "full", Dist: map[string]float64{"full": 0.9}}
	if !c.Confident(0.7) {
		t.Fatal("choice Dist[full]=0.9 should be confident")
	}
	c.Dist = map[string]float64{"full": 0.5}
	if c.Confident(0.7) {
		t.Fatal("choice Dist[full]=0.5 should not be confident")
	}
	c.Pick = "ultra"
	c.Dist = map[string]float64{"full": 0.9}
	if c.Confident(0.7) {
		t.Fatal("choice with a missing pick should not be confident")
	}
}

func TestSummaryUnderBudget(t *testing.T) {
	cases := map[string][]messages.Message{
		"empty":    nil,
		"three":    {messages.Human("hi there"), messages.AI("hello"), messages.Tool("ok", "c1")},
		"huge":     {messages.Human(strings.Repeat("a", 5<<20))},
		"unicode":  {messages.Human(strings.Repeat("ação 日本語 🙂 ", 5000)), messages.AI(strings.Repeat("ñandú ", 5000))},
		"escapes":  {messages.AI(strings.Repeat("<&>\"\\\n", 1<<16))},
		"ctrl-end": {messages.Human("x"), messages.Tool(strings.Repeat("\x01", 1<<20), "c2")},
	}
	for name, msgs := range cases {
		s := Summarize(msgs, SummaryOptions{})
		if n := s.EncodedTokens(); n > 700 {
			t.Errorf("%s: EncodedTokens=%d, want <=700", name, n)
		}
	}
}

func TestSummaryCountsToolShare(t *testing.T) {
	msgs := []messages.Message{messages.Human("aaaa"), messages.Tool("bbbbbbbb", "c1")}
	s := Summarize(msgs, SummaryOptions{})
	if s.Messages != 2 || s.Turns != 1 {
		t.Fatalf("Messages=%d Turns=%d, want 2 and 1", s.Messages, s.Turns)
	}
	if math.Abs(s.ToolShare-2.0/3.0) > 1e-9 {
		t.Fatalf("ToolShare=%v, want 2/3", s.ToolShare)
	}
	if s.LargestToolBytes != 8 {
		t.Fatalf("LargestToolBytes=%d, want 8", s.LargestToolBytes)
	}
}

func TestRuleDeciderHistory(t *testing.T) {
	qs := HistoryQuestions()
	ans, err := RuleDecider{}.Decide(context.Background(), State{"tokens": 5000}, qs)
	if err != nil {
		t.Fatal(err)
	}
	if !ans[0].True {
		t.Fatal("5000 tokens should compress history")
	}
	if ans[1].Pick != "full" {
		t.Fatalf("level=%q, want full", ans[1].Pick)
	}

	ans, err = RuleDecider{}.Decide(context.Background(), State{"tokens": 100}, qs)
	if err != nil {
		t.Fatal(err)
	}
	if ans[0].True {
		t.Fatal("100 tokens should not compress history")
	}

	_, err = RuleDecider{}.Decide(context.Background(), State{}, []Question{{ID: "nope", Kind: KindNoul}})
	if err == nil {
		t.Fatal("unknown question id should be an error")
	}
}

func TestRuleDeciderShell(t *testing.T) {
	q := []Question{ShellQuestion()}
	ctx := context.Background()
	cases := []struct {
		command string
		prev    int
		want    bool
	}{
		{"go test ./...", 5000, true},
		{"echo hi", 5000, false},
		{"go build", -1, true},
	}
	for _, c := range cases {
		ans, err := RuleDecider{}.Decide(ctx, ShellState(c.command, c.prev), q)
		if err != nil {
			t.Fatal(err)
		}
		if ans[0].True != c.want {
			t.Errorf("%q prev=%d: got %v, want %v", c.command, c.prev, ans[0].True, c.want)
		}
	}
}

func TestGuardedFallsBack(t *testing.T) {
	qs := append(HistoryQuestions(), ShellQuestion())
	st := State{"tokens": 5000, "command": "go test ./...", "prev_output_bytes": 5000}
	want, err := RuleDecider{}.Decide(context.Background(), st, qs)
	if err != nil {
		t.Fatal(err)
	}

	inners := map[string]Decider{
		"error": FuncDecider(func(context.Context, State, []Question) ([]Answer, error) {
			return nil, errors.New("boom")
		}),
		"wrong id": FuncDecider(func(_ context.Context, _ State, qs []Question) ([]Answer, error) {
			var out []Answer
			for _, q := range qs {
				out = append(out, Answer{ID: "other", Kind: q.Kind, P: 1, True: true})
			}
			return out, nil
		}),
		"low confidence": FuncDecider(func(_ context.Context, _ State, qs []Question) ([]Answer, error) {
			var out []Answer
			for _, q := range qs {
				out = append(out, Answer{ID: q.ID, Kind: q.Kind, P: 0.4, True: false})
			}
			return out, nil
		}),
		"slow": FuncDecider(func(context.Context, State, []Question) ([]Answer, error) {
			time.Sleep(3 * time.Second)
			return nil, nil
		}),
	}
	for name, inner := range inners {
		g := NewGuarded(inner)
		g.Timeout = 50 * time.Millisecond
		got, err := g.Decide(context.Background(), st, qs)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want rule answers %+v", name, got, want)
		}
	}
}

func TestGuardedUsesInnerWhenConfident(t *testing.T) {
	qs := HistoryQuestions()
	inner := FuncDecider(func(_ context.Context, _ State, qs []Question) ([]Answer, error) {
		return []Answer{
			{ID: QCompressHistory, Kind: KindNoul, P: 0.95, True: true},
			{ID: QLevel, Kind: KindChoice, Pick: "lite", Dist: map[string]float64{"lite": 0.5}},
			{ID: QCompressReply, Kind: KindNoul, P: 0.9, True: true},
		}, nil
	})
	got, err := NewGuarded(inner).Decide(context.Background(), State{"tokens": 100}, qs)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].True || got[0].P != 0.95 {
		t.Errorf("confident inner history answer not used: %+v", got[0])
	}
	if got[1].Pick != "full" {
		t.Errorf("unconfident inner level answer should fall back to full, got %q", got[1].Pick)
	}
	if !got[2].True || got[2].P != 0.9 {
		t.Errorf("confident inner reply answer not used: %+v", got[2])
	}
}

func TestGuardedPropagatesCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inner := FuncDecider(func(_ context.Context, _ State, qs []Question) ([]Answer, error) {
		return []Answer{{ID: QCompressHistory, Kind: KindNoul, P: 1, True: true}}, nil
	})
	_, err := NewGuarded(inner).Decide(ctx, State{}, HistoryQuestions())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

func TestGuardedRejectsNoulTrueDisagreeingWithP(t *testing.T) {
	// P=0 says "certainly false" but True says true. The answer is confident
	// by max(P,1-P), so only the True/P consistency check can reject it.
	st := State{"tokens": 100}
	qs := []Question{{ID: QCompressHistory, Kind: KindNoul}}
	inner := FuncDecider(func(_ context.Context, _ State, qs []Question) ([]Answer, error) {
		return []Answer{{ID: QCompressHistory, Kind: KindNoul, True: true, P: 0}}, nil
	})
	got, err := NewGuarded(inner).Decide(context.Background(), st, qs)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].True || got[0].P != 0 {
		t.Errorf("inconsistent noul accepted: %+v; want rule answer (False)", got[0])
	}
}
