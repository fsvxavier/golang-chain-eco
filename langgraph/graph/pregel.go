package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

const defaultRecursionLimit = 25

// Pseudo channels used to persist task control-flow inside Write lists.
const (
	gotoWrite = "__goto__"
	sendWrite = "__send__"
)

// GraphRecursionError is returned when the step limit is exceeded.
type GraphRecursionError struct{ Limit int }

func (e *GraphRecursionError) Error() string {
	return fmt.Sprintf("graph: recursion limit of %d reached without hitting a stop condition", e.Limit)
}

// InterruptInfo describes a paused node.
type InterruptInfo struct {
	ID    string
	Node  string
	Value any
}

// Interrupts extracts the pending interrupts from a run result.
func Interrupts(s State) []InterruptInfo {
	is, _ := s[InterruptKey].([]InterruptInfo)
	return is
}

// StreamMode selects which events StreamEvents emits.
type StreamMode string

const (
	// ModeValues emits the full state after every super-step.
	ModeValues StreamMode = "values"
	// ModeUpdates emits {node: update} per finished node.
	ModeUpdates StreamMode = "updates"
	// ModeCustom emits whatever nodes pass to StreamWriter.
	ModeCustom StreamMode = "custom"
	// ModeMessages emits LLM token chunks (messages.Message) as they arrive.
	ModeMessages StreamMode = "messages"
)

// Event is one streamed item.
type Event struct {
	Mode StreamMode
	Node string // set for updates / messages / custom
	Data any
}

// Compiled is an executable graph.
type Compiled struct {
	g      *StateGraph
	name   string
	specs  map[string]channels.Spec
	saver  checkpoint.Saver
	store  store.Store
	cache  Cache
	before map[string]bool
	after  map[string]bool
}

var _ runnables.Runnable[any, State] = (*Compiled)(nil)

// ---------------------------------------------------------------- contexts

type taskCtx struct {
	mu      sync.Mutex
	resumes []any
	idx     int
	node    string
	emit    func(Event)
	writer  bool
}

type tcKey struct{}

type interruptPanic struct{ value any }

// Interrupt pauses the run and surfaces value to the caller. When the
// run is resumed with Command{Resume: v} the same call returns v (the node
// re-executes from its start, so keep code before it idempotent).
func Interrupt(ctx context.Context, value any) any {
	tc, _ := ctx.Value(tcKey{}).(*taskCtx)
	if tc == nil {
		panic("graph: Interrupt called outside a graph node")
	}
	tc.mu.Lock()
	if tc.idx < len(tc.resumes) {
		v := tc.resumes[tc.idx]
		tc.idx++
		tc.mu.Unlock()
		return v
	}
	tc.mu.Unlock()
	panic(&interruptPanic{value})
}

// StreamWriter returns a function emitting ModeCustom events (no-op unless
// the run is streamed with ModeCustom).
func StreamWriter(ctx context.Context) func(any) {
	tc, _ := ctx.Value(tcKey{}).(*taskCtx)
	if tc == nil || tc.emit == nil || !tc.writer {
		return func(any) {}
	}
	return func(v any) { tc.emit(Event{Mode: ModeCustom, Node: tc.node, Data: v}) }
}

type storeKey struct{}

// StoreFrom returns the long-term store attached with WithStore.
func StoreFrom(ctx context.Context) store.Store {
	s, _ := ctx.Value(storeKey{}).(store.Store)
	return s
}

// ---------------------------------------------------------------- run state

type run struct {
	c       *Compiled
	chans   map[string]channels.Channel
	thread  string
	step    int
	tasks   []checkpoint.Task
	parent  string
	emit    func(Event)
	want    map[StreamMode]bool
	nested  *taskCtx
	pending []InterruptInfo
}

func (c *Compiled) fresh() map[string]channels.Channel {
	m := make(map[string]channels.Channel, len(c.specs))
	for k, s := range c.specs {
		m[k] = s()
	}
	return m
}

func (r *run) state() State {
	s := State{}
	for k, ch := range r.chans {
		if v, ok := ch.Get(); ok && !isInternal(k) {
			s[k] = v
		}
	}
	return s
}

