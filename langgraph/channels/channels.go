// Package channels implements LangGraph channels: the typed slots that hold
// graph state between Pregel super-steps (langgraph.channels).
//
// Each step, every task's writes are grouped by channel and passed to
// Update. Channels that receive no writes get Update(nil), which lets
// ephemeral channels clear themselves.
package channels

import (
	"fmt"
	"maps"
	"slices"
	"sort"
)

// Channel is a state slot.
type Channel interface {
	// Get returns the value and whether the channel holds one.
	Get() (any, bool)
	// Update applies the writes of one step and reports whether the value changed.
	Update(values []any) (bool, error)
	// Checkpoint returns a snapshot of the channel (ok=false when empty).
	Checkpoint() (any, bool)
	// Restore returns a new channel of the same kind holding snapshot.
	Restore(snapshot any, ok bool) Channel
	// Consume is called after a task triggered by this channel ran.
	Consume() bool
}

// Spec builds a fresh, empty channel.
type Spec func() Channel

// InvalidUpdateError reports an illegal write (e.g. two writes to a LastValue
// channel in the same step).
type InvalidUpdateError struct{ Msg string }

func (e *InvalidUpdateError) Error() string { return e.Msg }

// Overwrite bypasses a reducer and replaces the channel value.
type Overwrite struct{ Value any }

// ------------------------------------------------------------- LastValue

type lastValue struct {
	v  any
	ok bool
}

// LastValue stores the last value written; more than one write per step is
// an error (the default for un-annotated state keys).
func LastValue() Spec { return func() Channel { return &lastValue{} } }

func (c *lastValue) Get() (any, bool) { return c.v, c.ok }
func (c *lastValue) Update(vs []any) (bool, error) {
	if len(vs) == 0 {
		return false, nil
	}
	if len(vs) > 1 {
		return false, &InvalidUpdateError{Msg: "can receive only one value per step; use a reducer to handle multiple values"}
	}
	v := vs[0]
	if o, ok := v.(Overwrite); ok {
		v = o.Value
	}
	c.v, c.ok = v, true
	return true, nil
}
func (c *lastValue) Checkpoint() (any, bool)           { return c.v, c.ok }
func (c *lastValue) Restore(v any, ok bool) Channel    { return &lastValue{v, ok} }
func (c *lastValue) Consume() bool                     { return false }

// ------------------------------------------------------------- AnyValue

type anyValue struct{ lastValue }

// AnyValue stores the last of possibly many writes (all assumed equal).
func AnyValue() Spec { return func() Channel { return &anyValue{} } }

func (c *anyValue) Update(vs []any) (bool, error) {
	if len(vs) == 0 {
		if !c.ok {
			return false, nil
		}
		c.v, c.ok = nil, false
		return true, nil
	}
	c.v, c.ok = vs[len(vs)-1], true
	return true, nil
}
func (c *anyValue) Restore(v any, ok bool) Channel { return &anyValue{lastValue{v, ok}} }

// ------------------------------------------------------------- Ephemeral

type ephemeral struct {
	lastValue
	guard bool
}

// Ephemeral holds a value for exactly one step, then clears it. With guard,
// more than one write per step is an error.
func Ephemeral(guard bool) Spec { return func() Channel { return &ephemeral{guard: guard} } }

func (c *ephemeral) Update(vs []any) (bool, error) {
	if len(vs) == 0 {
		if !c.ok {
			return false, nil
		}
		c.v, c.ok = nil, false
		return true, nil
	}
	if c.guard && len(vs) > 1 {
		return false, &InvalidUpdateError{Msg: "ephemeral channel can receive only one value per step"}
	}
	c.v, c.ok = vs[len(vs)-1], true
	return true, nil
}
func (c *ephemeral) Restore(v any, ok bool) Channel { return &ephemeral{lastValue{v, ok}, c.guard} }

// ------------------------------------------------------------- BinOp

// Reducer folds an update into the current value.
type Reducer func(current, update any) (any, error)

type binop struct {
	reduce  Reducer
	initial func() any
	v       any
	ok      bool
}

// BinOp folds every write into the value with reduce
// (BinaryOperatorAggregate, i.e. Annotated[T, reducer]). initial, if set,
// provides the starting value so the channel is never empty.
func BinOp(reduce Reducer, initial func() any) Spec {
	return func() Channel {
		c := &binop{reduce: reduce, initial: initial}
		if initial != nil {
			c.v, c.ok = initial(), true
		}
		return c
	}
}

