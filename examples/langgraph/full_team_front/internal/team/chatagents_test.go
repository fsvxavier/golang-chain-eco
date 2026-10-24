package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChatAgentsAndRuns(t *testing.T) {
	cfg, root := t.TempDir(), filepath.Join(t.TempDir(), "my.proj")
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	write(t, filepath.Join(root, ".claude/agents/debugger.md"), "---\nname: debugger\ndescription: \"Finds bugs.\\n\\nMore\"\nmodel: sonnet\ntools: [Read, Grep]\n---\nbody")
	pd := projectDirs(root)[0]
	write(t, filepath.Join(pd, "sess1/subagents/agent-abc123.meta.json"), `{"agentType":"debugger","description":"find the leak"}`)
	write(t, filepath.Join(pd, "sess1/subagents/agent-abc123.jsonl"), `{"type":"user","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"hi"}}`+"\n")
	write(t, filepath.Join(pd, "sess1/subagents/agent-zzz.meta.json"), `{"agentType":"Explore"}`)
	write(t, filepath.Join(pd, "sess1/subagents/agent-zzz.jsonl"), "{}\n")

	chatCached = chatCache{}
	as, rs := ChatAgents(root)
	if len(rs) != 2 {
		t.Fatalf("runs %+v", rs)
	}
	got := map[string]ChatAgent{}
	for _, a := range as {
		got[a.Name] = a
	}
	d := got["debugger"]
	if d.Description != "Finds bugs." || d.Model != "sonnet" || d.Source != "project" || d.Runs != 1 || d.Active != 1 {
		t.Fatalf("debugger %+v", d)
	}
	if e := got["Explore"]; e.Source != "built-in" || e.Runs != 1 {
		t.Fatalf("Explore %+v", e)
	}
	if _, err := ChatTranscriptPath(root, "abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := ChatTranscriptPath(root, "../x"); err == nil {
		t.Fatal("a path was accepted as an agent id")
	}
}

func TestReadTranscript(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, p, strings.Join([]string{
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"hidden"}}`,
		`{"type":"user","message":{"role":"user","content":"question"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer"},{"type":"tool_use","name":"read","input":{"path":"a"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"file text"}]}}`,
		`{"type":"attachment"}`, "",
	}, "\n"))
	ts, err := ReadTranscript(p)
	if err != nil || len(ts) != 4 || ts[0].Text != "question" || ts[1].Text != "answer" || !strings.HasPrefix(ts[2].Text, "call read") || ts[3].Text != "result file text" {
		t.Fatalf("%v %+v", err, ts)
	}
}

func TestCLITranscriptPathRejectsNonSessions(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if _, err := CLITranscriptPath("../../etc/passwd"); err == nil {
		t.Fatal("accepted")
	}
	id := "123e4567-e89b-42d3-a456-426614174000"
	write(t, filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects/-tmp/"+id+".jsonl"), "{}")
	if _, err := CLITranscriptPath(id); err != nil {
		t.Fatal(err)
	}
}

func TestHubKeepsCLICalls(t *testing.T) {
	h := NewHub(time.Second)
	h.AddCLI(CLICall{ID: "a", Worker: "po"})
	h.AddCLI(CLICall{ID: "a", Worker: "po", Done: true})
	for i := 0; i < maxCLICalls+5; i++ {
		h.AddCLI(CLICall{ID: string(rune('b' + i))})
	}
	cs := h.CLICalls()
	if len(cs) != maxCLICalls {
		t.Fatalf("kept %d", len(cs))
	}
}