func isInternal(k string) bool { return len(k) > 5 && k[:5] == "join:" }

func (r *run) snapshot() map[string]any {
	m := map[string]any{}
	for k, ch := range r.chans {
		if v, ok := ch.Checkpoint(); ok {
			m[k] = v
		}
	}
	return m
}

func (r *run) restore(cp checkpoint.Checkpoint) {
	for k, ch := range r.chans {
		v, ok := cp.Channels[k]
		r.chans[k] = ch.Restore(v, ok)
	}
	r.tasks = slices.Clone(cp.Next)
	r.step = cp.Step
	r.parent = cp.ID
}

// applyWrites groups writes per channel and updates every channel (channels
// without writes get Update(nil) so ephemerals clear).
func (r *run) applyWrites(ws []checkpoint.Write) error {
	by := map[string][]any{}
	for _, w := range ws {
		if _, ok := r.chans[w.Channel]; !ok {
			return fmt.Errorf("graph: write to unknown state key %q", w.Channel)
		}
		by[w.Channel] = append(by[w.Channel], w.Value)
	}
	names := make([]string, 0, len(r.chans))
	for k := range r.chans {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, err := r.chans[k].Update(by[k]); err != nil {
			return fmt.Errorf("graph: state key %q: %w", k, err)
		}
	}
	return nil
}

func (r *run) save(ctx context.Context, source string) error {
	if r.c.saver == nil || r.thread == "" {
		return nil
	}
	id, err := r.c.saver.Put(ctx, checkpoint.Checkpoint{
		ThreadID: r.thread, ParentID: r.parent, Step: r.step, Source: source,
		Channels: r.snapshot(), Next: slices.Clone(r.tasks),
	})
	if err != nil {
		return err
	}
	r.parent = id
	return nil
}

func (r *run) emitValues() {
	if r.emit != nil && r.want[ModeValues] {
		r.emit(Event{Mode: ModeValues, Data: r.state()})
	}
}

// ---------------------------------------------------------------- public API

// Invoke runs the graph to completion (or until an interrupt) and returns
// the final state. When paused, the state carries InterruptKey.
func (c *Compiled) Invoke(ctx context.Context, input any) (State, error) {
	return c.run(ctx, input, nil, nil)
}

// Stream implements runnables.Streamer, yielding the state after each step.
func (c *Compiled) Stream(ctx context.Context, input any) iter.Seq2[State, error] {
	return func(yield func(State, error) bool) {
		for ev, err := range c.StreamEvents(ctx, input, ModeValues) {
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(ev.Data.(State), nil) {
				return
			}
		}
	}
}

// StreamEvents runs the graph and yields events for the requested modes
// (default: values).
func (c *Compiled) StreamEvents(ctx context.Context, input any, modes ...StreamMode) iter.Seq2[Event, error] {
	if len(modes) == 0 {
		modes = []StreamMode{ModeValues}
	}
	return func(yield func(Event, error) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		type item struct {
			ev  Event
			err error
		}
		ch := make(chan item, 32)
		go func() {
			defer close(ch)
			_, err := c.run(ctx, input, modes, func(e Event) {
				select {
				case ch <- item{ev: e}:
				case <-ctx.Done():
				}
			})
			if err != nil {
				select {
				case ch <- item{err: err}:
				case <-ctx.Done():
				}
			}
		}()
		for it := range ch {
			if !yield(it.ev, it.err) {
				cancel()
				for range ch {
				}
				return
			}
		}
	}
}

func threadOf(ctx context.Context) (thread, cpID string) {
	cfg := runnables.GetConfig(ctx)
	thread, _ = cfg.Configurable["thread_id"].(string)
	cpID, _ = cfg.Configurable["checkpoint_id"].(string)
	return
}

