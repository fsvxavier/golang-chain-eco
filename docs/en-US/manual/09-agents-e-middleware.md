# 9. Agents and middleware

**English** | [Português](../../pt-BR/manual/09-agents-e-middleware.md) | [Español](../../es-ES/manual/09-agents-e-middleware.md)

`agents.Create` builds the classic agent loop: the model decides, the tools run, the result goes back to the model, until there is a final answer. It is the equivalent of LangChain's `create_agent`.

## 9.1 Creating an agent

```go
agent, err := agents.Create(agents.Options{
	Model:        m,
	Tools:        []tools.Tool{search, sendEmail},
	SystemPrompt: "You are a customer support assistant.",
	Middleware:   []agents.Middleware{ /* see 9.4 */ },
	Compile:      []graph.CompileOption{graph.WithCheckpointer(saver)},
})
```

| Field | Role |
|---|---|
| `Model` | **Required.** Any `models.ChatModel` |
| `Tools` | Available tools (the ones from middleware are added) |
| `SystemPrompt` | System prompt |
| `Middleware` | Ordered list of middleware |
| `ResponseFormat` | Requests JSON output; result in `state["structured_response"]` |
| `StateSchema` | Extra state channels (`graph.Schema`) |
| `Compile` | Graph compilation options (checkpointer, store, interrupts, cache) |

The return value is a `*graph.Compiled`: everything from chapters 7 and 8 applies (`Invoke`, `Stream`, `StreamEvents`, `GetState`, threads...).

## 9.2 Running

```go
out, err := agent.Invoke(ctx, graph.State{"messages": "What's the weather in Lisbon?"})
msgs := out["messages"].([]messages.Message)
fmt.Println(msgs[len(msgs)-1].Text())
```

The `messages` input accepts a `string`, a `messages.Message` or a `[]messages.Message`. With a checkpointer and `graph.Thread(id)` in the context, subsequent calls continue the conversation.

### Streaming

```go
for ev, err := range agent.StreamEvents(ctx, graph.State{"messages": "hi"}, graph.ModeMessages) {
	if err != nil {
		log.Fatal(err)
	}
	if msg, ok := ev.Data.(messages.Message); ok {
		fmt.Print(msg.Text())
	}
}
```

## 9.3 Internal flow and extension points

```
before_agent → [ before_model → model → after_model ] ⇄ tools → after_agent
```

A middleware implements `Name() string` and any combination of these hooks:

| Hook | When it runs |
|---|---|
| `BeforeAgent(ctx, st)` | Once, before everything |
| `BeforeModel(ctx, st)` | Before each model call |
| `WrapModelCall(ctx, req, next)` | Around the model call (can change `req` or replace the result) |
| `AfterModel(ctx, st)` | After each model response |
| `WrapToolCall(ctx, call, next)` | Around each tool execution |
| `AfterAgent(ctx, st)` | Once, at the end |
| `Tools() []tools.Tool` | (optional) adds tools |
| `StateSchema() graph.Schema` | (optional) adds state channels |

State hooks return `nil`, a `graph.State` update or a `graph.Command`. To divert the flow, include `jump_to`:

```go
return graph.State{"jump_to": agents.JumpEnd}, nil // or agents.JumpModel, agents.JumpTools
```

## 9.4 Built-in middleware (`agents/middleware`)

```go
Middleware: []agents.Middleware{
	middleware.ModelCallLimit{RunLimit: 10},
	middleware.ToolRetry{Options: runnables.RetryOptions{MaxAttempts: 3}},
	middleware.Summarization{Model: m, MaxTokens: 8000, KeepMessages: 12},
	middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail, middleware.PIICreditCard}, Strategy: middleware.PIIRedact},
	middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_email": {}}},
},
```

| Middleware | Purpose | Main fields |
|---|---|---|
| `ModelCallLimit` | Limits model calls (cost, loops) | `ThreadLimit`, `RunLimit`, `ExitBehavior` (`"end"` default or `"error"`) |
| `ToolCallLimit` | Limits tool calls (of one or of all) | `ToolName`, `ThreadLimit`, `RunLimit`, `ExitBehavior` (`"continue"`, `"error"`, `"end"`) |
| `ModelFallback` | Tries other models if the main one fails | `Fallbacks []models.ChatModel` |
| `ModelRetry` | Repeats the model call with backoff | `Options runnables.RetryOptions` |
| `ToolRetry` | Repeats tool execution | `Options`, `Tools` (restricts to some names) |
| `Summarization` | Summarizes the history when it exceeds a limit | `Model`, `MaxTokens` (4000), `KeepMessages` (20), `Counter`, `Prompt` |
| `ContextEditing` | Clears old tool results | `TriggerTokens` (100000), `Keep` (3), `Placeholder` |
| `TodoList` | Gives the agent the `write_todos` tool and the `todos` key | `SystemPrompt` |
| `PII` | Removes personal data from the input | `Kinds`, `Strategy` |
| `HumanInTheLoop` | Pauses to approve/edit/reject tools | `Tools map[string]ToolPolicy` |

