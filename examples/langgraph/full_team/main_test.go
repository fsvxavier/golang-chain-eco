package main

import (
	"context"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

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

// The offline run must show both new rewrites: a Ponytail note on model calls
// and at least one JSON tool result rewritten to TOON with bytes saved.
func TestOfflineRunReportsPonytailAndTOON(t *testing.T) {
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
	if m.ponytail < 1 {
		t.Errorf("calls with the Ponytail note = %d, want >= 1", m.ponytail)
	}
	if m.toonResults < 1 || m.toonSaved < 1 {
		t.Errorf("TOON rewrites = %d saving %d bytes, want >= 1 rewrite with bytes saved", m.toonResults, m.toonSaved)
	}
}
