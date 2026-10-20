# 17. Agent Protocol: serving and consuming agents through an open API

**English** | [Português](../../pt-BR/manual/17-agent-protocol.md) | [Español](../../es-ES/manual/17-agent-protocol.md)

The `agentprotocol` package publishes any compiled graph (`agents.Create`, `deepagents.Create`, a `StateGraph`) with the [Agent Protocol](https://github.com/langchain-ai/agent-protocol): a REST API **independent of framework** for agents in production, plus a **per-thread streaming** protocol (typed events, filters, replay, human approval). Full reference and limits: [docs/en-US/AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md).

Prerequisites: chapters 7 and 8 (graphs, checkpointer, interrupts) and 9 (agents).

## 17.1 When to use it

| You want… | Use |
|---|---|
| an external UI/service talking to your agent over HTTP | `agentprotocol.Server` |
| the LangGraph Platform dialect (official SDKs) | `remote/server` (chap. 11) |
| to call a Go agent from another Go process | `agentprotocol.Client` |
| to follow tokens, tools and approvals live | streaming channels (17.5) |

## 17.2 Serving an agent

```go
agent, _ := agents.Create(agents.Options{
	Model: model, Tools: tools,
	Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}, // thread state
})
srv, err := agentprotocol.New(agentprotocol.Options{
	Agents: []agentprotocol.AgentSpec{{ID: "bot", Name: "Bot", Description: "answers questions", Graph: agent}},
	APIKey: os.Getenv("AP_KEY"), // optional
})
http.ListenAndServe(":8123", srv)
```

- **Without a checkpointer** the agent runs, but threads do not keep state (no `history`, `state.get` or resume).
- In production, swap `checkpoint.NewMemory()` for `sqlsaver`, Redis, Mongo or DynamoDB (chap. 8 and 13) — and `Options.Store` for the matching long-term store.
- The server keeps threads/runs in memory, but rebuilds a thread from the checkpointer on first access after a restart.

## 17.3 Calling with the client

```go
c := agentprotocol.NewClient("http://localhost:8123", os.Getenv("AP_KEY"))

th, _ := c.CreateThread(ctx, agentprotocol.ThreadCreate{Metadata: map[string]any{"user": "ana"}})
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{
	ThreadID: th.ThreadID, AgentID: "bot",
	Messages: []agentprotocol.Message{{Role: "user", Content: "What's the weather in Lisbon?"}},
})
fmt.Println(out.Run.Status, out.Messages[len(out.Messages)-1].Content)
```

Three ways to run:

| Way | Call | When |
|---|---|---|
| wait | `CreateAndWait` (`POST /runs/wait`) | short answer |
| background | `CreateRun` + `WaitRun`/`GetRun` | long work; `webhook` notifies at the end |
| stream | `StreamRun` (`POST /runs/stream`) | incremental UI: `for ev, err := range c.StreamRun(ctx, req)` |

Without a `thread_id` the run is **stateless** (ephemeral thread, deleted at the end). With a busy thread the response is `409`. `on_completion: "delete"`, `on_disconnect: "cancel"` and `if_not_exists: "create"` change the lifecycle.

## 17.4 Threads, history and time travel

```go
hist, _ := c.ThreadHistory(ctx, th.ThreadID, 20, "")            // newest first
c.PatchThread(ctx, th.ThreadID, agentprotocol.ThreadPatch{
	Checkpoint: &agentprotocol.ThreadCheckpoint{CheckpointID: hist[3].Checkpoint.CheckpointID},
	Messages:   []agentprotocol.Message{{Role: "user", Content: "Actually, what about Porto?"}},
})                                                                // forks from that point
cp, _ := c.CopyThread(ctx, th.ThreadID)                           // independent copy
found, _ := c.SearchThreads(ctx, agentprotocol.ThreadSearchRequest{Metadata: map[string]any{"user": "ana"}})
```

Cancelling: `c.CancelRun(ctx, id, "interrupt", true)` keeps the progress; `"rollback"` restores the state to what it was before the run.

## 17.5 Per-thread streaming: typed events

The streaming protocol is **centered on the thread**. You open a filtered stream and send commands; the events arrive with increasing `seq`.

```go
filter := agentprotocol.Filter{Channels: []string{"lifecycle", "messages", "tools", "input"}}
go c.Command(ctx, "t1", "run.start", map[string]any{"assistantId": "bot", "input": "hi"})
for ev, err := range c.Events(ctx, "t1", filter) {
	data := ev.Params["data"].(map[string]any)
	switch ev.Method {
	case "messages": // message-start, content-block-start/delta/finish, message-finish
	case "tools":    // tool-started, tool-finished, tool-error (toolCallId links to the message block)
	case "lifecycle":// started, running, completed, failed, interrupted
	}
}
```

To reconnect without losing anything: `filter.Since = lastSeq` (or the `Last-Event-ID` header) resends from the buffer and continues live. Full example: `go run ./examples/agentprotocol/human_in_the_loop`.

## 17.6 Human approval

An agent with `HumanInTheLoop` (chap. 9) or `graph.Interrupt` (chap. 8) **pauses**: the thread becomes `interrupted` and the `input` channel emits `input.requested` with `interruptId` and `payload`. Answer with:

```go
c.Command(ctx, "t1", "input.respond", map[string]any{
	"namespace": []string{}, "interruptId": id,
	"response": []map[string]any{{"type": "approve"}},   // the value the interrupt expects
	// optional: "update": {...state...}, "goto": "no" or {"node": "x", "input": {...}}
})
```

(`response` is delivered to the `Interrupt`; invalid ids come back as a `no_such_interrupt` error.) Through the REST API, sending a new `run` with `input` on an interrupted thread also resumes it.

Messages that arrive during a run in progress (`run.start` or `input.inject`) go into the **queue** and run right after the current one.

## 17.7 WebSocket

`GET /threads/{id}/stream` with `Upgrade: websocket` exchanges commands and events on the same connection (`subscription.subscribe`, `unsubscribe`, `reconnect` + `run.start`, `input.respond`, `state.*`). The server is a minimal RFC 6455 (text, fragmentation, ping/pong; no compression). For the browser, use any WebSocket client and the same JSON messages `{"id": 1, "method": "...", "params": {...}}`.

## 17.8 Long-term memory (`/store`)

```go
c.PutItem(ctx, []string{"users", "ana"}, "prefs", map[string]any{"units": "metric"})
items, _ := c.SearchItems(ctx, agentprotocol.StoreSearchRequest{NamespacePrefix: []string{"users"}, Filter: map[string]any{"units": "metric"}})
```

It is the same `store.Store` as the graphs (`graph.WithStore`): pass it in `Options.Store` and the agent's nodes read what clients wrote.

## 17.9 Production

- **Authentication**: a single `APIKey`; for multiple users, put a gateway in front and propagate the identity in `config.configurable` (e.g. `user_id`), which reaches the tools and `langmem` (chap. 16).
- **Replicas**: the event buffer and the run registry are per process; use affinity by `thread_id`.
- **Limits**: the request body is limited to 16 MB; slow SSE consumers are disconnected instead of blocking the run.
- **Observability**: runs go through the `core/callbacks` callbacks (chap. 12); each run carries `run_id`, `thread_id` and `agent_id` in its metadata.
- **What is missing**: the `tasks` event and an event buffer shared across replicas. Subgraphs already appear as child namespaces, and finished runs persist with `Options.RunStore = agentprotocol.NewStoreRunStore(st)` (see the reference document).

Examples: `go run ./examples/agentprotocol/serve_and_call` and `.../human_in_the_loop`.

---

Previous: [16. Long-term memory](16-memoria-langmem.md) · Next: [18. ciam](18-ciam-contexto.md) · [Manual index](README.md)
