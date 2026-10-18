# Architecture

## Layers

```
deepagents ──► agents (create_agent + middleware) ──► langgraph/graph (Pregel)
                    │                                      │
                    └──► langgraph/prebuilt (ToolNode)     ├─ channels  (state slots + reducers)
                                                           ├─ checkpoint (Saver)
core (messages, models, tools, runnables, …)  ◄────────────┴─ store (long-term memory)
```

## LangGraph runtime (`langgraph/graph`)

* **State** is `map[string]any`; each key is a channel declared in a `Schema`
  (`LastValue` by default, or `graph.Messages()`, `graph.Append()`, `graph.Sum()`,
  any `channels.BinOp` reducer).
* **Execution** is bulk-synchronous: every super-step runs all scheduled tasks
  concurrently against the same state snapshot, applies their writes through the
  reducers in task order, then routes (static edges, `AddJoin` barriers,
  conditional routers, `Command{Goto,Send}`, `Send` fan-out).
* **Nodes** return `nil`, a `State` update or a `Command`.
* **Persistence**: with `WithCheckpointer`, a checkpoint is stored after the input
  and each step. Threads are selected with `runnables.WithConfig(ctx, graph.Thread(id))`;
  `GetState`, `GetStateHistory`, `UpdateState` (and `checkpoint_id` for forking /
  time travel) are available.
* **Human-in-the-loop**: `graph.Interrupt(ctx, v)` pauses; the run returns the state
  with `graph.Interrupts(state)`. Resume with `Invoke(ctx, graph.Command{Resume: x})`.
  The node re-executes from its start, so side effects before `Interrupt` must be
  idempotent. `InterruptBefore/After` pause statically. Finished sibling tasks of an
  interrupted step are not re-run.
* **Streaming**: `StreamEvents(ctx, in, ModeValues|ModeUpdates|ModeCustom|ModeMessages)`.
  Nodes emit custom events with `graph.StreamWriter(ctx)`.
* **Subgraphs**: invoke a compiled graph inside a node. Without its own checkpointer,
  an inner `Interrupt` propagates to the outer run and resumes correctly.

## Agents

`agents.Create` builds `before_agent → [before_model → model → after_model] ⇄ tools →
after_agent`. Middleware implements any of `BeforeAgent`, `BeforeModel`, `WrapModel`,
`AfterModel`, `WrapTool`, `AfterAgent`, plus optional `Tools()` and `StateSchema()`.
Hooks redirect flow by setting `jump_to` (`agents.JumpEnd|JumpTools|JumpModel`).
Built-ins: model/tool call limits, fallback, retry, summarization, context editing,
todo list, PII scrubbing, human-in-the-loop.

## Deep Agents

`deepagents.Create` = planning (`write_todos`) + virtual filesystem tools
(`ls read_file write_file edit_file glob grep`, `execute` for sandbox backends) +
`task` subagents + summarization + optional memory (`AGENTS.md`), skills
(`SKILL.md`) and HITL. Backends: `State` (graph state), `Store` (cross-thread),
`Filesystem` (disk, root-confined, symlink-checked), `LocalShell` (adds `execute`,
**runs commands on the host — trusted use only**), `Composite` (route by prefix).

## Persistence and operations

* `checkpoint.Memory` for tests; `checkpoint/sqlsaver` for SQLite/PostgreSQL via
  `database/sql` (bring your own driver). Values are JSON: register custom state
  types with `checkpoint.Register` (messages, `graph.Send`, files, todos, async
  tasks and rubric evaluations are pre-registered). Whole numbers come back as
  `int`, other numbers as `float64`.
  `internal/sqlitetest` is a separate module that runs the saver on real SQLite
  (`cd internal/sqlitetest && go test ./...`).
* **Node cache**: `graph.WithCachePolicy` on a node + `graph.WithCache(graph.NewMemoryCache())`.
* **Deferred nodes**: `graph.Deferred()` runs a node only when no other task is pending.
* **LangSmith**: `tracing/langsmith` is a `callbacks.Handler` that posts runs
  (`callbacks.WithHandlers(ctx, tracer)`), best effort, async queue, `Close()` flushes.
* **Remote graphs**: `remote.Graph` runs an assistant on an Agent Server (LangGraph
  Platform) behind the same `Invoke/Stream` API; `remote.Client` exposes threads and runs.
* **Deep Agents extras**: `deepagents/middleware.Rubric` (grader loop),
  `backends.WithPermissions` / `Options.Permissions` (allow/deny by operation and
  path glob, enforced for subagents too), `middleware.AsyncSubagents` with
  `LocalRunner` (goroutines) or `RemoteRunner` (Agent Server) and the
  `start/check/update/cancel/list` task tools.

## Differences from the Python libraries

* State is a `map[string]any`, not a TypedDict/Pydantic model; use `tools.Decode`
  to bind into structs.
* Not ported: durability modes, Studio/CLI, the managed LangSmith sandbox/context-hub
  backends, evals.
