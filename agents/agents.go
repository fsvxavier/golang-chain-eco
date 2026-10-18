// Package agents is the Go port of langchain's create_agent: a model <->
// tools loop built on LangGraph, extensible with middleware hooks
// (before_agent, before_model, wrap_model_call, after_model, wrap_tool_call,
// after_agent).
package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

// Jump targets for the "jump_to" state key a hook may set.
const (
	JumpEnd   = "end"
	JumpTools = "tools"
	JumpModel = "model"
)

// ModelRequest is what the model node is about to send; WrapModel hooks may
// change any field.
type ModelRequest struct {
	Model    models.ChatModel
	System   string
	Messages []messages.Message
	Tools    []tools.Tool
	Options  []models.Option
	State    graph.State
}

// ModelCaller performs the model call.
type ModelCaller func(ctx context.Context, req *ModelRequest) (messages.Message, error)

// Middleware is the marker interface; implement any of the hook interfaces.
type Middleware interface{ Name() string }

// Hook interfaces. A hook returns nil, a graph.State update, or a
// graph.Command; setting "jump_to" in the update redirects the flow.
type (
	BeforeAgent interface {
		BeforeAgent(ctx context.Context, st graph.State) (any, error)
	}
	BeforeModel interface {
		BeforeModel(ctx context.Context, st graph.State) (any, error)
	}
	AfterModel interface {
		AfterModel(ctx context.Context, st graph.State) (any, error)
	}
	AfterAgent interface {
		AfterAgent(ctx context.Context, st graph.State) (any, error)
	}
	WrapModel interface {
		WrapModelCall(ctx context.Context, req *ModelRequest, next ModelCaller) (messages.Message, error)
	}
	WrapTool interface {
		WrapToolCall(ctx context.Context, call messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error)
	}
	WithTools interface{ Tools() []tools.Tool }
	WithState interface{ StateSchema() graph.Schema }
)

// Options configures Create.
type Options struct {
	Model        models.ChatModel
	Tools        []tools.Tool
	SystemPrompt string
	Middleware   []Middleware
	// ResponseFormat, when set, asks the model for JSON and stores the parsed
	// result in state["structured_response"].
	ResponseFormat *models.ResponseFormat
	// StateSchema adds custom state channels.
	StateSchema graph.Schema
	Compile     []graph.CompileOption
}

