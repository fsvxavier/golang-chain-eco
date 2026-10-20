# 2. Core concepts

**English** | [Português](../../pt-BR/manual/02-conceitos.md) | [Español](../../es-ES/manual/02-conceitos.md)

## 2.1 Layers

```
deepagents ──► agents (create_agent + middleware) ──► langgraph/graph (Pregel)
                    │                                      │
                    └──► langgraph/prebuilt (ToolNode)     ├─ channels  (state slots + reducers)
                                                           ├─ checkpoint (Saver)
core (messages, models, tools, runnables, …)  ◄────────────┴─ store (long-term memory)
```

Each layer uses the one below it. You can stop at any level:

- **core**: simple chains (`prompt | model | parser`), RAG, structured extraction.
- **langgraph**: flows with state, branches, loops, pauses and persistence.
- **agents**: a ready-made "model decides, tool executes" loop with middleware.
- **deepagents**: agent with planning, files and delegation.

## 2.2 Messages (`core/messages`)

A `messages.Message` represents one conversation message. The type is single; the `Role` field tells them apart:

| Role | Constructor | Meaning |
|---|---|---|
| `RoleSystem` | `messages.System(text)` | Instructions to the model |
| `RoleHuman` | `messages.Human(text)` | User's speech |
| `RoleAI` | `messages.AI(text, calls...)` | Model response, optionally with `ToolCalls` |
| `RoleTool` | `messages.Tool(text, toolCallID)` | Result of a tool |
| `RoleRemove` | `messages.Remove(id)` | Marks the removal of a message by ID, applied by the reducer |

Relevant fields: `ID`, `Content`, `Parts` (multimodal content), `ToolCalls`, `ToolCallID`, `Status` (`success`/`error`), `Artifact`, `Usage` (tokens), `ResponseMetadata`.

Useful functions:

```go
msg.Text()                          // message text
messages.Coerce(v)                  // string | Message | []Message -> []Message
messages.AddMessages(a, b)          // reducer: appends, updates by ID, applies Remove
messages.Filter(msgs, include, exclude)
messages.Trim(msgs, messages.TrimOptions{...})
messages.MergeRuns(msgs)            // joins consecutive messages with the same role
messages.ApproxTokens(msgs)         // approximate token count
messages.Last(msgs)                 // last message
```

## 2.3 Runnable (`core/runnables`)

`Runnable[I, O]` is the central abstraction: something that receives `I` and returns `O`.

```go
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, in I) (O, error)
}
```

Prompts, models (via `models.AsRunnable`), parsers, chains and retrievers are runnables. They compose with `Pipe`, `Pipe3`, `Pipe4`, `Parallel`, `Branch`, `WithRetry`, `WithFallbacks`. Chapter 4.

Optional capabilities, detected by interface: `Streamer[I, O]` (native streaming) and `Batcher[I, O]` (batch). Use the generic functions `runnables.Stream` and `runnables.Batch`; they use the native implementation when it exists and fall back to `Invoke` when it does not.

## 2.4 Configuration through context

The execution configuration travels in the `context.Context`:

```go
ctx = runnables.WithConfig(ctx, runnables.Config{
	RunName:        "my-chain",
	Tags:           []string{"prod"},
	Metadata:       map[string]any{"user": "ana"},
	Callbacks:      []callbacks.Handler{tracer},
	MaxConcurrency: 4,
	RecursionLimit: 50,
	Configurable:   map[string]any{"thread_id": "user-42"},
})
```

`WithConfig` **merges** with the configuration already present. Shortcut for graph threads: `graph.Thread("user-42")` returns a `Config` with `thread_id`.

## 2.5 State and channels (LangGraph)

The state of a graph is a `graph.State`, which is `map[string]any`. Each key is a **channel** with an update rule (reducer):

| Channel | Constructor | Behavior |
|---|---|---|
| Last value | `channels.LastValue()` (default) | The write replaces the value |
| Messages | `graph.Messages()` | `AddMessages` reducer |
| Accumulate list | `graph.Append()` | Concatenates lists |
| Sum | `graph.Sum()` | Sums integers |
| Ephemeral | `channels.Ephemeral(false)` | Lasts only one step |
| Custom | `channels.BinOp` | Any function `(current, new) -> result` |

Each node receives a **copy** of the state and returns a **partial update**; the runtime applies the updates through the reducers. More in chapter 7.

## 2.6 Execution in super-steps

The runtime is bulk-synchronous (Pregel style):

1. In each super-step, all scheduled nodes run **in parallel** over the same snapshot of the state.
2. The writes are applied by the reducers, in task order.
3. The runtime routes to the next nodes (static edges, joins, conditional routers, `Command`, `Send`).
4. With a checkpointer, a checkpoint is saved after the input and after each step.

Practical consequence: nodes in the same step cannot see each other's writes. To sum the results of parallel branches, declare a reducer (`Append`, `Sum` or custom) on the channel that receives the writes.

## 2.7 Return types of a node

A node (`func(ctx, graph.State) (any, error)`) can return:

- `nil`: no update.
- `graph.State{...}`: partial update.
- `graph.Command{Update: ..., Goto: []string{...}}`: update and flow redirection.
- `[]graph.Send{...}`: dynamic fan-out (map-reduce).

## 2.8 Tools

A `tools.Tool` exposes a name, a description, the JSON Schema of the arguments and an execution function. `tools.New[Args]` generates the schema by reflecting on the `Args` struct. The model "calls" the tool by requesting `ToolCalls`; the agent runs it and returns a `RoleTool` message. Chapter 5.

## 2.9 Callbacks

Any execution emits events (start/end/error of a chain, model, tool, node). `callbacks.Handler` consumes these events for logs, tracing and metrics. Handlers go in the context: `callbacks.WithHandlers(ctx, h1, h2)`. Chapter 12.

## 2.10 Errors you will see

| Error | When |
|---|---|
| `*graph.GraphRecursionError` | The graph went past the `RecursionLimit` (default 25 steps) |
| `*tools.Error` (via `tools.Errorf`) | "Model" error: becomes a tool message and the loop continues |
| `*middleware.LimitError` | Model/tool call limit reached (with `ExitBehavior: "error"`) |
| `*middleware.PIIError` | PII detected with the `block` strategy |
| `*mcp.RPCError` | JSON-RPC error returned by an MCP server |

---

Previous: [1. Introduction and installation](01-introducao-e-instalacao.md) · Next: [3. Models and providers](03-modelos-e-provedores.md) · [Manual index](README.md)
