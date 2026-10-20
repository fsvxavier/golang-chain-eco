# Agent Protocol in Go (`agentprotocol/`)

**English** | [Português](../pt-BR/AGENT-PROTOCOL.md) | [Español](../es-ES/AGENT-PROTOCOL.md)

Implementation of the [Agent Protocol](https://github.com/langchain-ai/agent-protocol) (API v0.1.6 + *Agent Streaming Protocol*) over the compiled graphs of this project. A graph served here can be operated by any protocol client, including the `agentprotocol.Client` in this package. Stdlib only.

Tutorial: [manual/17-agent-protocol.md](manual/17-agent-protocol.md). Examples: [`examples/agentprotocol/`](../../examples/agentprotocol/).

The existing `remote/server` package serves the LangGraph Platform dialect (`/threads/{id}/runs/...`). `agentprotocol` serves the open protocol (`/runs`, `/threads`, `/agents`, `/store`, per-thread streaming). The two coexist; choose by the client you need to serve.

## What the protocol defines

| Resource | Purpose | Endpoints |
|---|---|---|
| **Agents** | introspection: name, capabilities, JSON schemas | `POST /agents/search`, `GET /agents/{id}`, `GET /agents/{id}/schemas` |
| **Threads** | multi-turn conversations with persistent state | `POST /threads`, `POST /threads/search`, `GET/PATCH/DELETE /threads/{id}`, `GET /threads/{id}/history`, `POST /threads/{id}/copy` |
| **Runs** | agent executions (in background, waiting, or streaming) | `POST /runs`, `/runs/wait`, `/runs/stream`, `/runs/search`, `GET/DELETE /runs/{id}`, `GET /runs/{id}/wait`, `GET /runs/{id}/stream`, `POST /runs/{id}/cancel` |
| **Store** | long-term memory across threads | `PUT/GET/DELETE /store/items`, `POST /store/items/search`, `POST /store/namespaces` |
| **Streaming** | ordered events per thread, filterable, with replay | `POST /threads/{id}/stream` (SSE), `GET /threads/{id}/stream` (WebSocket), `POST /threads/{id}/commands` |

All 27 OpenAPI endpoints are implemented.

## Serving

```go
srv, _ := agentprotocol.New(agentprotocol.Options{
    Agents: []agentprotocol.AgentSpec{{ID: "bot", Name: "Bot", Description: "...", Graph: compiled}},
    APIKey: os.Getenv("AP_KEY"),   // optional: x-api-key or Authorization: Bearer
    Store:  store.NewMemory(),      // /store/*; swap for sqlstore/redis/mongo
})
http.ListenAndServe(":8123", srv)
```

The graph needs a **checkpointer** to have thread state (`graph.WithCheckpointer`). Without one, runs work but threads do not keep state, and `history`/`state.*`/resume have nothing to read.

`AgentSpec` accepts `InputSchema/OutputSchema/StateSchema/ConfigSchema`; without them the server returns a chat schema (`messages`). Several agents: the run body says `agent_id` (with a single registered agent it is the default).

## Execution model

- **Thread** = durable identity (state in the checkpointer) + in-memory record (metadata, status, event buffer). Status: `idle`, `busy`, `interrupted`, `error`.
- **Run** in background on a goroutine; status `pending` until it finishes, then `success`, `error` or `interrupted` (cancelled or paused by `Interrupt`).
- **One active run per thread**: another `POST /runs` on the same thread responds `409`. (Through the streaming protocol, `run.start` during a run **enqueues** the input to run right after.)
- **Interrupted thread**: the next `run` with `input` **resumes** the graph (`Command{Resume}`). The resume value is `input.resume`, if present; otherwise the `messages`, if that is all there is; otherwise the whole `input` object. To answer by `interruptId` (with `update`/`goto`), use the `input.respond` command.
- **Run without `thread_id`** (stateless): ephemeral thread, deleted (with its checkpoint) at the end.
- `on_completion: delete` deletes the run; `on_disconnect: cancel` cancels if the client disappears (default `continue`); `webhook` receives `POST {run, values, messages}` on completion; `if_not_exists: create` creates the thread on the spot (default `reject`).
- **Persistence after restart**: a `GET /threads/{id}` rebuilds the thread from the checkpointer. Run records are written on every status change to a `RunStore` (`Options.RunStore`): the default (`NewMemoryRunStore`) keeps them in memory only, and `NewStoreRunStore(store)` writes them to a `langgraph/store.Store` (namespace `["agentprotocol","runs",threadID]`), which survives restarts when the store is durable. After a restart, `GET /runs/{id}` and `POST /runs/search` return those runs; `DELETE` removes them from the store. A run that was still `pending` when the process died is marked `error` (with the message `server stopped before the run finished`) at startup, because the protocol has no failure state for a crash. The event buffer (`GET /runs/{id}/stream`, thread streaming) stays in memory only: after a restart, a restored run has no events to replay. Cancelling or rolling back a restored run does not act on the graph (the run is no longer executing).

### Messages

`Message{role, content, id, metadata}` with `content` as a string or blocks. Input accepts `user|human`, `assistant|ai`, `system`, `tool`; non-text blocks become their text. Output uses LangChain roles (`human`, `ai`, `system`, `tool`) and `metadata.tool_calls` on `ai` messages.

### Time travel

- `GET /threads/{id}/history?limit&before` — revisions, newest first (`before` = checkpoint id).
- `PATCH /threads/{id}` with `checkpoint` + `values`/`messages` — **forks** from that checkpoint (it becomes the new tip).
- `POST /threads/{id}/copy` — independent copy of the history.
- `POST /runs/{id}/cancel?action=rollback` — cancels and restores the state from before the run (fork to the previous checkpoint; **the run's checkpoints are not deleted**).

## Per-thread streaming

Channels: `values`, `updates`, `messages`, `tools`, `lifecycle`, `input`, `checkpoints`, `custom` (and `custom:*`); `tasks` is accepted but never emitted.

| Channel | Events (`params.data.event`) |
|---|---|
| `lifecycle` | `started`, `running`, `completed`, `failed`, `interrupted` (+ `graphName`, `error`) |
| `messages` | `message-start` → `content-block-start` → `content-block-delta` (`text-delta`) → `content-block-finish` → … → `message-finish` (with `usage`); finalized `tool_call` blocks |
| `tools` | `tool-started` (`toolCallId`, `toolName`, `input`), `tool-finished` (`output`), `tool-error` |
| `input` | `input.requested` method with `interruptId` and `payload` |
| `values` / `updates` | full state / `{node, values}` per node |
| `checkpoints` | `{id, parentId, step, source}` at every super-step |

Guarantees: every event has a monotonic `seq` and an `eventId` (`evt_<seq>`); **messages never interleave** and every `message-start` closes with `message-finish`; the `toolCallId` of `tool-started/finished` is the **same id** as the message's `tool_call` block, including when the tool runs after a human approval (resume); a resumed run **does not resend** messages already announced. Models that do not stream (they only return a whole response) also produce message events, derived from `updates`.

**SSE**: `POST /threads/{id}/stream` with `{channels, namespaces?, depth?, since?}`. `since` (or the `Last-Event-ID` header) resends from the circular buffer (default 2000 events per thread) and then goes live, without losing or duplicating events. The thread is created on demand if the stream opens before the first run. A `: keepalive` comment every 15 s. A slow consumer is disconnected (instead of blocking the run).

**Commands** (`POST /threads/{id}/commands` or over the WebSocket): `run.start`, `input.respond` (by `interruptId`, with `update` and `goto`), `input.inject`, `state.get`, `state.listCheckpoints`, `state.fork`, `agent.getTree`. Responses are `{"type":"success","id","result","meta":{"appliedThroughSeq"}}` or `{"type":"error","id","error": <code>}`, using the protocol codes (`invalid_argument`, `unknown_command`, `no_such_interrupt`, `no_such_checkpoint`, `no_such_subscription`, `not_supported`...).

**WebSocket** (`GET /threads/{id}/stream`): minimal RFC 6455 — text, fragmentation, ping/pong, close; no extensions or compression. `subscription.subscribe/unsubscribe/reconnect` commands on the same connection; subscriptions persist across runs.

## Client

`agentprotocol.NewClient(url, apiKey)`: `SearchAgents`, `GetAgent`, `GetAgentSchemas`, `CreateThread`/`GetThread`/`SearchThreads`/`PatchThread`/`DeleteThread`/`CopyThread`/`ThreadHistory`, `CreateRun`/`CreateAndWait`/`WaitRun`/`GetRun`/`SearchRuns`/`CancelRun`/`DeleteRun`, `StreamRun` (SSE via `iter.Seq2`), `Events(thread, Filter)`, `Command(thread, method, params)`, `PutItem`/`GetItem`/`DeleteItem`/`SearchItems`/`ListNamespaces`. HTTP errors become `*StatusError{Status, Code, Message}`.

## Differences and limits (no sugarcoating)

- **Run streaming (REST)**: `POST /runs/stream` emits `event: <mode>` (`metadata`, `values`, `updates`, `messages`, `custom`, `end`); in `messages` mode the data is `{chunk, node}`. The exact format of the Python LangGraph Platform SDK may differ.
- **Namespaces**: streaming requests `graph.Subgraphs()`, so subgraph events arrive with `namespace` = the subgraph path (`["sub:0"]`, one `node:id` element per level; root = `[]`). The `namespaces` and `depth` filters of SSE apply to that path, and `agent.getTree` builds `children` from the namespaces seen in the thread's runs. Limits: each child's `status` is that of the thread's last run (there is no per-subgraph status), and the event history follows the thread's circular buffer.
- **`run.start` while a run is active** enqueues (it does not inject input into the running graph: the runtime does not support it).
- **`state.fork`** creates a **new thread** with the history up to the checkpoint, and runs if there is an `input`.
- **Checkpoint timestamps** are those of the moment the server saw them (the checkpointer does not persist them); checkpoints from before a restart use the thread's creation date.
- **No multi-process**: the event buffer and running runs live in the process, and recovery marks every `pending` record in the store as `error` at startup, which assumes a single process per store. Several replicas sharing the same store would interfere with each other; use thread affinity or one server per thread. There is no Redis or other distributed backend here. Multiple replicas would need three pieces: a shared event log (with a global `seq` per thread), a lease on running runs (so recovery does not mark live runs of another replica as errors), and routing of cancellation and commands to the replica that owns the run.
- **Retention**: at most 5000 finished runs per process; when exceeded, the oldest half is deleted from the `RunStore` too. Runs from a previous process are not counted and are not pruned.
- **No metrics/rate limits**; `APIKey` is a single key. Put a gateway in front for multi-tenant use.
- `GET /store/items` accepts a repeated `namespace` (`?namespace=a&namespace=b`) and the `a.b` format used by the SDKs; namespace names containing a dot need the first format.

## Tests

`go test -race ./agentprotocol/`: 100% offline (`httptest`, *fake* models). They cover introspection, thread lifecycle (history/patch/fork/copy/search/delete), runs (wait, background, stateless, 409, cancel, webhook, `on_completion`), run SSE, store, API key, the end-to-end streaming protocol (ordering and message closing, `toolCallId`, channel filter, replay, interruption + `input.respond`, commands and error codes), queued runs, **WebSocket** with a low-level client (fragmentation, subscribe/unsubscribe/reconnect), and state that survives a restart.
