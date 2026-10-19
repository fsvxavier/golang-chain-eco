package agentprotocol

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

var rootNS = []string{}

// translator turns graph runtime events into streaming-protocol events on a
// thread hub: the messages channel with explicit start/delta/finish
// boundaries, tools lifecycle, updates, values, checkpoints, custom and the
// lifecycle status machine. Every event keeps the namespace of the (sub)graph
// that emitted it, and each namespace streams its own message at a time.
type translator struct {
	h     *hub
	graph string

	mu       sync.Mutex
	open     map[string]*openMessage // namespace key -> message being streamed
	mem      *streamMemory           // shared with the thread's other runs
	toolRuns map[string]string       // callback run id -> tool call id
	anon     int
}

// nsKey identifies a namespace in maps.
func nsKey(ns []string) string { return strings.Join(ns, "|") }

type openMessage struct {
	ns        []string
	id        string
	node      string
	text      string
	textOpen  bool
	nextIndex int
	calls     []messages.ToolCall
	usage     *messages.Usage
	model     string
}

// streamMemory is what a thread remembers between its runs: which messages
// the events stream already announced (so a resumed run does not replay them)
// and which tool calls are waiting to start (so a tool that runs after a
// human approval still reports the id of the call that was streamed earlier).
type streamMemory struct {
	streamed map[string]bool
	pending  []messages.ToolCall
}

func newStreamMemory() *streamMemory { return &streamMemory{streamed: map[string]bool{}} }

// newTranslator builds a translator over a thread's memory.
func newTranslator(h *hub, graphName string, mem *streamMemory) *translator {
	return &translator{h: h, graph: graphName, mem: mem, open: map[string]*openMessage{}, toolRuns: map[string]string{}}
}

func (t *translator) lifecycle(status string, extra map[string]any) {
	data := map[string]any{"event": status, "graphName": t.graph}
	for k, v := range extra {
		data[k] = v
	}
	t.h.publish("lifecycle", rootNS, "", data)
}

// handle consumes one graph event.
func (t *translator) handle(ev graph.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ns := ev.Namespace
	switch ev.Mode {
	case graph.ModeMessages:
		if m, ok := ev.Data.(messages.Message); ok {
			t.chunk(ns, ev.Node, m)
		}
	case graph.ModeUpdates:
		t.closeMessage(ns)
		if upd := unwrapUpdate(ev); upd != nil {
			t.updates(ns, ev.Node, upd)
		}
	case graph.ModeValues:
		if st, ok := ev.Data.(graph.State); ok {
			t.h.publish("values", ns, "", jsonState(st))
		}
	case graph.ModeCustom:
		t.h.publish("custom", ns, ev.Node, map[string]any{"payload": ev.Data})
	}
}

func (t *translator) updates(ns []string, node string, upd map[string]any) {
	vals := map[string]any{}
	for k, v := range upd {
		vals[k] = v
	}
	data := map[string]any{"values": jsonState(vals)}
	if node != "" {
		data["node"] = node
	}
	t.h.publish("updates", ns, node, data)
	// models that do not stream still deserve message events
	if v, ok := upd["messages"]; ok {
		ms, err := messages.Coerce(v)
		if err != nil {
			return
		}
		for _, m := range ms {
			if m.Role == messages.RoleAI && m.ID != "" && !t.mem.streamed[m.ID] {
				t.mem.streamed[m.ID] = true
				t.emitWhole(ns, node, m)
			}
		}
	}
}

func (t *translator) emitWhole(ns []string, node string, m messages.Message) {
	t.h.publish("messages", ns, node, map[string]any{"event": "message-start", "role": "ai", "id": m.ID})
	idx := 0
	if text := m.Text(); text != "" {
		blk := map[string]any{"type": "text", "text": text}
		t.h.publish("messages", ns, node, map[string]any{"event": "content-block-start", "index": idx, "content": map[string]any{"type": "text", "text": ""}})
		t.h.publish("messages", ns, node, map[string]any{"event": "content-block-delta", "index": idx, "delta": map[string]any{"type": "text-delta", "text": text}})
		t.h.publish("messages", ns, node, map[string]any{"event": "content-block-finish", "index": idx, "content": blk})
		idx++
	}
	for _, tc := range m.ToolCalls {
		t.toolBlock(ns, node, idx, tc)
		t.mem.pending = append(t.mem.pending, tc)
		idx++
	}
	t.h.publish("messages", ns, node, finishData(m.Usage))
}

func finishData(u *messages.Usage) map[string]any {
	d := map[string]any{"event": "message-finish"}
	if u != nil {
		d["usage"] = map[string]any{"inputTokens": u.InputTokens, "outputTokens": u.OutputTokens, "totalTokens": u.TotalTokens}
	}
	return d
}

