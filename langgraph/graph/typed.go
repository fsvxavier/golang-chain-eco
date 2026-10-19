package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
)

// Typed state layer.
//
// NewTyped[T] builds the Schema of a graph from the `graph` struct tags of T
// and gives typed nodes, routers and runs on top of the map-based runtime:
//
//	type MyState struct {
//	    Messages []messages.Message `graph:"messages,reducer=messages"`
//	    Count    int                `graph:"count"`
//	    Log      []string           `graph:"log,reducer=append"`
//	}
//
// Tag rules:
//   - `graph:"key"` maps the field to the state channel key. Exported fields
//     without a tag are skipped; `graph:"-"` skips a field explicitly.
//   - `reducer=last` (the default) keeps the last value written, like LastValue.
//   - `reducer=messages` uses the add_messages reducer; the field must be a slice.
//   - `reducer=append` concatenates the slice elements of each update; the field must be a slice.
//   - `reducer=sum` adds numeric updates; the field must be numeric.
//   - Unknown reducers, duplicate keys, unexported tagged fields and kind
//     mismatches are reported by Compile.
//
// Typed nodes return an Update (a partial map), not a T. A struct cannot say
// "this field was not updated" because its zero value is a valid value, so
// the node names the keys it writes; a key that is not a tag of T is an error.
//
// Typed Invoke with a T writes every tagged field, zero values included. To
// resume a run or update part of it, pass a Command.
//
// Validate: if T (or *T) has a method `Validate() error`, it runs on the input
// of every node and a non-nil error fails the run.
//
// Checkpoints: struct field types are registered with checkpoint.Register
// under "<package path>.<Name>" when NewTyped runs, so they survive a
// checkpoint with checkpoint.Strict(true). Anonymous struct types cannot be
// registered; with Strict on they make Marshal fail.
type TypedGraph[T any] struct {
	g      *StateGraph
	fields []typedField
	keys   map[string]bool
	err    error
}

// Update is the partial state returned by a typed node. Its keys must be
// tags of T.
type Update = State

type typedField struct {
	key   string
	index int
}

// NewTyped creates a graph builder whose state is the struct T. Errors in the
// tags are reported by Compile.
func NewTyped[T any]() *TypedGraph[T] {
	tg := &TypedGraph[T]{keys: map[string]bool{}}
	t := reflect.TypeFor[T]()
	schema := Schema{}
	if t.Kind() != reflect.Struct {
		tg.err = fmt.Errorf("graph: NewTyped needs a struct type, got %v", t)
		tg.g = NewStateGraph(schema)
		return tg
	}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag, ok := sf.Tag.Lookup("graph")
		if !ok || tag == "-" {
			continue
		}
		key, reducer, err := parseTag(tag)
		if err == nil && !sf.IsExported() {
			err = fmt.Errorf("field %s must be exported", sf.Name)
		}
		if err == nil {
			if _, dup := schema[key]; dup {
				err = fmt.Errorf("duplicate state key %q", key)
			}
		}
		var spec channels.Spec
		if err == nil {
			spec, err = reducerSpec(reducer, sf.Type)
		}
		if err != nil {
			tg.failf("graph: %v: field %s: %v", t, sf.Name, err)
			continue
		}
		schema[key] = spec
		tg.keys[key] = true
		tg.fields = append(tg.fields, typedField{key: key, index: i})
		registerFieldTypes(sf.Type)
	}
	tg.g = NewStateGraph(schema)
	return tg
}

func (tg *TypedGraph[T]) failf(format string, a ...any) {
	if tg.err == nil {
		tg.err = fmt.Errorf(format, a...)
	}
}

// parseTag parses `key[,reducer=name]`.
func parseTag(tag string) (key, reducer string, err error) {
	parts := strings.Split(tag, ",")
	key = strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", errors.New("empty state key in graph tag")
	}
	if key == InterruptKey || strings.HasPrefix(key, "join:") {
		return "", "", fmt.Errorf("state key %q is reserved", key)
	}
	reducer = "last"
	for _, p := range parts[1:] {
		name, val, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || name != "reducer" {
			return "", "", fmt.Errorf("unknown graph tag option %q", p)
		}
		reducer = val
	}
	return key, reducer, nil
}

