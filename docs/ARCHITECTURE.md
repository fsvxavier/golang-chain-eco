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
  any `channels.BinOp` reducer). `graph.NewTyped[T]` is an optional typed layer:
  the schema comes from struct tags (`graph:"key,reducer=messages|append|sum"`),
  nodes take `T` and return partial updates checked against the tags.
* **Execution** is bulk-synchronous: every super-step runs all scheduled tasks
  concurrently against the same state snapshot, applies their writes through the
  reducers in task order, then routes (static edges, `AddJoin` barriers,
  conditional routers, `Command{Goto,Send}`, `Send` fan-out).
* **Nodes** return `nil`, a `State` update or a `Command`.
* **Persistence**: with `WithCheckpointer`, a checkpoint is stored after the input
  and each step. Threads are selected with `runnables.WithConfig(ctx, graph.Thread(id))`;
  `GetState`, `GetStateHistory`, `UpdateState` (and `checkpoint_id` for forking /
  time travel) are available. Checkpoints are keyed by (thread, namespace): the root
  graph uses `""`, a subgraph run by task `node:idx` uses `parent|node:idx`.
  Durability (`WithDurability` / `WithRunDurability`): `sync` (default, save every step),
  `async` (ordered background saves, flushed before `Invoke` returns), `exit` (save only
  on pause, end or failure).
* **Human-in-the-loop**: `graph.Interrupt(ctx, v)` pauses; the run returns the state
  with `graph.Interrupts(state)`. Resume with `Invoke(ctx, graph.Command{Resume: x})`.
  The node re-executes from its start, so side effects before `Interrupt` must be
  idempotent. `InterruptBefore/After` pause statically. Finished sibling tasks of an
  interrupted step are not re-run.
* **Streaming**: `StreamEvents(ctx, in, ModeValues|ModeUpdates|ModeCustom|ModeMessages)`.
  Nodes emit custom events with `graph.StreamWriter(ctx)`. With `graph.Subgraphs()`,
  events of nested graphs are forwarded with `Event.Namespace` set.
* **Subgraphs**: invoke a compiled graph inside a node. An inner `Interrupt` propagates
  to the outer run and resumes correctly, with or without the subgraph's own
  checkpointer (its checkpoint lives in its namespace).

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
  tasks and rubric evaluations are pre-registered). Go scalars (`int64`, `uint*`,
  `float32`, `time.Time`, `time.Duration`, `[]byte`) keep their type; untyped JSON
  numbers come back as `int` or `float64`. `checkpoint.Strict(true)` makes saving an
  unregistered struct an error instead of a silent `map[string]any`.
  `internal/sqlitetest` runs the saver on real SQLite
  (`go test ./internal/sqlitetest/...`; the driver is a test-only dependency).
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

## Integrations (`integrations/`, `providers/`)

* **One small contract per kind of backend**, so a new service is a handful of methods:
  `vectorstore.Backend` (`Upsert`, `Query`, `Delete`; `vectorstore.New(embedder, backend)` adds
  embedding, filters and MMR), `checkpoint.Saver` (4 methods), `store.Store`, `retrievers.Retriever`,
  `backends.Backend` / `backends.Executor` for Deep Agents.
* **No SDKs.** HTTP/JSON services use `net/http`; the wire protocols that had no HTTP form are implemented
  directly with the standard library: RESP (Redis), MongoDB OP_MSG + BSON + SCRAM-SHA-256, Bolt/PackStream
  (Memgraph), the Kubernetes exec WebSocket, Digest auth (Infinispan) and AWS SigV4 (`providers/bedrock.Sign`,
  reused by DynamoDB, S3, Kendra, Lambda). SQL backends take a `*sql.DB`; the driver is the caller's choice.
* **One conformance suite** (`internal/conformance`: `Saver`, `Store`, `Vector`) is run against every backend,
  in-memory or real, so they all behave like the reference implementations. Tests that need a real service
  are env-gated and skip themselves; the ones needing a SQL driver live in the `internal/pgtest` package (test-only driver dependencies)
  to keep the library code itself free of third-party imports.
* **`openai.Chat` is also the base for dialects** of the Chat Completions API. `Path`, `StreamPath`, `Query`,
  `TokenFunc` (per-request OAuth/IAM token) and `Rewrite` (edit the body) let IBM watsonx.ai reuse the whole
  request/stream/tool-call pipeline instead of duplicating it.