func (t *translator) toolBlock(ns []string, node string, idx int, tc messages.ToolCall) {
	t.h.publish("messages", ns, node, map[string]any{"event": "content-block-start", "index": idx,
		"content": map[string]any{"type": "tool_call_chunk", "id": tc.ID, "name": tc.Name, "args": ""}})
	t.h.publish("messages", ns, node, map[string]any{"event": "content-block-finish", "index": idx,
		"content": map[string]any{"type": "tool_call", "id": tc.ID, "name": tc.Name, "args": tc.Args}})
}

// chunk handles one streamed model chunk, opening and closing the message and
// its content blocks so clients never have to infer boundaries.
func (t *translator) chunk(ns []string, node string, c messages.Message) {
	k := nsKey(ns)
	cur := t.open[k]
	id := c.ID
	if id == "" && cur != nil && cur.node == node {
		id = cur.id
	}
	if id == "" {
		t.anon++
		id = fmt.Sprintf("msg_%d", t.anon)
	}
	if cur != nil && cur.id != id {
		t.closeMessage(ns)
		cur = nil
	}
	if cur == nil {
		cur = &openMessage{ns: ns, id: id, node: node}
		t.open[k] = cur
		t.mem.streamed[id] = true
		t.h.publish("messages", ns, node, map[string]any{"event": "message-start", "role": "ai", "id": id})
	}
	if c.Content != "" {
		if !cur.textOpen {
			cur.textOpen = true
			t.h.publish("messages", ns, node, map[string]any{"event": "content-block-start", "index": cur.nextIndex, "content": map[string]any{"type": "text", "text": ""}})
		}
		cur.text += c.Content
		t.h.publish("messages", ns, node, map[string]any{"event": "content-block-delta", "index": cur.nextIndex, "delta": map[string]any{"type": "text-delta", "text": c.Content}})
	}
	if len(c.ToolCalls) > 0 {
		cur.calls = append(cur.calls, c.ToolCalls...)
	}
	if c.Usage != nil {
		cur.usage = c.Usage
	}
}

// closeMessage finishes the namespace's open message (text block first, then
// tool-call blocks, then message-finish) and queues its tool calls for
// correlation.
func (t *translator) closeMessage(ns []string) {
	k := nsKey(ns)
	cur := t.open[k]
	if cur == nil {
		return
	}
	delete(t.open, k)
	if cur.textOpen {
		t.h.publish("messages", ns, cur.node, map[string]any{"event": "content-block-finish", "index": cur.nextIndex,
			"content": map[string]any{"type": "text", "text": cur.text}})
		cur.nextIndex++
	}
	for _, tc := range cur.calls {
		t.toolBlock(ns, cur.node, cur.nextIndex, tc)
		t.mem.pending = append(t.mem.pending, tc)
		cur.nextIndex++
	}
	t.h.publish("messages", ns, cur.node, finishData(cur.usage))
}

// finish flushes anything still open at the end of a run.
func (t *translator) finish() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, cur := range t.open {
		t.closeMessage(cur.ns)
	}
}

// callbacksHandler reports tool execution on the tools channel.
func (t *translator) callbacksHandler() callbacks.Handler {
	return callbacks.HandlerFunc(func(hctx context.Context, e callbacks.Event) {
		ns := graph.NamespaceOf(hctx)
		switch e.Kind {
		case callbacks.ToolStart:
			// A tool node runs each call with its call id in ctx: that id is the
			// one the messages channel announced, whatever the event order.
			callID := tools.CallID(hctx)
			t.mu.Lock()
			if callID == "" { // a tool outside a tool node: match the call by name
				for _, tc := range t.mem.pending {
					if tc.Name == e.Name {
						callID = tc.ID
						break
					}
				}
			}
			if callID == "" {
				callID = e.RunID
			}
			t.mem.pending = slices.DeleteFunc(t.mem.pending, func(tc messages.ToolCall) bool { return tc.ID == callID })
			t.toolRuns[e.RunID] = callID
			t.mu.Unlock()
			t.h.publish("tools", ns, "tools", map[string]any{"event": "tool-started", "toolCallId": callID, "toolName": e.Name, "input": e.Input})
		case callbacks.ToolEnd, callbacks.ToolError:
			t.mu.Lock()
			callID := t.toolRuns[e.RunID]
			delete(t.toolRuns, e.RunID)
			t.mu.Unlock()
			if e.Kind == callbacks.ToolError {
				msg := "tool error"
				if e.Err != nil {
					msg = e.Err.Error()
				}
				t.h.publish("tools", ns, "tools", map[string]any{"event": "tool-error", "toolCallId": callID, "message": msg})
				return
			}
			t.h.publish("tools", ns, "tools", map[string]any{"event": "tool-finished", "toolCallId": callID, "output": e.Output})
		}
	})
}

// unwrapUpdate extracts a node's state update from an updates event, whose
// payload is {node: update}.
func unwrapUpdate(ev graph.Event) map[string]any {
	outer, ok := ev.Data.(map[string]any)
	if !ok {
		return nil
	}
	if inner, ok := outer[ev.Node].(map[string]any); ok {
		return inner
	}
	return outer
}