// reducerSpec maps a reducer name to a channel spec and checks the field kind.
func reducerSpec(name string, t reflect.Type) (channels.Spec, error) {
	switch name {
	case "last":
		return channels.LastValue(), nil
	case "messages":
		if t.Kind() != reflect.Slice {
			return nil, fmt.Errorf("reducer messages needs a slice, got %v", t)
		}
		return Messages(), nil
	case "append":
		if t.Kind() != reflect.Slice {
			return nil, fmt.Errorf("reducer append needs a slice, got %v", t)
		}
		return typedAppend(), nil
	case "sum":
		switch t.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return Sum(), nil
		}
		return nil, fmt.Errorf("reducer sum needs a numeric field, got %v", t)
	}
	return nil, fmt.Errorf("unknown reducer %q (want last, messages, append or sum)", name)
}

// typedAppend is Append for typed fields: a slice update adds its elements,
// a nil update adds nothing (graph.Append would add the slice as one element
// and a nil as an element).
func typedAppend() channels.Spec {
	return channels.BinOp(func(cur, upd any) (any, error) {
		c, _ := cur.([]any)
		out := append([]any(nil), c...)
		if upd == nil {
			return out, nil
		}
		rv := reflect.ValueOf(upd)
		if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				out = append(out, rv.Index(i).Interface())
			}
			return out, nil
		}
		return append(out, upd), nil
	}, func() any { return []any{} })
}

// registerFieldTypes registers the named struct types under t so they survive
// a checkpoint with Strict(true). Pointers, slices, arrays and maps are walked.
func registerFieldTypes(t reflect.Type) {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
			continue
		}
		break
	}
	// messages.Message is registered by the checkpoint package under its own name.
	if t.Kind() != reflect.Struct || t.Name() == "" || t == reflect.TypeFor[messages.Message]() {
		return
	}
	checkpoint.Register(t.PkgPath()+"."+t.Name(), reflect.Zero(t).Interface())
}

// toState converts a T to the map the runtime takes (all tagged fields).
func (tg *TypedGraph[T]) toState(v T) State {
	rv := reflect.ValueOf(v)
	s := make(State, len(tg.fields))
	for _, f := range tg.fields {
		s[f.key] = rv.Field(f.index).Interface()
	}
	return s
}

// fromState fills the tagged fields of v from s. Missing keys become zero.
func (tg *TypedGraph[T]) fromState(s State, v *T) error {
	rv := reflect.ValueOf(v).Elem()
	for _, f := range tg.fields {
		fv := rv.Field(f.index)
		x, ok := s[f.key]
		if !ok || x == nil {
			fv.SetZero()
			continue
		}
		if err := assign(fv, x); err != nil {
			return fmt.Errorf("graph: state %q: %w", f.key, err)
		}
	}
	return nil
}

// assign sets dst to x, through a JSON round trip when the types differ
// (checkpoints restore values as maps and []any).
func assign(dst reflect.Value, x any) error {
	src := reflect.ValueOf(x)
	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return nil
	}
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	p := reflect.New(dst.Type())
	if err := json.Unmarshal(b, p.Interface()); err != nil {
		return err
	}
	dst.Set(p.Elem())
	return nil
}

type validator interface{ Validate() error }

// AddNode registers a typed node. The node's update keys must be tags of T;
// otherwise the node fails with an error.
func (tg *TypedGraph[T]) AddNode(name string, fn func(ctx context.Context, s T) (Update, error), opts ...NodeOption) *TypedGraph[T] {
	tg.g.AddNode(name, func(ctx context.Context, s State) (any, error) {
		var in T
		if err := tg.fromState(s, &in); err != nil {
			return nil, err
		}
		if v, ok := any(&in).(validator); ok {
			if err := v.Validate(); err != nil {
				return nil, fmt.Errorf("input invalid: %w", err)
			}
		}
		up, err := fn(ctx, in)
		if err != nil {
			return nil, err
		}
		for k := range up {
			if !tg.keys[k] {
				return nil, fmt.Errorf("graph: node %q: update key %q is not a graph tag of %v", name, k, reflect.TypeFor[T]())
			}
		}
		if up == nil {
			return nil, nil
		}
		return up, nil
	}, opts...)
	return tg
}