func (c *Compiled) run(ctx context.Context, input any, modes []StreamMode, emit func(Event)) (State, error) {
	r := &run{c: c, chans: c.fresh(), emit: emit, want: map[StreamMode]bool{}}
	for _, m := range modes {
		r.want[m] = true
	}
	r.nested, _ = ctx.Value(tcKey{}).(*taskCtx)
	var cpID string
	r.thread, cpID = threadOf(ctx)
	if c.saver != nil && r.thread == "" {
		return nil, errors.New("graph: a checkpointer needs Configurable[\"thread_id\"] (use graph.Thread)")
	}
	if c.store != nil {
		ctx = context.WithValue(ctx, storeKey{}, c.store)
	}
	if emit != nil && r.want[ModeMessages] {
		ctx = callbacks.WithHandlers(ctx, callbacks.HandlerFunc(func(hctx context.Context, e callbacks.Event) {
			if e.Kind == callbacks.ModelNewToken {
				node := ""
				if tc, _ := hctx.Value(tcKey{}).(*taskCtx); tc != nil {
					node = tc.node
				}
				emit(Event{Mode: ModeMessages, Node: node, Data: e.Chunk})
			}
		}))
	}
	ctx, end := callbacks.Start(ctx, callbacks.ChainStart, c.name, input)

	out, err := r.execute(ctx, input, cpID)
	end(out, err)
	return out, err
}

func (r *run) execute(ctx context.Context, input any, cpID string) (State, error) {
	c := r.c
	resuming := false
	var prev *checkpoint.Checkpoint
	if c.saver != nil {
		cp, ok, err := c.saver.Get(ctx, checkpoint.Config{ThreadID: r.thread, CheckpointID: cpID})
		if err != nil {
			return nil, err
		}
		if cpID != "" && !ok {
			return nil, checkpoint.ErrNotFound
		}
		if ok {
			r.restore(cp)
			prev = &cp
		}
	}

	switch in := input.(type) {
	case nil:
		if len(r.tasks) == 0 {
			return nil, errors.New("graph: no input and nothing to resume")
		}
		resuming = true
	case Command, *Command:
		cmd := asCommand(in)
		if len(r.tasks) == 0 && (cmd.Resume != nil || cmd.ResumeMap != nil) {
			return nil, errors.New("graph: nothing to resume")
		}
		resuming = true
		for i := range r.tasks {
			t := &r.tasks[i]
			if !t.Interrupted {
				continue
			}
			id := interruptID(*t, i)
			if v, ok := cmd.ResumeMap[id]; ok {
				t.Resumes = append(t.Resumes, v)
				t.Interrupted = false
			} else if cmd.Resume != nil {
				t.Resumes = append(t.Resumes, cmd.Resume)
				t.Interrupted = false
			}
		}
		if len(cmd.Update) > 0 {
			ws, err := updateWrites(cmd.Update)
			if err != nil {
				return nil, err
			}
			if err := r.applyWrites(ws); err != nil {
				return nil, err
			}
		}
		if len(cmd.Goto)+len(cmd.Send) > 0 {
			r.tasks = nil
			for _, g := range cmd.Goto {
				r.tasks = append(r.tasks, checkpoint.Task{Node: g})
			}
			for _, s := range cmd.Send {
				r.tasks = append(r.tasks, checkpoint.Task{Node: s.Node, Arg: s.Arg, HasArg: true})
			}
			resuming = false
		}
	default:
		st, ok := input.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("graph: input must be a map[string]any, got %T", input)
		}
		ws, err := updateWrites(st)
		if err != nil {
			return nil, err
		}
		if err := r.applyWrites(ws); err != nil {
			return nil, err
		}
		next, err := r.successors(ctx, Start, nil, nil)
		if err != nil {
			return nil, err
		}
		r.tasks = next
		if prev != nil {
			r.step++
		}
		if err := r.save(ctx, "input"); err != nil {
			return nil, err
		}
	}
	r.emitValues()

	limit := runnables.GetConfig(ctx).RecursionLimit
	if limit <= 0 {
		limit = defaultRecursionLimit
	}
	for steps := 0; len(r.tasks) > 0; steps++ {
		if steps >= limit {
			return nil, &GraphRecursionError{Limit: limit}
		}
		if !(resuming && steps == 0) && r.hits(c.before, r.tasks) {
			return r.paused(ctx, nil)
		}
		var held []checkpoint.Task
		r.tasks, held = r.splitDeferred()
		results, err := r.runTasks(ctx)
		if err != nil {
			return nil, err
		}
		var ints []InterruptInfo
		for i, t := range r.tasks {
			res := results[i]
			if res.interrupted {
				r.tasks[i].Interrupted, r.tasks[i].InterruptValue = true, res.ivalue
				ints = append(ints, InterruptInfo{ID: interruptID(r.tasks[i], i), Node: t.Node, Value: res.ivalue})
			} else if !t.Done {
				r.tasks[i].Done, r.tasks[i].Writes = true, res.writes
			}
		}
		if len(ints) > 0 {
			if r.nested != nil && c.saver == nil {
				panic(&interruptPanic{ints[0].Value})
			}
			r.tasks = append(r.tasks, held...)
			if err := r.save(ctx, "loop"); err != nil {
				return nil, err
			}
			return r.paused(ctx, ints)
		}
		// all tasks done: apply writes in task order
		var ws []checkpoint.Write
		for _, t := range r.tasks {
			ws = append(ws, plain(t.Writes)...)
		}
		if err := r.applyWrites(ws); err != nil {
			return nil, err
		}
		if r.emit != nil && r.want[ModeUpdates] {
			for _, t := range r.tasks {
				upd := State{}
				for _, w := range plain(t.Writes) {
					upd[w.Channel] = w.Value
				}
				r.emit(Event{Mode: ModeUpdates, Node: t.Node, Data: map[string]any{t.Node: upd}})
			}
		}
		next, err := r.nextTasks(ctx, r.tasks)
		if err != nil {
			return nil, err
		}
		ran := r.tasks
		r.tasks = dedupe(append(next, held...))
		r.step++
		if err := r.save(ctx, "loop"); err != nil {
			return nil, err
		}
		r.emitValues()
		if r.hitsRan(c.after, ran) && len(next) > 0 {
			return r.paused(ctx, nil)
		}
	}
	return r.state(), nil
}

