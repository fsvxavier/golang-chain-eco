// Package prebuilt holds ready-made graph components (langgraph.prebuilt):
// ToolNode and the ReAct agent.
package prebuilt

import (
	"context"
	"fmt"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type stateKey struct{}

// StateOf returns the graph state visible to the running tool
// (InjectedState).
func StateOf(ctx context.Context) graph.State {
	s, _ := ctx.Value(stateKey{}).(graph.State)
	return s
}

// ErrorHandler turns a tool error into the text shown to the model; return
// ok=false to propagate the error and abort the run.
type ErrorHandler func(call messages.ToolCall, err error) (text string, ok bool)

// DefaultErrorHandler reports every error to the model.
func DefaultErrorHandler(call messages.ToolCall, err error) (string, bool) {
	return fmt.Sprintf("Error: %v\n Please fix your mistakes.", err), true
}

// ToolCaller executes one tool call; ToolNode middleware wraps it.
type ToolCaller func(ctx context.Context, call messages.ToolCall) (messages.Message, error)

// ToolNode executes the tool calls of the last AI message in parallel.
type ToolNode struct {
	Tools []tools.Tool
	// OnError handles tool failures (default DefaultErrorHandler).
	OnError ErrorHandler
	// Wrap, if set, intercepts every call (wrap_tool_call).
	Wrap func(ctx context.Context, call messages.ToolCall, next ToolCaller) (messages.Message, error)
	// MessagesKey is the state key holding messages (default "messages").
	MessagesKey string
}

// NewToolNode builds a ToolNode.
func NewToolNode(ts ...tools.Tool) *ToolNode { return &ToolNode{Tools: ts} }

// Node returns the graph node function.
func (n *ToolNode) Node() graph.NodeFunc { return n.run }

func (n *ToolNode) key() string {
	if n.MessagesKey != "" {
		return n.MessagesKey
	}
	return "messages"
}

func (n *ToolNode) run(ctx context.Context, st graph.State) (any, error) {
	msgs, err := messages.Coerce(st[n.key()])
	if err != nil {
		return nil, err
	}
	last, ok := messages.LastOfRole(msgs, messages.RoleAI)
	if !ok || len(last.ToolCalls) == 0 {
		return nil, nil
	}
	ctx = context.WithValue(ctx, stateKey{}, st)
	// skip calls already answered (e.g. rejected by a reviewer)
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == messages.RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	var calls []messages.ToolCall
	for _, c := range last.ToolCalls {
		if !answered[c.ID] {
			calls = append(calls, c)
		}
	}
	if len(calls) == 0 {
		return nil, nil
	}
	outs := make([]messages.Message, len(calls))
	cmds := make([]*graph.Command, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for i, c := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], cmds[i], errs[i] = n.exec(ctx, c)
		}()
	}
	wg.Wait()
	res := make([]any, 0, len(calls)+1)
	plain := make([]messages.Message, 0, len(calls))
	for i := range calls {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if cmds[i] != nil {
			res = append(res, *cmds[i])
		}
		if outs[i].Role != "" {
			plain = append(plain, outs[i])
		}
	}
	res = append(res, graph.State{n.key(): plain})
	return res, nil
}

func (n *ToolNode) exec(ctx context.Context, c messages.ToolCall) (messages.Message, *graph.Command, error) {
	var cmd *graph.Command
	base := func(ctx context.Context, c messages.ToolCall) (messages.Message, error) {
		t, ok := tools.Find(n.Tools, c.Name)
		if !ok {
			return errMsg(c, fmt.Sprintf("Error: %s is not a valid tool, try one of [%s].", c.Name, names(n.Tools))), nil
		}
		out, err := t.Call(tools.WithCallID(ctx, c.ID), c.Args)
		if err != nil {
			h := n.OnError
			if h == nil {
				h = DefaultErrorHandler
			}
			if text, handled := h(c, err); handled {
				return errMsg(c, text), nil
			}
			return messages.Message{}, err
		}
		switch v := out.(type) {
		case graph.Command:
			cmd = &v
			return messages.Message{}, nil
		case *graph.Command:
			cmd = v
			return messages.Message{}, nil
		case tools.ContentAndArtifact:
			m := messages.Tool(tools.Stringify(v.Content), c.ID)
			m.Name, m.Artifact = c.Name, v.Artifact
			return m, nil
		}
		m := messages.Tool(tools.Stringify(out), c.ID)
		m.Name = c.Name
		return m, nil
	}
	call := base
	if n.Wrap != nil {
		call = func(ctx context.Context, c messages.ToolCall) (messages.Message, error) {
			return n.Wrap(ctx, c, base)
		}
	}
	m, err := call(ctx, c)
	return m, cmd, err
}

func errMsg(c messages.ToolCall, text string) messages.Message {
	m := messages.Tool(text, c.ID)
	m.Name, m.Status = c.Name, "error"
	return m
}

func names(ts []tools.Tool) string {
	s := ""
	for i, t := range ts {
		if i > 0 {
			s += ", "
		}
		s += t.Name()
	}
	return s
}

// ToolsCondition routes to "tools" when the last AI message has tool calls,
// otherwise to graph.End (tools_condition).
func ToolsCondition(_ context.Context, st graph.State) (any, error) {
	msgs, _ := messages.Coerce(st["messages"])
	if last, ok := messages.Last(msgs); ok && len(last.ToolCalls) > 0 {
		return "tools", nil
	}
	return graph.End, nil
}