// AddEdge runs `to` after `from` finishes (see StateGraph.AddEdge).
func (tg *TypedGraph[T]) AddEdge(from, to string) *TypedGraph[T] {
	tg.g.AddEdge(from, to)
	return tg
}

// AddJoin runs `to` once every node in `from` has finished.
func (tg *TypedGraph[T]) AddJoin(from []string, to string) *TypedGraph[T] {
	tg.g.AddJoin(from, to)
	return tg
}

// SetEntryPoint is AddEdge(Start, name).
func (tg *TypedGraph[T]) SetEntryPoint(name string) *TypedGraph[T] {
	tg.g.SetEntryPoint(name)
	return tg
}

// SetFinishPoint is AddEdge(name, End).
func (tg *TypedGraph[T]) SetFinishPoint(name string) *TypedGraph[T] {
	tg.g.SetFinishPoint(name)
	return tg
}

// AddConditionalEdges routes after `from`. The router receives the state as T
// and returns what Router returns (a node name, a []string, a Send or nil).
func (tg *TypedGraph[T]) AddConditionalEdges(from string, router func(ctx context.Context, s T) (any, error), pathMap ...map[string]string) *TypedGraph[T] {
	tg.g.AddConditionalEdges(from, func(ctx context.Context, s State) (any, error) {
		var in T
		if err := tg.fromState(s, &in); err != nil {
			return nil, err
		}
		return router(ctx, in)
	}, pathMap...)
	return tg
}

// Graph returns the underlying map-based builder, for the features not wrapped here.
func (tg *TypedGraph[T]) Graph() *StateGraph { return tg.g }

// Compile validates the tags and the graph and returns the typed executable.
func (tg *TypedGraph[T]) Compile(opts ...CompileOption) (*TypedCompiled[T], error) {
	if tg.err != nil {
		return nil, tg.err
	}
	c, err := tg.g.Compile(opts...)
	if err != nil {
		return nil, err
	}
	return &TypedCompiled[T]{c: c, tg: tg}, nil
}

// TypedCompiled is the typed executable. Unwrap gives the *Compiled for
// GetState, StreamEvents, Saver and the other map-based features.
type TypedCompiled[T any] struct {
	c  *Compiled
	tg *TypedGraph[T]
}

// Unwrap returns the underlying compiled graph.
func (c *TypedCompiled[T]) Unwrap() *Compiled { return c.c }

// input converts a T (all tagged fields) or a Command (resume / update).
func (c *TypedCompiled[T]) input(in any) (any, error) {
	switch v := in.(type) {
	case T:
		return c.tg.toState(v), nil
	case Command:
		return v, nil
	}
	return nil, fmt.Errorf("graph: typed run takes %v or Command, got %T", reflect.TypeFor[T](), in)
}

// Invoke runs the graph with a T or a Command and returns the final state as
// T. The interrupts of a paused run are returned separately (empty when the
// run completed).
func (c *TypedCompiled[T]) Invoke(ctx context.Context, in any) (T, []InterruptInfo, error) {
	var out T
	input, err := c.input(in)
	if err != nil {
		return out, nil, err
	}
	st, err := c.c.Invoke(ctx, input)
	if err != nil {
		return out, nil, err
	}
	if err := c.tg.fromState(st, &out); err != nil {
		return out, nil, err
	}
	return out, Interrupts(st), nil
}

// Stream yields the state as T after each super-step. Interrupts are not
// visible here; use Unwrap().StreamEvents for them.
func (c *TypedCompiled[T]) Stream(ctx context.Context, in any) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		input, err := c.input(in)
		if err != nil {
			yield(zero, err)
			return
		}
		for st, err := range c.c.Stream(ctx, input) {
			var out T
			if err == nil {
				err = c.tg.fromState(st, &out)
			}
			if err != nil {
				yield(zero, err)
				return
			}
			if !yield(out, nil) {
				return
			}
		}
	}
}
