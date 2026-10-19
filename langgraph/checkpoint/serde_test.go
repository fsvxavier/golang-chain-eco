package checkpoint_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

type unregisteredPlan struct{ Steps int }

type registeredPlan struct{ Steps int }

type namedList []unregisteredPlan

func init() {
	checkpoint.Register("checkpoint_test.registeredPlan", registeredPlan{})
}

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

// scalars are the Go types plain JSON would widen or lose.
var scalars = []any{
	int8(-8), int16(-16), int32(-32), int64(1<<62 + 3),
	uint(9), uint8(255), uint16(65535), uint32(1 << 31), uint64(1<<64 - 1),
	float32(1.25), []byte("raw\x00bytes"), time.Date(2026, 10, 19, 12, 30, 0, 123, time.UTC),
	time.Duration(90 * time.Second),
}

func TestScalarRoundTripInChannelsSliceAndMap(t *testing.T) {
	for _, v := range scalars {
		cp := checkpoint.Checkpoint{ThreadID: "t", Channels: map[string]any{
			"direct": v,
			"list":   []any{v, "s"},
			"nested": map[string]any{"k": v},
		}}
		b, err := checkpoint.Marshal(cp)
		if err != nil {
			t.Fatalf("%T: %v", v, err)
		}
		got, err := checkpoint.Unmarshal(b)
		if err != nil {
			t.Fatalf("%T: %v", v, err)
		}
		if !reflect.DeepEqual(got.Channels["direct"], v) {
			t.Errorf("direct %T: got %#v want %#v", v, got.Channels["direct"], v)
		}
		if !reflect.DeepEqual(got.Channels["list"], []any{v, "s"}) {
			t.Errorf("list %T: got %#v", v, got.Channels["list"])
		}
		if !reflect.DeepEqual(got.Channels["nested"], map[string]any{"k": v}) {
			t.Errorf("nested %T: got %#v", v, got.Channels["nested"])
		}
	}
}

// Plain int, float64, string and bool keep their JSON encoding, and payloads
// written before the scalar envelopes existed decode exactly as before.
func TestOldPayloadDecodesUnchanged(t *testing.T) {
	old := []byte(`{"id":"c1","thread_id":"t","step":2,"source":"loop",
		"channels":{"n":7,"f":1.5,"big":12345678901234,"s":"x","b":true,"z":null,
			"l":[1,"a"],"m":{"k":2}},
		"next":[{"node":"n","arg":3,"has_arg":true}],
		"metadata":{"source":"old"}}`)
	cp, err := checkpoint.Unmarshal(old)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"n": 7, "f": 1.5, "big": 12345678901234, "s": "x", "b": true, "z": nil,
		"l": []any{1, "a"}, "m": map[string]any{"k": 2},
	}
	for k, w := range want {
		if !reflect.DeepEqual(cp.Channels[k], w) {
			t.Errorf("%s: got %#v (%T) want %#v (%T)", k, cp.Channels[k], cp.Channels[k], w, w)
		}
	}
	if cp.Next[0].Arg != 3 || cp.Metadata["source"] != "old" || cp.ID != "c1" || cp.Step != 2 {
		t.Fatalf("%#v %#v", cp.Next, cp.Metadata)
	}

	// Plain values still encode without an envelope.
	b, err := checkpoint.Marshal(checkpoint.Checkpoint{ThreadID: "t", Channels: map[string]any{"n": 7, "f": 1.5, "s": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "$type") {
		t.Fatalf("plain values wrapped: %s", b)
	}
}

func TestStrictOffKeepsDegradation(t *testing.T) {
	b, err := checkpoint.Marshal(checkpoint.Checkpoint{ThreadID: "t", Channels: map[string]any{"p": unregisteredPlan{Steps: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := checkpoint.Unmarshal(b)
	if !reflect.DeepEqual(got.Channels["p"], map[string]any{"Steps": 2}) {
		t.Fatalf("%#v", got.Channels["p"])
	}
}

func TestStrictRejectsUnregistered(t *testing.T) {
	checkpoint.Strict(true)
	defer checkpoint.Strict(false)

	bad := map[string]any{
		"struct":     unregisteredPlan{Steps: 1},
		"pointer":    &unregisteredPlan{Steps: 1},
		"slice":      []unregisteredPlan{{Steps: 1}},
		"named":      namedList{{Steps: 1}},
		"nestedList": []any{map[string]any{"p": unregisteredPlan{}}},
	}
	for k, v := range bad {
		_, err := checkpoint.Marshal(checkpoint.Checkpoint{ThreadID: "t", Channels: map[string]any{k: v}})
		if err == nil {
			t.Errorf("%s (%T): expected error", k, v)
			continue
		}
		if !strings.Contains(err.Error(), "checkpoint.Register") {
			t.Errorf("%s: unhelpful error %q", k, err)
		}
	}

	// Registered structs pass, and scalars and plain values are untouched.
	cp := checkpoint.Checkpoint{ThreadID: "t", Channels: map[string]any{
		"plan": registeredPlan{Steps: 4}, "n": int64(5), "s": "x", "tags": []string{"a"},
	}}
	b, err := checkpoint.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := checkpoint.Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channels["plan"] != (registeredPlan{Steps: 4}) || got.Channels["n"] != int64(5) {
		t.Fatalf("%#v", got.Channels)
	}
}
