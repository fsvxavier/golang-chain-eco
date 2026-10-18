package agents_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type args struct {
	City string `json:"city"`
}

func weather() tools.Tool {
	return tools.New("weather", "weather", func(_ context.Context, a args) (any, error) { return "sunny in " + a.City, nil })
}

func call(id string) messages.Message {
	return messages.AI("", messages.ToolCall{ID: id, Name: "weather", Args: map[string]any{"city": "Rome"}})
}

func msgsOf(out graph.State) []messages.Message { return out["messages"].([]messages.Message) }

func TestAgentLoopAndPII(t *testing.T) {
	fake := models.NewFake(call("1"), messages.AI("done"))
	ag, err := agents.Create(agents.Options{
		Model: fake, Tools: []tools.Tool{weather()}, SystemPrompt: "sys",
		Middleware: []agents.Middleware{middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ag.Invoke(context.Background(), graph.State{"messages": "mail me at a@b.com"})
	if err != nil {
		t.Fatal(err)
	}
	ms := msgsOf(out)
	if len(ms) != 4 || strings.Contains(ms[0].Content, "a@b.com") || ms[2].Content != "sunny in Rome" {
		t.Fatalf("%+v", ms)
	}
}

func TestHumanInTheLoop(t *testing.T) {
	run := func(d middleware.HITLDecision) []messages.Message {
		fake := models.NewFake(call("1"), messages.AI("ok"))
		ag, _ := agents.Create(agents.Options{
			Model: fake, Tools: []tools.Tool{weather()},
			Middleware: []agents.Middleware{middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"weather": {}}}},
			Compile:    []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())},
		})
		ctx := runnables.WithConfig(context.Background(), graph.Thread("h"))
		out, err := ag.Invoke(ctx, graph.State{"messages": "hi"})
		if err != nil || len(graph.Interrupts(out)) != 1 {
			t.Fatalf("%v %v", out, err)
		}
		out, err = ag.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: []middleware.HITLDecision{d}}})
		if err != nil {
			t.Fatal(err)
		}
		return msgsOf(out)
	}
	ms := run(middleware.HITLDecision{Type: middleware.Approve})
	if ms[2].Content != "sunny in Rome" {
		t.Fatalf("%+v", ms)
	}
	ms = run(middleware.HITLDecision{Type: middleware.Edit, EditedAction: &middleware.ActionRequest{Name: "weather", Args: map[string]any{"city": "Oslo"}}})
	if ms[2].Content != "sunny in Oslo" {
		t.Fatalf("%+v", ms)
	}
	ms = run(middleware.HITLDecision{Type: middleware.Reject, Message: "no"})
	if ms[2].Content != "no" || ms[2].Status != "error" || ms[len(ms)-1].Content != "ok" {
		t.Fatalf("%+v", ms)
	}
}

func TestModelCallLimitAndSummarization(t *testing.T) {
	fake := models.NewFake(call("1"), call("2"), call("3"))
	ag, _ := agents.Create(agents.Options{
		Model: fake, Tools: []tools.Tool{weather()},
		Middleware: []agents.Middleware{middleware.ModelCallLimit{RunLimit: 2}},
	})
	out, err := ag.Invoke(context.Background(), graph.State{"messages": "go"})
	if err != nil {
		t.Fatal(err)
	}
	ms := msgsOf(out)
	if last := ms[len(ms)-1]; !strings.Contains(last.Content, "limits exceeded") {
		t.Fatalf("%+v", ms)
	}

	sum := models.NewFake(messages.AI("SUMMARY"))
	ag, _ = agents.Create(agents.Options{
		Model:      models.NewFake(messages.AI("fine")),
		Middleware: []agents.Middleware{middleware.Summarization{Model: sum, MaxTokens: 10, KeepMessages: 2}},
	})
	in := []messages.Message{messages.Human(strings.Repeat("a ", 40)), messages.AI(strings.Repeat("b ", 40)), messages.Human("c")}
	out, err = ag.Invoke(context.Background(), graph.State{"messages": in})
	if err != nil {
		t.Fatal(err)
	}
	ms = msgsOf(out)
	if len(ms) != 4 || !strings.Contains(ms[0].Content, "SUMMARY") {
		t.Fatalf("%+v", ms)
	}
}

func TestTodoList(t *testing.T) {
	fake := models.NewFake(
		messages.AI("", messages.ToolCall{ID: "t", Name: "write_todos", Args: map[string]any{"todos": []any{map[string]any{"content": "x", "status": "pending"}}}}),
		messages.AI("done"),
	)
	ag, _ := agents.Create(agents.Options{Model: fake, Middleware: []agents.Middleware{middleware.TodoList{}}})
	out, err := ag.Invoke(context.Background(), graph.State{"messages": "plan"})
	if err != nil || out["todos"] == nil {
		t.Fatalf("%v %v", out, err)
	}
}
