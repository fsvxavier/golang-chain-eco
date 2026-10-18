// Package deepagents is the Go port of LangChain's Deep Agents harness: a
// ready-made agent with planning (write_todos), a virtual filesystem,
// subagent delegation, conversation summarization, optional memory, skills
// and human-in-the-loop review, all built on agents.Create.
package deepagents

import (
	"fmt"

	"github.com/fsvxavier/golang-chain-eco/agents"
	amw "github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	dmw "github.com/fsvxavier/golang-chain-eco/deepagents/middleware"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// BasePrompt is appended after the user's system prompt.
const BasePrompt = `You are a deep agent: an AI assistant that completes tasks with tools.

## Core behavior
- Be concise and direct; skip preamble.
- Plan multi-step work with write_todos, and keep the list current.
- Understand first (read files), then act, then verify against what was asked.
- Keep working until the task is complete; stop only when done or genuinely blocked.
- If something fails repeatedly, analyse why instead of retrying the same approach.`

// SubAgentSpec declares a custom subagent.
type SubAgentSpec struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []tools.Tool // defaults to the main agent's tools
	Model        models.ChatModel
	Middleware   []agents.Middleware
}

// Options configures Create.
type Options struct {
	Model        models.ChatModel
	Tools        []tools.Tool
	SystemPrompt string
	// Backend is the virtual filesystem (default backends.State{}).
	Backend backends.Backend
	// SubAgents are extra delegates; a "general-purpose" one is always added
	// unless a spec with that name exists.
	SubAgents []SubAgentSpec
	// Permissions restricts file access (first matching rule wins).
	Permissions []backends.Permission
	// Memory lists memory file paths (e.g. "/AGENTS.md") loaded into the prompt.
	Memory []string
	// Skills lists skill directories ("/skills/").
	Skills []string
	// InterruptOn requires human approval for the named tools.
	InterruptOn map[string]amw.ToolPolicy
	// SummarizeAt is the token count that triggers summarization (default 60000; <0 disables).
	SummarizeAt int
	Middleware  []agents.Middleware
	// ResponseFormat requests structured output.
	ResponseFormat *models.ResponseFormat
	Checkpointer   checkpoint.Saver
	Store          store.Store
}

// Create builds the deep agent.
func Create(o Options) (*graph.Compiled, error) {
	if o.Model == nil {
		return nil, fmt.Errorf("deepagents: Model is required")
	}
	be := o.Backend
	if be == nil {
		be = backends.State{}
	}
	if len(o.Permissions) > 0 {
		be = backends.WithPermissions(be, o.Permissions...)
	}
	baseStack := func(model models.ChatModel) []agents.Middleware {
		stack := []agents.Middleware{amw.TodoList{}, dmw.Filesystem{Backend: be}}
		if o.SummarizeAt >= 0 {
			stack = append(stack, amw.Summarization{Model: model, MaxTokens: orInt(o.SummarizeAt, 60000), KeepMessages: 20})
		}
		return append(stack, dmw.PatchToolCalls{})
	}
	prompt := o.SystemPrompt
	if prompt != "" {
		prompt += "\n\n"
	}
	prompt += BasePrompt

	specs := o.SubAgents
	hasGP := false
	for _, s := range specs {
		hasGP = hasGP || s.Name == "general-purpose"
	}
	if !hasGP {
		specs = append([]SubAgentSpec{{
			Name:        "general-purpose",
			Description: "General-purpose agent for researching complex questions and executing multi-step tasks. Same tools as the main agent.",
		}}, specs...)
	}
	var subs []dmw.SubAgent
	for _, s := range specs {
		m := s.Model
		if m == nil {
			m = o.Model
		}
		ts := s.Tools
		if ts == nil {
			ts = o.Tools
		}
		sp := s.SystemPrompt
		if sp == "" {
			sp = BasePrompt
		}
		ag, err := agents.Create(agents.Options{
			Model: m, Tools: ts, SystemPrompt: sp,
			Middleware: append(baseStack(m), s.Middleware...),
		})
		if err != nil {
			return nil, fmt.Errorf("deepagents: subagent %q: %w", s.Name, err)
		}
		subs = append(subs, dmw.SubAgent{Name: s.Name, Description: s.Description, Agent: ag})
	}

	stack := baseStack(o.Model)
	stack = append(stack, dmw.Subagents{Agents: subs})
	if len(o.Memory) > 0 {
		stack = append(stack, dmw.Memory{Backend: be, Sources: o.Memory})
	}
	if len(o.Skills) > 0 {
		stack = append(stack, dmw.Skills{Backend: be, Sources: o.Skills})
	}
	stack = append(stack, o.Middleware...)
	if len(o.InterruptOn) > 0 {
		stack = append(stack, amw.HumanInTheLoop{Tools: o.InterruptOn})
	}
	var copts []graph.CompileOption
	if o.Checkpointer != nil {
		copts = append(copts, graph.WithCheckpointer(o.Checkpointer))
	}
	if o.Store != nil {
		copts = append(copts, graph.WithStore(o.Store))
	}
	return agents.Create(agents.Options{
		Model: o.Model, Tools: o.Tools, SystemPrompt: prompt, Middleware: stack,
		ResponseFormat: o.ResponseFormat, Compile: copts,
	})
}

func orInt(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}
