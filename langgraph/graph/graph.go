// Package graph is the Go port of langgraph's StateGraph and Pregel runtime:
// nodes read a shared state, return partial updates, and are executed in
// bulk-synchronous super-steps with optional checkpointing and human-in-the-
// loop interrupts.
//
// Subgraphs run by a node have checkpoint namespaces ("node:id|node:id"). Their
// stream events are forwarded to the enclosing stream only when it asks for
// Subgraphs(); each event then carries Event.Namespace with the same path.
//
// A checkpointed graph writes its checkpoints according to its Durability:
// DurabilitySync (default) before each super-step, DurabilityAsync from a
// background goroutine (Invoke and Stream still return after the last write),
// or DurabilityExit only when the run pauses, ends or fails between
// super-steps. The compile-time default is set with WithDurability; a run
// overrides it with WithRunDurability. Pauses of subgraphs with their own
// checkpointer are always saved.
//
// The state is a map (State). For a struct state, NewTyped[T] builds the
// Schema from the `graph:"key[,reducer=name]"` tags of T and wraps nodes, runs
// and routers with T: nodes return an Update (a partial map validated against
// the tags), runs take and return T, and a Validate() error method on T guards
// every node's input. The map-based API is unchanged; see typed.go.
package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Reserved node names.
const (
	Start = "__start__"
	End   = "__end__"
	// InterruptKey is added to the returned state when a run pauses.
	InterruptKey = "__interrupt__"
)

// State is the graph state: one entry per channel.
type State = map[string]any

// NodeFunc is a graph node. It returns nil, a State (partial update), or a
// Command.
type NodeFunc func(ctx context.Context, state State) (any, error)

// Schema declares the state channels. Keys without an entry cannot be written.
type Schema map[string]channels.Spec

// Send invokes Node with Arg as its input state in the next super-step
// (map-reduce fan-out).
type Send struct {
	Node string
	Arg  State
}

// Command combines a state update with control flow. Returned from a node it
// updates state and routes; passed as run input it resumes an interrupt.
type Command struct {
	Update State
	Goto   []string
	Send   []Send
	// Resume answers the pending interrupt(s) of a paused run.
	Resume any
	// ResumeMap answers specific interrupts by Interrupt.ID.
	ResumeMap map[string]any
}

// Router picks the next node(s): a string, []string, Send, []Send or nil.
type Router func(ctx context.Context, state State) (any, error)

// Messages is the channel spec for a chat-message list with the add_messages
// reducer (IDs are assigned, equal IDs replace, RemoveMessage deletes).
func Messages() channels.Spec {
	return channels.BinOp(func(cur, upd any) (any, error) {
		l, err := messages.Coerce(cur)
		if err != nil {
			return nil, err
		}
		r, err := messages.Coerce(upd)
		if err != nil {
			return nil, err
		}
		return messages.AddMessages(l, r)
	}, func() any { return []messages.Message{} })
}

// Append is a channel spec that concatenates []any / slice updates.
func Append() channels.Spec {
	return channels.BinOp(func(cur, upd any) (any, error) {
		c, _ := cur.([]any)
		switch u := upd.(type) {
		case []any:
			return append(append([]any(nil), c...), u...), nil
		default:
			return append(append([]any(nil), c...), u), nil
		}
	}, func() any { return []any{} })
}

// Sum is a channel spec adding numeric updates.
func Sum() channels.Spec {
	return channels.BinOp(func(cur, upd any) (any, error) {
		a, ok1 := toFloat(cur)
		b, ok2 := toFloat(upd)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("sum: non-numeric %T + %T", cur, upd)
		}
		if _, isInt := cur.(int); isInt {
			if _, isInt2 := upd.(int); isInt2 {
				return int(a + b), nil
			}
		}
		return a + b, nil
	}, func() any { return 0 })
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

type node struct {
	fn       NodeFunc
	retry    *runnables.RetryOptions
	cache    *CachePolicy
	deferred bool
}

// CachePolicy memoises a node's writes per input state.
type CachePolicy struct {
	// TTL of an entry; zero keeps it forever.
	TTL time.Duration
	// Key derives the cache key from the node input (default: JSON of the input).
	Key func(State) string
}

// Cache stores node results. Implementations must be safe for concurrent use.
type Cache interface {
	Get(key string) ([]checkpoint.Write, bool)
	Set(key string, ws []checkpoint.Write, ttl time.Duration)
}

type memEntry struct {
	ws  []checkpoint.Write
	exp time.Time
}

// MemoryCache is an in-process Cache.
type MemoryCache struct {
	mu sync.Mutex
	m  map[string]memEntry
}

// NewMemoryCache returns an empty cache.
func NewMemoryCache() *MemoryCache { return &MemoryCache{m: map[string]memEntry{}} }

func (c *MemoryCache) Get(k string) ([]checkpoint.Write, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if ok && !e.exp.IsZero() && time.Now().After(e.exp) {
		delete(c.m, k)
		return nil, false
	}
	return e.ws, ok
}