// Create builds and compiles the agent graph.
func Create(o Options) (*graph.Compiled, error) {
	if o.Model == nil {
		return nil, fmt.Errorf("agents: Model is required")
	}
	schema := graph.Schema{
		"messages":            graph.Messages(),
		"jump_to":             channels.Ephemeral(false),
		"structured_response": channels.LastValue(),
	}
	for k, v := range o.StateSchema {
		schema[k] = v
	}
	allTools := append([]tools.Tool(nil), o.Tools...)
	for _, m := range o.Middleware {
		if w, ok := m.(WithState); ok {
			for k, v := range w.StateSchema() {
				schema[k] = v
			}
		}
		if w, ok := m.(WithTools); ok {
			allTools = append(allTools, w.Tools()...)
		}
	}
	g := graph.NewStateGraph(schema)

	// tool node with wrap_tool_call chain
	tn := prebuilt.NewToolNode(allTools...)
	var wt []WrapTool
	for _, m := range o.Middleware {
		if w, ok := m.(WrapTool); ok {
			wt = append(wt, w)
		}
	}
	if len(wt) > 0 {
		tn.Wrap = func(ctx context.Context, c messages.ToolCall, base prebuilt.ToolCaller) (messages.Message, error) {
			call := base
			for i := len(wt) - 1; i >= 0; i-- {
				w, next := wt[i], call
				call = func(ctx context.Context, c messages.ToolCall) (messages.Message, error) {
					return w.WrapToolCall(ctx, c, next)
				}
			}
			return call(ctx, c)
		}
	}
	hasTools := len(allTools) > 0
	if hasTools {
		g.AddNode("tools", tn.Node())
	}

	// model node
	var wm []WrapModel
	for _, m := range o.Middleware {
		if w, ok := m.(WrapModel); ok {
			wm = append(wm, w)
		}
	}
	g.AddNode("model", func(ctx context.Context, st graph.State) (any, error) {
		msgs, err := messages.Coerce(st["messages"])
		if err != nil {
			return nil, err
		}
		req := &ModelRequest{Model: o.Model, System: o.SystemPrompt, Messages: msgs, Tools: allTools, State: st}
		if o.ResponseFormat != nil {
			req.Options = append(req.Options, models.WithResponseFormat(*o.ResponseFormat))
		}
		call := func(ctx context.Context, r *ModelRequest) (messages.Message, error) {
			in := r.Messages
			if r.System != "" {
				in = append([]messages.Message{messages.System(r.System)}, in...)
			}
			opts := r.Options
			if len(r.Tools) > 0 {
				opts = append([]models.Option{models.WithTools(r.Tools...)}, opts...)
			}
			return r.Model.Generate(ctx, in, opts...)
		}
		for i := len(wm) - 1; i >= 0; i-- {
			w, next := wm[i], call
			call = func(ctx context.Context, r *ModelRequest) (messages.Message, error) {
				return w.WrapModelCall(ctx, r, next)
			}
		}
		out, err := call(ctx, req)
		if err != nil {
			return nil, err
		}
		if out.ID == "" {
			out.ID = messages.NewID()
		}
		upd := graph.State{"messages": []messages.Message{out}}
		if o.ResponseFormat != nil && len(out.ToolCalls) == 0 {
			var v any
			if err := json.Unmarshal([]byte(models.ExtractJSON(out.Text())), &v); err != nil {
				return nil, fmt.Errorf("structured response: %w", err)
			}
			upd["structured_response"] = v
		}
		return upd, nil
	})

	// hook chains
	type hook struct {
		name string
		fn   graph.NodeFunc
	}
	var bAgent, bModel, aModel, aAgent []hook
	for _, m := range o.Middleware {
		n := m.Name()
		if h, ok := m.(BeforeAgent); ok {
			bAgent = append(bAgent, hook{n + ".before_agent", h.BeforeAgent})
		}
		if h, ok := m.(BeforeModel); ok {
			bModel = append(bModel, hook{n + ".before_model", h.BeforeModel})
		}
		if h, ok := m.(AfterModel); ok {
			aModel = append([]hook{{n + ".after_model", h.AfterModel}}, aModel...)
		}
		if h, ok := m.(AfterAgent); ok {
			aAgent = append([]hook{{n + ".after_agent", h.AfterAgent}}, aAgent...)
		}
	}
	for _, hs := range [][]hook{bAgent, bModel, aModel, aAgent} {
		for _, h := range hs {
			g.AddNode(h.name, h.fn)
		}
	}
	first := func(hs []hook, dflt string) string {
		if len(hs) > 0 {
			return hs[0].name
		}
		return dflt
	}
	loopEntry := first(bModel, "model")
	exit := first(aAgent, graph.End)
	entry := first(bAgent, loopEntry)
	g.AddEdge(graph.Start, entry)

	// jumpRouter sends to jump_to if set, otherwise to dflt.
	jumpTarget := func(j any, dflt string) string {
		switch j {
		case JumpEnd:
			return exit
		case JumpTools:
			if hasTools {
				return "tools"
			}
			return exit
		case JumpModel:
			return loopEntry
		}
		return dflt
	}
	chain := func(hs []hook, after string) {
		for i, h := range hs {
			next := after
			if i+1 < len(hs) {
				next = hs[i+1].name
			}
			g.AddConditionalEdges(h.name, func(_ context.Context, st graph.State) (any, error) {
				return jumpTarget(st["jump_to"], next), nil
			})
		}
	}
	chain(bAgent, loopEntry)
	chain(bModel, "model")
	// after the model: more hooks, then decide
	afterModelFirst := first(aModel, "decide")
	if len(aModel) == 0 {
		g.AddConditionalEdges("model", decide(hasTools, exit))
	} else {
		g.AddEdge("model", afterModelFirst)
		for i, h := range aModel {
			if i+1 < len(aModel) {
				nxt := aModel[i+1].name
				g.AddConditionalEdges(h.name, func(_ context.Context, st graph.State) (any, error) {
					return jumpTarget(st["jump_to"], nxt), nil
				})
			} else {
				d := decide(hasTools, exit)
				g.AddConditionalEdges(h.name, func(ctx context.Context, st graph.State) (any, error) {
					if j := st["jump_to"]; j != nil {
						return jumpTarget(j, exit), nil
					}
					return d(ctx, st)
				})
			}
		}
	}
	if hasTools {
		g.AddConditionalEdges("tools", func(_ context.Context, st graph.State) (any, error) {
			msgs, _ := messages.Coerce(st["messages"])
			direct := true
			n := 0
			for i := len(msgs) - 1; i >= 0 && msgs[i].Role == messages.RoleTool; i-- {
				n++
				t, ok := tools.Find(allTools, msgs[i].Name)
				rd, isRD := t.(tools.ReturnDirect)
				if !ok || !isRD || !rd.ReturnDirect() {
					direct = false
				}
			}
			if n > 0 && direct {
				return exit, nil
			}
			return jumpTarget(st["jump_to"], loopEntry), nil
		})
	}
	for i, h := range aAgent {
		if i+1 < len(aAgent) {
			g.AddEdge(h.name, aAgent[i+1].name)
		} else {
			g.AddEdge(h.name, graph.End)
		}
	}
	return g.Compile(o.Compile...)
}

func decide(hasTools bool, exit string) graph.Router {
	return func(_ context.Context, st graph.State) (any, error) {
		msgs, _ := messages.Coerce(st["messages"])
		if last, ok := messages.Last(msgs); ok && hasTools && len(last.ToolCalls) > 0 && last.Role == messages.RoleAI {
			return "tools", nil
		}
		return exit, nil
	}
}