func (c *binop) Get() (any, bool) { return c.v, c.ok }
func (c *binop) Update(vs []any) (bool, error) {
	if len(vs) == 0 {
		return false, nil
	}
	seenOverwrite := false
	for _, u := range vs {
		if o, ok := u.(Overwrite); ok {
			if seenOverwrite {
				return false, &InvalidUpdateError{Msg: "can receive only one Overwrite value per step"}
			}
			seenOverwrite = true
			c.v, c.ok = o.Value, true
			continue
		}
		if seenOverwrite {
			continue
		}
		if !c.ok {
			if c.initial != nil {
				c.v = c.initial()
			} else {
				c.v, c.ok = u, true
				// Still run the reducer with a zero current so it can normalise u.
				nv, err := c.reduce(nil, u)
				if err != nil {
					return false, err
				}
				c.v = nv
				continue
			}
		}
		nv, err := c.reduce(c.v, u)
		if err != nil {
			return false, err
		}
		c.v, c.ok = nv, true
	}
	return true, nil
}
func (c *binop) Checkpoint() (any, bool) { return c.v, c.ok }
func (c *binop) Restore(v any, ok bool) Channel {
	n := &binop{reduce: c.reduce, initial: c.initial, v: v, ok: ok}
	if !ok && c.initial != nil {
		n.v, n.ok = c.initial(), true
	}
	return n
}
func (c *binop) Consume() bool { return false }

// ------------------------------------------------------------- Topic

type topic struct {
	accumulate bool
	vals       []any
}

// Topic collects every value written (a pub/sub list). Without accumulate it
// is cleared at the start of each step.
func Topic(accumulate bool) Spec { return func() Channel { return &topic{accumulate: accumulate} } }

func (c *topic) Get() (any, bool) {
	if len(c.vals) == 0 {
		return nil, false
	}
	return slices.Clone(c.vals), true
}
func (c *topic) Update(vs []any) (bool, error) {
	changed := false
	if !c.accumulate && len(c.vals) > 0 {
		c.vals, changed = nil, true
	}
	for _, v := range vs {
		if list, ok := v.([]any); ok {
			c.vals = append(c.vals, list...)
		} else {
			c.vals = append(c.vals, v)
		}
		changed = true
	}
	return changed, nil
}
func (c *topic) Checkpoint() (any, bool) {
	if len(c.vals) == 0 {
		return nil, false
	}
	return slices.Clone(c.vals), true
}
func (c *topic) Restore(v any, ok bool) Channel {
	n := &topic{accumulate: c.accumulate}
	if ok {
		if list, isList := v.([]any); isList {
			n.vals = slices.Clone(list)
		}
	}
	return n
}
func (c *topic) Consume() bool { return false }

// ------------------------------------------------------------- NamedBarrier

type barrier struct {
	names map[string]bool
	seen  map[string]bool
}

// NamedBarrier becomes available once every name has been written; it is
// used for edges that wait on several nodes (add_edge([a, b], c)).
func NamedBarrier(names ...string) Spec {
	return func() Channel {
		n := map[string]bool{}
		for _, s := range names {
			n[s] = true
		}
		return &barrier{names: n, seen: map[string]bool{}}
	}
}

func (c *barrier) Get() (any, bool) {
	if len(c.seen) != len(c.names) {
		return nil, false
	}
	return nil, true
}
func (c *barrier) Update(vs []any) (bool, error) {
	changed := false
	for _, v := range vs {
		s, _ := v.(string)
		if !c.names[s] {
			return false, &InvalidUpdateError{Msg: fmt.Sprintf("barrier received unexpected value %v", v)}
		}
		if !c.seen[s] {
			c.seen[s], changed = true, true
		}
	}
	return changed, nil
}
func (c *barrier) Checkpoint() (any, bool) {
	keys := make([]any, 0, len(c.seen))
	for k := range c.seen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].(string) < keys[j].(string) })
	return keys, true
}
func (c *barrier) Restore(v any, ok bool) Channel {
	n := &barrier{names: maps.Clone(c.names), seen: map[string]bool{}}
	if list, isList := v.([]any); ok && isList {
		for _, s := range list {
			if str, ok := s.(string); ok {
				n.seen[str] = true
			}
		}
	}
	return n
}
func (c *barrier) Consume() bool {
	if len(c.seen) == len(c.names) {
		c.seen = map[string]bool{}
		return true
	}
	return false
}

// Copy duplicates a channel via its checkpoint.
func Copy(c Channel) Channel {
	v, ok := c.Checkpoint()
	return c.Restore(v, ok)
}
