package prebuilt_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

type addArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

func TestReactAgent(t *testing.T) {
	add := tools.New("add", "adds", func(_ context.Context, a addArgs) (any, error) { return a.A + a.B, nil })
	fake := models.NewFake(
		messages.AI("", messages.ToolCall{ID: "1", Name: "add", Args: map[string]any{"a": 2, "b": 3}},
			messages.ToolCall{ID: "2", Name: "nope", Args: map[string]any{}}),
		messages.AI("the answer is 5"),
	)
	ag, err := prebuilt.CreateReactAgent(prebuilt.ReactOptions{Model: fake, Tools: []tools.Tool{add}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ag.Invoke(context.Background(), graph.State{"messages": "2+3?"})
	if err != nil {
		t.Fatal(err)
	}
	ms := out["messages"].([]messages.Message)
	if len(ms) != 5 || ms[2].Content != "5" || ms[3].Status != "error" || !strings.Contains(ms[4].Content, "5") {
		t.Fatalf("%+v", ms)
	}
}
