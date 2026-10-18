package checkpoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Serde lets persistent savers store arbitrary channel values as JSON.
// Plain JSON types round-trip as generic values (objects become
// map[string]any, whole numbers become int). Concrete Go types survive only
// if registered with Register; they are stored as {"$type": name, "$v": ...}.
// Registered values nested in []any or map[string]any are handled too.

var (
	regMu    sync.RWMutex
	byName   = map[string]reflect.Type{}
	byType   = map[reflect.Type]string{}
	typeKey  = "$type"
	valueKey = "$v"
)

// Register makes the dynamic type of sample serialisable under name.
// Registering the same name for another type panics.
func Register(name string, sample any) {
	t := reflect.TypeOf(sample)
	regMu.Lock()
	defer regMu.Unlock()
	if old, ok := byName[name]; ok && old != t {
		panic(fmt.Sprintf("checkpoint: type name %q already registered for %v", name, old))
	}
	byName[name], byType[t] = t, name
}

func init() {
	Register("messages.Message", messages.Message{})
	Register("[]messages.Message", []messages.Message{})
}

func encodeValue(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	regMu.RLock()
	name, ok := byType[reflect.TypeOf(v)]
	regMu.RUnlock()
	if ok {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return map[string]any{typeKey: name, valueKey: json.RawMessage(b)}, nil
	}
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			var err error
			if out[i], err = encodeValue(e); err != nil {
				return nil, err
			}
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			var err error
			if out[k], err = encodeValue(e); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return v, nil
}

func decodeValue(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil && i >= math.MinInt && i <= math.MaxInt {
			return int(i), nil
		}
		return x.Float64()
	case []any:
		for i, e := range x {
			var err error
			if x[i], err = decodeValue(e); err != nil {
				return nil, err
			}
		}
		return x, nil
	case map[string]any:
		if name, ok := x[typeKey].(string); ok && len(x) == 2 {
			regMu.RLock()
			t, known := byName[name]
			regMu.RUnlock()
			if !known {
				return nil, fmt.Errorf("checkpoint: unregistered type %q", name)
			}
			raw, _ := json.Marshal(x[valueKey])
			p := reflect.New(t)
			if err := json.Unmarshal(raw, p.Interface()); err != nil {
				return nil, fmt.Errorf("checkpoint: decoding %q: %w", name, err)
			}
			return p.Elem().Interface(), nil
		}
		for k, e := range x {
			var err error
			if x[k], err = decodeValue(e); err != nil {
				return nil, err
			}
		}
		return x, nil
	}
	return v, nil
}

type wireTask struct {
	Node           string  `json:"node"`
	Arg            any     `json:"arg,omitempty"`
	HasArg         bool    `json:"has_arg,omitempty"`
	Resumes        []any   `json:"resumes,omitempty"`
	Done           bool    `json:"done,omitempty"`
	Writes         []wrote `json:"writes,omitempty"`
	InterruptValue any     `json:"interrupt_value,omitempty"`
	Interrupted    bool    `json:"interrupted,omitempty"`
}

type wrote struct {
	Channel string `json:"channel"`
	Value   any    `json:"value"`
}

type wireCheckpoint struct {
	ID       string         `json:"id"`
	ParentID string         `json:"parent_id,omitempty"`
	ThreadID string         `json:"thread_id"`
	Step     int            `json:"step"`
	Source   string         `json:"source"`
	Channels map[string]any `json:"channels"`
	Next     []wireTask     `json:"next"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func encodeSlice(in []any) ([]any, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]any, len(in))
	for i, v := range in {
		var err error
		if out[i], err = encodeValue(v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Marshal serialises a checkpoint to JSON.
func Marshal(cp Checkpoint) ([]byte, error) {
	w := wireCheckpoint{ID: cp.ID, ParentID: cp.ParentID, ThreadID: cp.ThreadID, Step: cp.Step, Source: cp.Source}
	var err error
	w.Channels = map[string]any{}
	for k, v := range cp.Channels {
		if w.Channels[k], err = encodeValue(v); err != nil {
			return nil, fmt.Errorf("channel %q: %w", k, err)
		}
	}
	if m, err := encodeValue(cp.Metadata); err != nil {
		return nil, err
	} else if mm, ok := m.(map[string]any); ok {
		w.Metadata = mm
	}
	for _, t := range cp.Next {
		wt := wireTask{Node: t.Node, HasArg: t.HasArg, Done: t.Done, Interrupted: t.Interrupted}
		if wt.Arg, err = encodeValue(t.Arg); err != nil {
			return nil, err
		}
		if wt.InterruptValue, err = encodeValue(t.InterruptValue); err != nil {
			return nil, err
		}
		if wt.Resumes, err = encodeSlice(t.Resumes); err != nil {
			return nil, err
		}
		for _, wr := range t.Writes {
			ev, err := encodeValue(wr.Value)
			if err != nil {
				return nil, err
			}
			wt.Writes = append(wt.Writes, wrote{wr.Channel, ev})
		}
		w.Next = append(w.Next, wt)
	}
	return json.Marshal(w)
}

// Unmarshal restores a checkpoint written by Marshal.
func Unmarshal(b []byte) (Checkpoint, error) {
	var w wireCheckpoint
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&w); err != nil {
		return Checkpoint{}, err
	}
	cp := Checkpoint{ID: w.ID, ParentID: w.ParentID, ThreadID: w.ThreadID, Step: w.Step, Source: w.Source}
	var err error
	cp.Channels = map[string]any{}
	for k, v := range w.Channels {
		if cp.Channels[k], err = decodeValue(v); err != nil {
			return cp, fmt.Errorf("channel %q: %w", k, err)
		}
	}
	if w.Metadata != nil {
		m, err := decodeValue(w.Metadata)
		if err != nil {
			return cp, err
		}
		cp.Metadata = m.(map[string]any)
	}
	for _, wt := range w.Next {
		t := Task{Node: wt.Node, HasArg: wt.HasArg, Done: wt.Done, Interrupted: wt.Interrupted}
		if t.Arg, err = decodeValue(wt.Arg); err != nil {
			return cp, err
		}
		if t.InterruptValue, err = decodeValue(wt.InterruptValue); err != nil {
			return cp, err
		}
		for _, r := range wt.Resumes {
			dv, err := decodeValue(r)
			if err != nil {
				return cp, err
			}
			t.Resumes = append(t.Resumes, dv)
		}
		for _, wr := range wt.Writes {
			dv, err := decodeValue(wr.Value)
			if err != nil {
				return cp, err
			}
			t.Writes = append(t.Writes, Write{wr.Channel, dv})
		}
		cp.Next = append(cp.Next, t)
	}
	return cp, nil
}
