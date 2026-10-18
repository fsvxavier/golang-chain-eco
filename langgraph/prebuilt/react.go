package prebuilt

import (
	"context"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// ReactOptions configures CreateReactAgent.
type ReactOptions struct {
	Model models.ChatModel
	Tools []tools.Tool
	// Prompt is prepended as a system message when non-empty.
	Prompt string
	// Compile options (checkpointer, interrupts, store).
	Compile []graph.CompileOption
}

// CreateReactAgent builds the classic model <-> tools loop graph over a
// "messages" state (create_react_agent).
func CreateReactAgent(o ReactOptions) (*graph.Compiled, error) {
	tn := NewToolNode(o.Tools...)
	model := models.BindTools(o.Model, o.Tools...)
	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages()}).
		AddNode("agent", func(ctx context.Context, st graph.State) (any, error) {
			msgs, err := messages.Coerce(st["messages"])
			if err != nil {
				return nil, err
			}
			if o.Prompt != "" {
				msgs = append([]messages.Message{messages.System(o.Prompt)}, msgs...)
			}
			out, err := model.Generate(ctx, msgs)
			if err != nil {
				return nil, err
			}
			return graph.State{"messages": []messages.Message{out}}, nil
		}).
		AddNode("tools", tn.Node()).
		AddEdge(graph.Start, "agent").
		AddConditionalEdges("agent", ToolsCondition).
		AddConditionalEdges("tools", func(_ context.Context, st graph.State) (any, error) {
			msgs, _ := messages.Coerce(st["messages"])
			// stop when every tool answered in the last batch is return_direct
			for i := len(msgs) - 1; i >= 0 && msgs[i].Role == messages.RoleTool; i-- {
				t, ok := tools.Find(o.Tools, msgs[i].Name)
				rd, isRD := t.(tools.ReturnDirect)
				if !ok || !isRD || !rd.ReturnDirect() {
					return "agent", nil
				}
			}
			return graph.End, nil
		})
	return g.Compile(o.Compile...)
}