// splitDeferred separates tasks of Deferred nodes while other tasks remain.
func (r *run) splitDeferred() (now, held []checkpoint.Task) {
	for _, t := range r.tasks {
		if n := r.c.g.nodes[t.Node]; n != nil && n.deferred && !t.Done {
			held = append(held, t)
		} else {
			now = append(now, t)
		}
	}
	if len(now) == 0 {
		return held, nil
	}
	return now, held
}

func plain(ws []checkpoint.Write) []checkpoint.Write {
	out := make([]checkpoint.Write, 0, len(ws))
	for _, w := range ws {
		if w.Channel != gotoWrite && w.Channel != sendWrite {
			out = append(out, w)
		}
	}
	return out
}

func asCommand(v any) Command {
	if p, ok := v.(*Command); ok {
		return *p
	}
	return v.(Command)
}

func interruptID(t checkpoint.Task, idx int) string { return fmt.Sprintf("%s:%d", t.Node, idx) }

func (r *run) hits(set map[string]bool, tasks []checkpoint.Task) bool {
	for _, t := range tasks {
		if !t.Done && (set["*"] || set[t.Node]) {
			return true
		}
	}
	return false
}

func (r *run) hitsRan(set map[string]bool, tasks []checkpoint.Task) bool { return r.hits2(set, tasks) }

func (r *run) hits2(set map[string]bool, tasks []checkpoint.Task) bool {
	for _, t := range tasks {
		if set["*"] || set[t.Node] {
			return true
		}
	}
	return false
}

// paused returns the state with interrupt info after persisting.
func (r *run) paused(ctx context.Context, ints []InterruptInfo) (State, error) {
	if ints == nil {
		if err := r.save(ctx, "loop"); err != nil {
			return nil, err
		}
	}
	s := r.state()
	if ints == nil {
		for _, t := range r.tasks {
			ints = append(ints, InterruptInfo{Node: t.Node})
		}
	}
	s[InterruptKey] = ints
	return s, nil
}