func (c *MemoryCache) Set(k string, ws []checkpoint.Write, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := memEntry{ws: ws}
	if ttl > 0 {
		e.exp = time.Now().Add(ttl)
	}
	c.m[k] = e
}

// WithCachePolicy memoises the node (needs the WithCache compile option).
func WithCachePolicy(p CachePolicy) NodeOption { return func(n *node) { n.cache = &p } }

// Deferred delays the node until no other tasks are pending, so it runs once
// after every parallel branch has finished (defer=True).
func Deferred() NodeOption { return func(n *node) { n.deferred = true } }

// NodeOption customises a node.
type NodeOption func(*node)

// WithRetry retries the node on error with exponential backoff.
func WithRetry(o runnables.RetryOptions) NodeOption { return func(n *node) { n.retry = &o } }

type join struct {
	from []string
	to   string
	ch   string
}

// StateGraph is the graph builder. Methods chain; the first error is
// reported by Compile.
type StateGraph struct {
	schema   Schema
	nodes    map[string]*node
	order    []string
	edges    map[string][]string
	joins    []join
	branches map[string][]Router
	err      error
}

// NewStateGraph creates a builder for the given state schema.
func NewStateGraph(schema Schema) *StateGraph {
	return &StateGraph{schema: schema, nodes: map[string]*node{}, edges: map[string][]string{}, branches: map[string][]Router{}}
}

func (g *StateGraph) fail(format string, a ...any) *StateGraph {
	if g.err == nil {
		g.err = fmt.Errorf(format, a...)
	}
	return g
}

// AddNode registers a node.
func (g *StateGraph) AddNode(name string, fn NodeFunc, opts ...NodeOption) *StateGraph {
	if name == "" || name == Start || name == End || strings.Contains(name, ":") {
		return g.fail("graph: invalid node name %q", name)
	}
	if _, dup := g.nodes[name]; dup {
		return g.fail("graph: node %q already exists", name)
	}
	n := &node{fn: fn}
	for _, o := range opts {
		o(n)
	}
	g.nodes[name] = n
	g.order = append(g.order, name)
	return g
}

// AddEdge runs `to` after `from` finishes.
func (g *StateGraph) AddEdge(from, to string) *StateGraph {
	g.edges[from] = append(g.edges[from], to)
	return g
}

// AddJoin runs `to` once every node in `from` has finished (add_edge([a,b], c)).
func (g *StateGraph) AddJoin(from []string, to string) *StateGraph {
	g.joins = append(g.joins, join{from: from, to: to, ch: "join:" + strings.Join(from, ",") + ":" + to})
	return g
}

// AddSequence adds nodes in order and chains them with edges.
func (g *StateGraph) AddSequence(names []string, fns []NodeFunc) *StateGraph {
	for i, n := range names {
		g.AddNode(n, fns[i])
		if i > 0 {
			g.AddEdge(names[i-1], n)
		}
	}
	return g
}

// AddConditionalEdges routes after `from` using router. pathMap, if given,
// translates the router's string results to node names.
func (g *StateGraph) AddConditionalEdges(from string, router Router, pathMap ...map[string]string) *StateGraph {
	if len(pathMap) > 0 && pathMap[0] != nil {
		pm, inner := pathMap[0], router
		router = func(ctx context.Context, s State) (any, error) {
			r, err := inner(ctx, s)
			if err != nil {
				return nil, err
			}
			return mapPath(r, pm), nil
		}
	}
	g.branches[from] = append(g.branches[from], router)
	return g
}

func mapPath(r any, pm map[string]string) any {
	tr := func(s string) string {
		if t, ok := pm[s]; ok {
			return t
		}
		return s
	}
	switch v := r.(type) {
	case string:
		return tr(v)
	case []string:
		out := make([]string, len(v))
		for i, s := range v {
			out[i] = tr(s)
		}
		return out
	}
	return r
}

// SetEntryPoint is AddEdge(Start, name).
func (g *StateGraph) SetEntryPoint(name string) *StateGraph { return g.AddEdge(Start, name) }

// SetFinishPoint is AddEdge(name, End).
func (g *StateGraph) SetFinishPoint(name string) *StateGraph { return g.AddEdge(name, End) }

// CompileOption configures Compile.
type CompileOption func(*Compiled)

// WithCheckpointer enables persistence and thread resumption.
func WithCheckpointer(s checkpoint.Saver) CompileOption { return func(c *Compiled) { c.saver = s } }

// WithCache enables node caching for nodes with a CachePolicy.
func WithCache(c Cache) CompileOption { return func(x *Compiled) { x.cache = c } }

// WithStore attaches a long-term memory store, readable with StoreFrom.
func WithStore(s store.Store) CompileOption { return func(c *Compiled) { c.store = s } }

// InterruptBefore pauses before the named nodes run ("*" for all).
func InterruptBefore(nodes ...string) CompileOption {
	return func(c *Compiled) { c.before = toSet(nodes) }
}

// InterruptAfter pauses after the named nodes ran ("*" for all).
func InterruptAfter(nodes ...string) CompileOption {
	return func(c *Compiled) { c.after = toSet(nodes) }
}

