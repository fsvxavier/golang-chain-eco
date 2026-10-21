package main

import (
	"reflect"
	"testing"
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