* **`backends.Remote` turns "run a shell command somewhere" into a full Deep Agents backend** (read, write, edit,
  ls, glob, grep, execute) with POSIX shell snippets. `kubernetes.Sandbox` provides the runner for a throwaway
  pod; any other isolated runner (container, VM, cloud sandbox) plugs in the same way.
* **Permission-aware retrieval** (`spicedb.FilterRetriever`, `permit.FilterRetriever`) checks every retrieved
  document against the policy engine and fails closed: a missing id or a failed check never leaks content.
* **Frontend protocol**: `agui.Handler` maps graph stream events (`messages`, `updates`, `values`) to AG-UI
  events over SSE (`RUN_STARTED`, `TEXT_MESSAGE_*`, `STEP_*`, `STATE_SNAPSHOT`, `RUN_FINISHED`/`RUN_ERROR`).
* **Honest verification levels** are recorded per integration in `docs/INTEGRATIONS.md` (real service vs
  contract test) and the provider coverage in `docs/PROVIDERS.md` / `docs/PROVIDERS-ANALISE.md`.

## Differences from the Python libraries

* State is a `map[string]any`, not a TypedDict/Pydantic model; use `graph.NewTyped[T]`
  or `tools.Decode` to work with structs.
* Not ported: Studio, CLI build/deploy, the managed LangSmith sandbox/context-hub
  backends and hosted experiments. Offline evals live in `evals/`; `langgraph/dev`
  serves graphs over the Agent Protocol for development.

## langmem

`langmem/` sits beside `agents/`: it depends on `langgraph/store`, `langgraph/graph`, `core/{models,tools,embeddings}` and `agents` (for `Middleware`), and nothing depends on it.

- **Memory kinds are tools.** Each `Schema` is offered to the model as a tool (the Go equivalent of LangMem's Pydantic schemas handed to trustcall); updates repeat `json_doc_id`, deletes use `RemoveDoc`. This keeps one extraction protocol for every kind of memory and every provider with tool calling.
- **Pure core, stateful shell.** `Manager.Extract` takes and returns memories; `StoreManager` wraps it with retrieval, seeding, phases and minimal writes (only changed items are `Put`).
- **Semantic search without changing `store.Store`.** `IndexedStore` keeps the vector inside the item value, so it works over every existing backend (memory, sql, redis, mongo, dynamodb); `Query` degrades to keyword ranking when the store is not a `SemanticSearcher`.
- **Namespaces are templates** resolved from `runnables.Config.Configurable`; a missing variable is an error, never a shared namespace.
- **Summaries are incremental and non-destructive**: the model sees `summarized_messages`, `messages` stays complete, and `RunningSummary` travels in the checkpointed state.
- **Background formation** is a debounced timer per thread, not a queue worker; see docs/LANGMEM.md for the remote-executor gap.

## agentprotocol, ciam, openswe

- **agentprotocol** serves compiled graphs. The thread is the unit: one `hub` per thread holds a ring buffer of typed streaming events (monotonic `seq`, replay by `since`), fed by a translator that maps graph events and callbacks onto the protocol's channels; REST handlers, SSE, commands and a small RFC 6455 WebSocket all read the same hub. Thread records live in memory and are rebuilt from the checkpointer; run records go through a `RunStore` (memory by default, `NewStoreRunStore` over any `store.Store` to survive restarts). Subgraph events keep their namespace, so `namespaces`/`depth` filters and `agent.getTree` see child graphs. `graph.Compiled.Saver()` was added so the server can list, copy and delete checkpoints.
- **ciam** keeps chunks, file hashes and memories in any `store.Store`; searching loads the project's corpus once (BM25 statistics + vectors) and fuses cosine and BM25 with RRF (cosine by scan, or through an optional vector index such as `vectorstore.NewHNSW`). `ciam api` serves the same tools over plain HTTP. Memories are `langmem` memories. Capabilities are `tools.Tool` values first and are served over `mcp/server` second, so in-process agents and editors share one implementation.
- **openswe** is composition: `deepagents` + a per-thread sandbox backend + guard middleware + GitHub tools. Rules are enforced twice (prompt and code). Durable state (findings, watches, automations, PR<->thread links, sandbox ids) goes through `store.Store`, never a bespoke database; a thread's work is serialised by the `triggers.Dispatcher` (one active run per thread, queue for the rest). Slack and Linear are adapters over the same dispatcher; their replies go through `Notifier` wrappers around the runner.