// Durability sets when checkpoints are written.
type Durability string

const (
	// DurabilitySync stores a checkpoint at every super-step before the next
	// one starts. It is the default.
	DurabilitySync Durability = "sync"
	// DurabilityAsync stores checkpoints from a background goroutine, in
	// order. A save waits for the previous one when the queue is full. Invoke
	// and Stream return only after the last save; a failed save aborts the run
	// at the next super-step.
	DurabilityAsync Durability = "async"
	// DurabilityExit stores a checkpoint only when the run pauses (interrupt),
	// finishes or fails between super-steps. Faster, but a crash mid-run loses
	// the steps since the last checkpoint.
	DurabilityExit Durability = "exit"
)

// WithDurability sets the default durability of the graph. A run overrides it
// with WithRunDurability.
func WithDurability(d Durability) CompileOption { return func(c *Compiled) { c.durability = d } }

// WithRunDurability returns a context that makes the runs started with it use
// durability d, overriding the compile-time default (sets
// runnables.Config.Durability). Subgraphs run inside it inherit d.
func WithRunDurability(ctx context.Context, d Durability) context.Context {
	return runnables.WithConfig(ctx, runnables.Config{Durability: string(d)})
}

// WithName names the compiled graph (used in traces).
func WithName(n string) CompileOption { return func(c *Compiled) { c.name = n } }

func toSet(ns []string) map[string]bool {
	m := map[string]bool{}
	for _, n := range ns {
		m[n] = true
	}
	return m
}

// Compile validates the graph and returns the executable.
func (g *StateGraph) Compile(opts ...CompileOption) (*Compiled, error) {
	if g.err != nil {
		return nil, g.err
	}
	check := func(n string, allowStart, allowEnd bool) error {
		if _, ok := g.nodes[n]; ok || (allowStart && n == Start) || (allowEnd && n == End) {
			return nil
		}
		return fmt.Errorf("graph: unknown node %q", n)
	}
	for from, tos := range g.edges {
		if err := check(from, true, false); err != nil {
			return nil, err
		}
		for _, to := range tos {
			if err := check(to, false, true); err != nil {
				return nil, err
			}
		}
	}
	for from := range g.branches {
		if err := check(from, true, false); err != nil {
			return nil, err
		}
	}
	for _, j := range g.joins {
		for _, f := range j.from {
			if err := check(f, false, false); err != nil {
				return nil, err
			}
		}
		if err := check(j.to, false, true); err != nil {
			return nil, err
		}
	}
	if len(g.edges[Start]) == 0 && len(g.branches[Start]) == 0 {
		return nil, errors.New("graph: no entry point (add an edge from Start)")
	}
	c := &Compiled{g: g, name: "LangGraph", specs: map[string]channels.Spec{}}
	for k, s := range g.schema {
		c.specs[k] = s
	}
	for _, j := range g.joins {
		c.specs[j.ch] = channels.NamedBarrier(j.from...)
	}
	for _, o := range opts {
		o(c)
	}
	for n := range union(c.before, c.after) {
		if n != "*" {
			if _, ok := g.nodes[n]; !ok {
				return nil, fmt.Errorf("graph: interrupt on unknown node %q", n)
			}
		}
	}
	return c, nil
}

func union(a, b map[string]bool) map[string]bool {
	m := map[string]bool{}
	for k := range a {
		m[k] = true
	}
	for k := range b {
		m[k] = true
	}
	return m
}

// Thread returns a run configuration selecting a persistence thread.
func Thread(id string) runnables.Config {
	return runnables.Config{Configurable: map[string]any{"thread_id": id}}
}

// Saver returns the checkpointer the graph was compiled with (nil if none).
func (c *Compiled) Saver() checkpoint.Saver { return c.saver }

// Nodes returns the node names in insertion order.
func (c *Compiled) Nodes() []string { return append([]string(nil), c.g.order...) }

// Mermaid renders the graph as a Mermaid flowchart (draw_mermaid).
func (c *Compiled) Mermaid() string {
	var b strings.Builder
	b.WriteString("graph TD;\n")
	name := func(n string) string {
		switch n {
		case Start:
			return "__start__([__start__])"
		case End:
			return "__end__([__end__])"
		}
		return n
	}
	froms := make([]string, 0, len(c.g.edges))
	for f := range c.g.edges {
		froms = append(froms, f)
	}
	sort.Strings(froms)
	for _, f := range froms {
		for _, t := range c.g.edges[f] {
			fmt.Fprintf(&b, "\t%s --> %s;\n", name(f), name(t))
		}
	}
	for _, j := range c.g.joins {
		for _, f := range j.from {
			fmt.Fprintf(&b, "\t%s --> %s;\n", f, name(j.to))
		}
	}
	bf := make([]string, 0, len(c.g.branches))
	for f := range c.g.branches {
		bf = append(bf, f)
	}
	sort.Strings(bf)
	for _, f := range bf {
		fmt.Fprintf(&b, "\t%s -.-> __conditional__;\n", name(f))
	}
	return b.String()
}