func updateWrites(u State) ([]checkpoint.Write, error) {
	keys := make([]string, 0, len(u))
	for k := range u {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ws := make([]checkpoint.Write, len(keys))
	for i, k := range keys {
		ws[i] = checkpoint.Write{Channel: k, Value: u[k]}
	}
	return ws, nil
}

// ---------------------------------------------------------------- tasks

type taskResult struct {
	writes      []checkpoint.Write
	interrupted bool
	ivalue      any
	err         error
}

func (r *run) runTasks(ctx context.Context) ([]taskResult, error) {
	res := make([]taskResult, len(r.tasks))
	view := r.state()
	cfg := runnables.GetConfig(ctx)
	var sem chan struct{}
	if cfg.MaxConcurrency > 0 {
		sem = make(chan struct{}, cfg.MaxConcurrency)
	}
	var wg sync.WaitGroup
	for i := range r.tasks {
		t := r.tasks[i]
		if t.Done {
			res[i] = taskResult{writes: t.Writes}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			res[i] = r.runTask(ctx, t, view)
		}()
	}
	wg.Wait()
	for _, x := range res {
		if x.err != nil {
			return nil, x.err
		}
	}
	return res, nil
}

func (r *run) runTask(ctx context.Context, t checkpoint.Task, view State) (out taskResult) {
	n := r.c.g.nodes[t.Node]
	if n == nil {
		return taskResult{err: fmt.Errorf("graph: unknown node %q", t.Node)}
	}
	in := view
	if t.HasArg {
		a, ok := t.Arg.(map[string]any)
		if !ok {
			return taskResult{err: fmt.Errorf("graph: Send to %q needs a State argument, got %T", t.Node, t.Arg)}
		}
		in = a
	}
	var ckey string
	if n.cache != nil && r.c.cache != nil {
		ckey = t.Node + ":" + cacheKey(n.cache, in)
		if ws, ok := r.c.cache.Get(ckey); ok {
			return taskResult{writes: ws}
		}
	}
	tc := &taskCtx{resumes: t.Resumes, node: t.Node, emit: r.emit, writer: r.want[ModeCustom]}
	if r.nested != nil {
		tc = r.nested // share resume cursor with the enclosing node
	}
	ctx = context.WithValue(ctx, tcKey{}, tc)
	ctx, end := callbacks.Start(ctx, callbacks.ChainStart, t.Node, in)
	var ret any
	attempt := func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				ip, ok := p.(*interruptPanic)
				if !ok {
					panic(p)
				}
				out.interrupted, out.ivalue = true, ip.value
				err = nil
			}
		}()
		if r.nested == nil {
			tc.mu.Lock()
			tc.idx = 0
			tc.mu.Unlock()
		}
		ret, err = n.fn(ctx, maps.Clone(in))
		return err
	}
	var err error
	if n.retry != nil {
		err = runnables.Backoff(ctx, *n.retry, func() error {
			if e := attempt(); e != nil || out.interrupted {
				return e
			}
			return nil
		})
	} else {
		err = attempt()
	}
	end(ret, err)
	if err != nil {
		return taskResult{err: fmt.Errorf("node %q: %w", t.Node, err)}
	}
	if out.interrupted {
		return out
	}
	ws, err := toWrites(ret)
	if err != nil {
		return taskResult{err: fmt.Errorf("node %q: %w", t.Node, err)}
	}
	if ckey != "" {
		r.c.cache.Set(ckey, ws, n.cache.TTL)
	}
	return taskResult{writes: ws}
}

func cacheKey(p *CachePolicy, in State) string {
	if p.Key != nil {
		return p.Key(in)
	}
	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// toWrites normalises a node's return value into writes plus control flow.
func toWrites(ret any) ([]checkpoint.Write, error) {
	switch v := ret.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return updateWrites(v)
	case Command, *Command:
		cmd := asCommand(v)
		ws, _ := updateWrites(cmd.Update)
		for _, g := range cmd.Goto {
			ws = append(ws, checkpoint.Write{Channel: gotoWrite, Value: g})
		}
		for _, s := range cmd.Send {
			ws = append(ws, checkpoint.Write{Channel: sendWrite, Value: s})
		}
		return ws, nil
	case []any:
		var all []checkpoint.Write
		for _, x := range v {
			ws, err := toWrites(x)
			if err != nil {
				return nil, err
			}
			all = append(all, ws...)
		}
		return all, nil
	}
	return nil, fmt.Errorf("node returned %T; want nil, State or Command", ret)
}

