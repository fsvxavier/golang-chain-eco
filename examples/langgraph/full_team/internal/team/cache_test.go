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

func TestInferDocRoot(t *testing.T) {
	if got := inferDocRoot("Analise /a/b e faça um plano na pasta /a/b/plans/new"); got != "/a/b/plans/new" {
		t.Fatalf("got %q", got)
	}
}