### `PII`

Ready-made detectors: `PIIEmail`, `PIICreditCard` (and others in `context.go`). Strategies:

| Constant | Result |
|---|---|
| `middleware.PIIRedact` (default) | `[REDACTED_EMAIL]` |
| `middleware.PIIMask` | `****1234` |
| `middleware.PIIBlock` | `*middleware.PIIError` error |

Your own detectors: `middleware.PIIKind{Name: "cpf", Pattern: regexp.MustCompile(...)}`. The stored history already contains the sanitized text.

> Detection is regex-based: it helps, but it does not replace a privacy review. Validate it with your real data.

### `Summarization` and `ContextEditing`

They control the size of the context in long conversations. `Summarization` replaces old messages with a summary generated by the model; `ContextEditing` only clears old tool results (cheaper, with no model call). They can be used together.

## 9.5 Human approval of tools (`HumanInTheLoop`)

Requires a checkpointer and a `thread_id`.

```go
policy := map[string]middleware.ToolPolicy{
	"send_email":   {},                                                   // approve, edit or reject
	"delete_file":  {Allowed: []string{middleware.Approve, middleware.Reject}}, // no editing
}

out, _ := agent.Invoke(ctx, graph.State{"messages": "Send the report to Bob"})
// pauses before send_email; the request comes in graph.Interrupts(out)

out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
	Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	// or {Type: middleware.Edit, EditedAction: &middleware.ActionRequest{Name: "send_email", Args: map[string]any{...}}}
	// or {Type: middleware.Reject, Message: "do not send"}
}})
```

- `Interrupt.Value` is a `middleware.HITLRequest` with the `ActionRequests` (name, arguments, description) and, per tool, the allowed decisions.
- Answer with **one decision per action**, in the same order.
- `Reject` returns the message you provide to the model; it can propose another approach.
- Tools not listed in `Tools` run without review.

Examples: `hitl_agent`, `customer_support`.

## 9.6 Your own middleware

Implement `Name()` and whichever hooks you want:

```go
type Timing struct{}

func (Timing) Name() string { return "timing" }

func (Timing) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	t := time.Now()
	out, err := next(ctx, req) // req.System, req.Messages, req.Tools and req.Model can be changed before
	log.Println("model took", time.Since(t))
	return out, err
}
```

Middleware that adds a tool and state:

```go
type Audit struct{ Log *slog.Logger }

func (Audit) Name() string { return "audit" }
func (Audit) StateSchema() graph.Schema { return graph.Schema{"audit": graph.Append()} }
func (a Audit) WrapToolCall(ctx context.Context, c messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	a.Log.Info("tool", "name", c.Name)
	return next(ctx, c)
}
```

`ModelRequest` exposes `Model`, `System`, `Messages`, `Tools`, `Options` and `State`: you can swap the model per request (routing by cost), filter tools by context or inject instructions.

### Order

In the `Wrap*` hooks, the **first** middleware in the list is the outermost (it calls the following ones through `next`). `Before*`/`After*` hooks run in sequence; do not rely on the order between them. Combine them according to the effect you want (for example, `ModelRetry` outside `ModelFallback` repeats the whole chain) and test the combination (see `examples/langchain/middleware_stack`).

## 9.7 Structured output in agents

```go
type Report struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}

agent, _ := agents.Create(agents.Options{
	Model: m,
	ResponseFormat: &models.ResponseFormat{
		Name: "report",
		Schema: map[string]any{ // JSON Schema of the Report type
			"type": "object",
			"properties": map[string]any{
				"title":   map[string]any{"type": "string"},
				"bullets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"title", "bullets"},
		},
	},
})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Summarize the article"})
raw := out["structured_response"] // parsed from the JSON of the final response
```

Complete example: `examples/langgraph/structured_output`.

## 9.8 Testing agents without network

```go
fake := models.NewFake(
	messages.AI("", messages.ToolCall{ID: "1", Name: "get_weather", Args: map[string]any{"city": "Lisbon"}}),
	messages.AI("Sunny in Lisbon."),
)
agent, _ := agents.Create(agents.Options{Model: fake, Tools: []tools.Tool{weather}})
out, _ := agent.Invoke(ctx, graph.State{"messages": "weather in Lisbon?"})
// check out["messages"] and fake.Calls
```

## 9.9 When to use what

| Situation | Use |
|---|---|
| Minimal model/tools loop | `prebuilt.CreateReactAgent` |
| Production agent with limits, retry, PII, approval | `agents.Create` + middleware |
| Flow with fixed steps, branches and parallelism | `graph.StateGraph` |
| Long tasks with planning, files and subagents | `deepagents.Create` |

---

Previous: [8. Persistence, HITL and streaming](08-persistencia-hitl-streaming.md) · Next: [10. Deep Agents](10-deep-agents.md) · [Manual index](README.md)