// ---------------------------------------------------------------- routing

func (r *run) nextTasks(ctx context.Context, ran []checkpoint.Task) ([]checkpoint.Task, error) {
	var gotos []string
	var sends []Send
	for _, t := range ran {
		for _, w := range t.Writes {
			switch w.Channel {
			case gotoWrite:
				gotos = append(gotos, w.Value.(string))
			case sendWrite:
				sends = append(sends, w.Value.(Send))
			}
		}
	}
	names := make([]string, 0, len(ran))
	seen := map[string]bool{}
	for _, t := range ran {
		if !seen[t.Node] {
			seen[t.Node] = true
			names = append(names, t.Node)
		}
	}
	var out []checkpoint.Task
	for _, n := range names {
		ts, err := r.successors(ctx, n, nil, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, ts...)
	}
	for _, g := range gotos {
		if g != End {
			if _, ok := r.c.g.nodes[g]; !ok {
				return nil, fmt.Errorf("graph: Command goto unknown node %q", g)
			}
			out = append(out, checkpoint.Task{Node: g})
		}
	}
	for _, s := range sends {
		if _, ok := r.c.g.nodes[s.Node]; !ok {
			return nil, fmt.Errorf("graph: Send to unknown node %q", s.Node)
		}
		out = append(out, checkpoint.Task{Node: s.Node, Arg: s.Arg, HasArg: true})
	}
	return dedupe(out), nil
}

func dedupe(ts []checkpoint.Task) []checkpoint.Task {
	seen := map[string]bool{}
	out := ts[:0:0]
	for _, t := range ts {
		if !t.HasArg {
			if seen[t.Node] {
				continue
			}
			seen[t.Node] = true
		}
		out = append(out, t)
	}
	return out
}

// successors computes the tasks following `from` via static edges, joins and
// conditional routers.
func (r *run) successors(ctx context.Context, from string, _, _ any) ([]checkpoint.Task, error) {
	g := r.c.g
	var out []checkpoint.Task
	for _, to := range g.edges[from] {
		if to != End {
			out = append(out, checkpoint.Task{Node: to})
		}
	}
	for _, j := range g.joins {
		if !slices.Contains(j.from, from) {
			continue
		}
		b := r.chans[j.ch]
		if _, err := b.Update([]any{from}); err != nil {
			return nil, err
		}
		if _, ok := b.Get(); ok {
			b.Consume()
			if j.to != End {
				out = append(out, checkpoint.Task{Node: j.to})
			}
		}
	}
	if rs := g.branches[from]; len(rs) > 0 {
		st := r.state()
		for _, router := range rs {
			v, err := router(ctx, st)
			if err != nil {
				return nil, fmt.Errorf("router after %q: %w", from, err)
			}
			ts, err := r.routed(v)
			if err != nil {
				return nil, fmt.Errorf("router after %q: %w", from, err)
			}
			out = append(out, ts...)
		}
	}
	return dedupe(out), nil
}

func (r *run) routed(v any) ([]checkpoint.Task, error) {
	var out []checkpoint.Task
	add := func(name string) error {
		if name == End {
			return nil
		}
		if _, ok := r.c.g.nodes[name]; !ok {
			return fmt.Errorf("unknown node %q", name)
		}
		out = append(out, checkpoint.Task{Node: name})
		return nil
	}
	switch x := v.(type) {
	case nil:
	case string:
		return out, add(x)
	case []string:
		for _, n := range x {
			if err := add(n); err != nil {
				return nil, err
			}
		}
	case Send:
		out = append(out, checkpoint.Task{Node: x.Node, Arg: x.Arg, HasArg: true})
	case []Send:
		for _, s := range x {
			out = append(out, checkpoint.Task{Node: s.Node, Arg: s.Arg, HasArg: true})
		}
	default:
		return nil, fmt.Errorf("router returned %T", v)
	}
	return out, nil
}

// ---------------------------------------------------------------- state API

