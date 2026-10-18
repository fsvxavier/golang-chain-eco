package checkpoint_test

import (
	"reflect"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

func TestSerdeRoundTrip(t *testing.T) {
	cp := checkpoint.Checkpoint{
		ID: "a", ThreadID: "t", Step: 3, Source: "loop",
		Channels: map[string]any{
			"messages": []messages.Message{messages.Human("hi"), messages.AI("yo", messages.ToolCall{ID: "1", Name: "x", Args: map[string]any{"n": "2"}})},
			"count":    7,
			"list":     []any{"a", 1, messages.Human("nested")},
		},
		Next: []checkpoint.Task{{
			Node: "n", Arg: map[string]any{"k": "v"}, HasArg: true, Resumes: []any{"r", 5},
			Done: true, Writes: []checkpoint.Write{{Channel: "messages", Value: []messages.Message{messages.AI("w")}}},
			Interrupted: true, InterruptValue: "q?",
		}},
	}
	b, err := checkpoint.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := checkpoint.Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Channels["messages"], cp.Channels["messages"]) {
		t.Fatalf("messages: %#v", got.Channels["messages"])
	}
	if got.Channels["count"] != 7 || !reflect.DeepEqual(got.Channels["list"], cp.Channels["list"]) {
		t.Fatalf("%#v", got.Channels)
	}
	if !reflect.DeepEqual(got.Next, cp.Next) || got.Step != 3 {
		t.Fatalf("%#v", got.Next)
	}
}
