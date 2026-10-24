package team

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func TestCachePromptMarksSystemPrompt(t *testing.T) {
	var got []messages.Message
	fake := &models.Fake{Name: "c", Func: func(m []messages.Message, _ models.Options) (messages.Message, error) {
		got = m
		return messages.AI("ok"), nil
	}}
	agent, err := agents.Create(agents.Options{Model: fake, SystemPrompt: "skill text", Middleware: []agents.Middleware{cachePrompt{}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Invoke(context.Background(), graph.State{"messages": []messages.Message{messages.Human("hi")}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Role != messages.RoleSystem || got[1].Role != messages.RoleHuman {
		t.Fatalf("messages: %+v", got)
	}
	p := got[0].Parts
	if len(p) != 1 || p[0].Text != "skill text" || p[0].Extra["cache_control"] == nil {
		t.Fatalf("system part not marked: %+v", p)
	}
}

func TestCacheForOnlyAnthropic(t *testing.T) {
	t.Setenv("AGENT_MODEL", "anthropic:sonnet_5_5")
	if len(cacheFor("po")) != 1 {
		t.Fatal("anthropic model must be cached")
	}
	t.Setenv("PROMPT_CACHE", "off")
	if len(cacheFor("po")) != 0 {
		t.Fatal("PROMPT_CACHE=off must disable it")
	}
	t.Setenv("PROMPT_CACHE", "")
	t.Setenv("AGENT_MODEL", "openai:gpt-4o-mini")
	if len(cacheFor("po")) != 0 {
		t.Fatal("other providers must not get cache_control")
	}
}

func TestMarkLastCachesTheNewestTurn(t *testing.T) {
	in := []messages.Message{{Role: messages.RoleHuman, Content: "q"}, {Role: messages.RoleTool, ToolCallID: "1", Content: "big"}}
	out := markLast(in)
	if len(out[1].Parts) != 1 || out[1].Parts[0].Extra["cache_control"] == nil || out[1].Content != "big" {
		t.Fatalf("tool result not marked: %+v", out[1])
	}
	if len(in[1].Parts) != 0 {
		t.Fatal("input was modified")
	}
	h := markLast(in[:1])
	if h[0].Content != "" || h[0].Parts[0].Text != "q" || h[0].Parts[0].Extra["cache_control"] == nil {
		t.Fatalf("human not marked: %+v", h[0])
	}
}

func TestInferDocRoot(t *testing.T) {
	for in, want := range map[string]string{
		"Analise o projeto na pasta /a/b/front-end e faça um plano de desenvolvimento na pasta /a/b/front-end/plans/new": "/a/b/front-end/plans/new",
		"Analise /a/b":               "",
		"save the plan in /x/plans.": "/x/plans",
	} {
		if got := inferDocRoot(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestAgentBackendClaudeCLI(t *testing.T) {
	t.Setenv("AGENT_BACKEND", "claude-cli")
	t.Setenv("ANTHROPIC_API_KEY", "k")
	if got := ModelSpec("po"); got != "claude-cli:claude-opus-5-5" {
		t.Fatalf("got %q", got)
	}
	if cacheFor("po") != nil {
		t.Fatal("the API prompt cache must not wrap CLI calls")
	}
}