// StateSnapshot is a thread's state at a checkpoint.
type StateSnapshot struct {
	Values       State
	Next         []string
	CheckpointID string
	ParentID     string
	Step         int
	Interrupts   []InterruptInfo
}

func (c *Compiled) snapshotOf(cp checkpoint.Checkpoint) StateSnapshot {
	r := &run{c: c, chans: c.fresh()}
	r.restore(cp)
	s := StateSnapshot{Values: r.state(), CheckpointID: cp.ID, ParentID: cp.ParentID, Step: cp.Step}
	for i, t := range cp.Next {
		if !t.Done {
			s.Next = append(s.Next, t.Node)
		}
		if t.Interrupted {
			s.Interrupts = append(s.Interrupts, InterruptInfo{ID: interruptID(t, i), Node: t.Node, Value: t.InterruptValue})
		}
	}
	return s
}

// GetState returns the latest (or Configurable["checkpoint_id"]) snapshot of
// the configured thread.
func (c *Compiled) GetState(ctx context.Context) (StateSnapshot, error) {
	if c.saver == nil {
		return StateSnapshot{}, errors.New("graph: no checkpointer")
	}
	thread, id := threadOf(ctx)
	cp, ok, err := c.saver.Get(ctx, checkpoint.Config{ThreadID: thread, CheckpointID: id})
	if err != nil {
		return StateSnapshot{}, err
	}
	if !ok {
		return StateSnapshot{Values: State{}}, nil
	}
	return c.snapshotOf(cp), nil
}

// GetStateHistory lists snapshots of the thread, newest first.
func (c *Compiled) GetStateHistory(ctx context.Context, limit int) ([]StateSnapshot, error) {
	if c.saver == nil {
		return nil, errors.New("graph: no checkpointer")
	}
	thread, _ := threadOf(ctx)
	cps, err := c.saver.List(ctx, thread, limit)
	if err != nil {
		return nil, err
	}
	out := make([]StateSnapshot, len(cps))
	for i, cp := range cps {
		out[i] = c.snapshotOf(cp)
	}
	return out, nil
}

// UpdateState writes values into the thread as if node asNode produced them
// (applying reducers) and stores a new checkpoint. With asNode set, the next
// tasks are that node's successors; otherwise pending tasks are kept. Using
// Configurable["checkpoint_id"] forks from an earlier checkpoint.
func (c *Compiled) UpdateState(ctx context.Context, values State, asNode string) (string, error) {
	if c.saver == nil {
		return "", errors.New("graph: no checkpointer")
	}
	thread, id := threadOf(ctx)
	cp, ok, err := c.saver.Get(ctx, checkpoint.Config{ThreadID: thread, CheckpointID: id})
	if err != nil {
		return "", err
	}
	r := &run{c: c, chans: c.fresh(), thread: thread}
	if ok {
		r.restore(cp)
		r.step++
	}
	ws, _ := updateWrites(values)
	if err := r.applyWrites(ws); err != nil {
		return "", err
	}
	if asNode != "" {
		if _, ok := c.g.nodes[asNode]; !ok && asNode != Start {
			return "", fmt.Errorf("graph: unknown node %q", asNode)
		}
		next, err := r.successors(ctx, asNode, nil, nil)
		if err != nil {
			return "", err
		}
		r.tasks = next
	} else {
		for i := range r.tasks {
			r.tasks[i].Interrupted = false
		}
	}
	if err := r.save(ctx, "update"); err != nil {
		return "", err
	}
	return r.parent, nil
}

// RunInterruptible runs fn so that Interrupt works inside it, feeding the
// calls with resumes in order. It reports an interrupt instead of panicking.
// It is the building block for the functional API.
func RunInterruptible(ctx context.Context, resumes []any, fn func(context.Context) error) (value any, interrupted bool, err error) {
	tc := &taskCtx{resumes: resumes}
	defer func() {
		if p := recover(); p != nil {
			ip, ok := p.(*interruptPanic)
			if !ok {
				panic(p)
			}
			value, interrupted, err = ip.value, true, nil
		}
	}()
	err = fn(context.WithValue(ctx, tcKey{}, tc))
	return
}

func init() { checkpoint.Register("graph.Send", Send{}) }
