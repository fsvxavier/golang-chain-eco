package main

import (
	"context"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/internal/team"
)

func TestAllowedProgramsWithProviderKey(t *testing.T) {
	env := func(keys map[string]string) func(string) string {
		return func(k string) string { return keys[k] }
	}
	offline := allowedPrograms(env(nil))
	if want := []string{"go", "git", "ls", "grep", "echo"}; !reflect.DeepEqual(offline, want) {
		t.Errorf("offline = %v, want %v", offline, want)
	}
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		got := allowedPrograms(env(map[string]string{k: "x"}))
		if want := []string{"ls", "grep", "echo"}; !reflect.DeepEqual(got, want) {
			t.Errorf("with %s = %v, want %v", k, got, want)
		}
	}
}

// The scripted decider takes its ponytail answer from the rule: a conversation
// without code blocks gets no note, one with a real share of code does.
func TestScriptedPonytailFollowsRule(t *testing.T) {
	q := compress.Question{ID: compress.QPonytail, Kind: compress.KindNoul}
	for _, tc := range []struct {
		share float64
		want  bool
	}{{0, false}, {0.5, true}} {
		ans, err := scripted(context.Background(), compress.State{"code_share": tc.share}, []compress.Question{q})
		if err != nil {
			t.Fatal(err)
		}
		if len(ans) != 1 || ans[0].True != tc.want {
			t.Errorf("code_share %v: answer %+v, want True=%v", tc.share, ans, tc.want)
		}
	}
}

// The offline run must show at least one JSON tool result rewritten to TOON
// with bytes saved. Ponytail notes are not required: the offline run has no
// code blocks, so the rule may give zero notes.
func TestOfflineRunReportsTOON(t *testing.T) {
	if os.Getenv("LAYA_URL") != "" {
		t.Skip("offline run only: LAYA_URL is set")
	}
	for _, r := range team.Roles {
		if team.ModelSpec(r) != "" {
			t.Skipf("offline run only: role %s has a model spec", r)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, err := run(ctx, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m.toonResults < 1 || m.toonSaved < 1 {
		t.Errorf("TOON rewrites = %d saving %d bytes, want >= 1 rewrite with bytes saved", m.toonResults, m.toonSaved)
	}
}
