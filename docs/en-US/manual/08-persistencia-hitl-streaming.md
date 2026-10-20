# 8. Persistence, human-in-the-loop and streaming

**English** | [Português](../../pt-BR/manual/08-persistencia-hitl-streaming.md) | [Español](../../es-ES/manual/08-persistencia-hitl-streaming.md)

## 8.1 Checkpointers and threads

With a checkpointer, each super-step is saved by `thread_id`. This makes it possible to continue conversations, pause for human approval, inspect history and go back in time.

```go
saver := checkpoint.NewMemory()
app, _ := g.Compile(graph.WithCheckpointer(saver))

ctx := runnables.WithConfig(context.Background(), graph.Thread("user-42"))
out, _ := app.Invoke(ctx, graph.State{"messages": "hi"})
out, _ = app.Invoke(ctx, graph.State{"messages": "and now?"}) // continues the same conversation
```

- Without a `thread_id` in the configuration, the graph does not persist between calls.
- Each thread has its own timeline of checkpoints.
- The same rule applies to `agents.Create` (via `Options.Compile`) and `deepagents.Create` (via `Options.Checkpointer`).

### Durability

By default (`graph.DurabilitySync`) the checkpoint is written before each super-step proceeds. Two alternatives trade guarantees for latency:

| Mode | When it writes | Use |
|------|--------------|-----|
| `DurabilitySync` | At every super-step, before continuing (default) | Maximum guarantee: a crash loses at most the step in progress |
| `DurabilityAsync` | At every super-step, in a background goroutine, in order | Lower latency; `Invoke`/`Stream` only return after the last save. A save error aborts the execution at the next super-step |
| `DurabilityExit` | Only on pause (interrupt), on finish or on failure between super-steps | Fewer writes; a crash in the middle of the execution loses the steps since the last checkpoint |

```go
app, _ := g.Compile(graph.WithCheckpointer(saver), graph.WithDurability(graph.DurabilityAsync)) // graph default

// per call, overrides the graph default
ctx := graph.WithRunDurability(runnables.WithConfig(context.Background(), graph.Thread("user-42")), graph.DurabilityExit)
out, err := app.Invoke(ctx, graph.State{"messages": "hi"})
```

- Pauses of subgraphs with their own checkpointer are always written, in any mode, because the parent needs to resume from them.
- In `exit`, a node failure between super-steps writes the last good state, so the thread can be resumed. A reducer failure in the middle of a super-step writes nothing.

### Checkpoint backends

| Backend | Constructor | Note |
|---|---|---|
| Memory | `checkpoint.NewMemory()` | Tests and prototypes; loses everything on exit |
| SQLite / PostgreSQL | `sqlsaver.New(ctx, db, sqlsaver.SQLite \| sqlsaver.Postgres)` | `database/sql`; you bring the driver |
| Redis | `redis.NewSaver(redis.New("host:6379"))` | Own RESP client |
| MongoDB / Atlas | `mongo.NewSaver(client)` | Own OP_MSG; no `mongodb+srv://` |
| DynamoDB | `dynamodb.NewSaver(ctx, client, "table")` | Creates the table on demand; item ≤ 400 KB |
| S3 and compatible | `s3.NewSaver(client, "bucket", "prefix")` | AWS, MinIO, Tigris, R2... |

Example with SQLite:

```go
import (
	"database/sql"
	_ "modernc.org/sqlite" // or _ "github.com/jackc/pgx/v5/stdlib"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
)

db, _ := sql.Open("sqlite", "checkpoints.db")
saver, err := sqlsaver.New(ctx, db, sqlsaver.SQLite) // or sqlsaver.Postgres
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

### Serialization: custom types

Values are written as JSON. If the state contains your own types, register them so they survive the round trip:

```go
checkpoint.Register("myapp.Order", Order{})
checkpoint.Register("[]myapp.Order", []Order{})
```

Already registered: messages, `graph.Send`, files, todos, async tasks and rubric evaluations, plus the Go scalars `int8`…`int64`, `uint`…`uint64`, `float32`, `[]byte`, `time.Time` and `time.Duration`, which come back with their original type. Numbers without an explicit type still come back as `int` (integers) or `float64`. With `checkpoint.Strict(true)`, writing an unregistered struct becomes an error instead of turning into `map[string]any`.

### Custom backend

Implement `checkpoint.Saver` (4 methods):

```go
type Saver interface {
	Put(ctx context.Context, cp Checkpoint) (string, error)
	Get(ctx context.Context, cfg Config) (Checkpoint, bool, error)
	List(ctx context.Context, threadID string, limit int) ([]Checkpoint, error)
	DeleteThread(ctx context.Context, threadID string) error
}
```

Validate it with the `internal/conformance` suite (the same one used by the project's backends).

## 8.2 Human-in-the-loop with `Interrupt`

`graph.Interrupt(ctx, value)` pauses the execution and returns the value to the caller. On resume, `Interrupt` returns the answer sent.

```go
approve := func(ctx context.Context, s graph.State) (any, error) {
	// The first run pauses here. On resume, Interrupt returns the resume value.
	answer := graph.Interrupt(ctx, map[string]any{"question": "Approve sending?"})
	if answer != "yes" {
		return graph.State{"status": "cancelled"}, nil
	}
	return graph.State{"status": "sent"}, nil
}

out, _ := app.Invoke(ctx, graph.State{"status": "new"})
for _, i := range graph.Interrupts(out) {
	fmt.Println("paused at", i.Node, i.Value) // InterruptInfo{ID, Node, Value}
}
out, _ = app.Invoke(ctx, graph.Command{Resume: "yes"}) // resumes
```

Requirements and rules:

- **Requires a checkpointer and a `thread_id`**: the paused state is saved.
- **The node re-executes from the beginning on resume.** Keep idempotent (or move past the `Interrupt`) everything that has side effects and comes before it.
- Several simultaneous interrupts (parallel branches): answer by ID with `graph.Command{ResumeMap: map[string]any{id: value}}`.
- Sibling tasks already completed in the same step are not re-executed.

### Static pauses

```go
app, _ := g.Compile(graph.WithCheckpointer(saver), graph.InterruptBefore("tools"))
// or graph.InterruptAfter("planner")

out, _ := app.Invoke(ctx, input) // runs up to before "tools" and pauses
// inspect/edit the state...
out, _ = app.Invoke(ctx, nil)    // nil continues from where it stopped
```

## 8.3 Inspection and time travel

```go
snap, _ := app.GetState(ctx)            // Values, Next, Interrupts, CheckpointID, ParentID, Step
hist, _ := app.GetStateHistory(ctx, 10) // from most recent to oldest

// edits the state as if the "research" node had written it:
app.UpdateState(ctx, graph.State{"topic": "other"}, "research")

// fork from an old checkpoint:
old := runnables.WithConfig(ctx, runnables.Config{
	Configurable: map[string]any{"checkpoint_id": hist[3].CheckpointID},
})
app.Invoke(old, nil)
```

Uses: debugging an execution step by step, manually fixing a bad state and continuing, or exploring alternatives from an earlier point.

## 8.4 Long-term memory (`langgraph/store`)

The checkpoint keeps the state of **one thread**. The `store.Store` keeps knowledge **across threads** (preferences, facts about the user).

```go
type Store interface {
	Put(ctx, ns []string, key string, value map[string]any) error
	Get(ctx, ns []string, key string) (*Item, error)
	Delete(ctx, ns []string, key string) error
	Search(ctx, prefix []string, filter map[string]any, limit int) ([]Item, error)
	ListNamespaces(ctx, prefix []string) ([][]string, error)
}
```

| Backend | Constructor |
|---|---|
| Memory | `store.NewMemory()` |
| SQLite / PostgreSQL | `sqlstore` (`langgraph/store/sqlstore`) |
| Redis | `redis.NewStore(client)` |
| MongoDB | `mongo.NewStore(client)` |
| DynamoDB | `dynamodb.NewStore(ctx, client, "table")` (`Search` does a Scan) |
| S3 | `s3.NewStore(client, "bucket", "prefix")` |

Plug it into the graph with `graph.WithStore(s)`; inside the node, `graph.StoreFrom(ctx)`. In Deep Agents, `backends.Store` exposes a `Store` as a shared filesystem.

## 8.5 Streaming

`app.Stream(ctx, input)` returns the state at each step. `StreamEvents` lets you choose the modes:

```go
for ev, err := range app.StreamEvents(ctx, input, graph.ModeUpdates, graph.ModeMessages, graph.ModeCustom) {
	if err != nil {
		log.Fatal(err)
	}
	switch ev.Mode {
	case graph.ModeUpdates:  // ev.Node + update, each time a node finishes
	case graph.ModeValues:   // full state at each step
	case graph.ModeMessages: // LLM tokens as they arrive (ev.Data is messages.Message)
	case graph.ModeCustom:   // whatever the node sent via graph.StreamWriter(ctx)(value)
	}
}
```

Custom event, inside a node:

```go
write := graph.StreamWriter(ctx)
write(map[string]any{"progress": 40})
```

When to use each mode:

| Mode | Typical use |
|---|---|
| `ModeValues` | Debugging; a UI that redraws the whole state |
| `ModeUpdates` | Showing "what each step did" |
| `ModeMessages` | Chat with real-time typing |
| `ModeCustom` | Progress bars and business telemetry |

The iteration is an `iter.Seq2`. If you leave the `for` early (`break`), the execution is cancelled. To serve these events to a browser, see the AG-UI protocol in [chapter 11](11-mcp-remote-servidor.md#115-ag-ui-protocol).

## 8.6 Recipe: a chatbot that remembers and asks for approval

```go
saver := checkpoint.NewMemory() // swap for sqlsaver/redis in production
agent, _ := agents.Create(agents.Options{
	Model: m,
	Tools: []tools.Tool{search, sendEmail},
	Middleware: []agents.Middleware{
		middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_email": {}}},
	},
	Compile: []graph.CompileOption{graph.WithCheckpointer(saver)},
})

ctx := runnables.WithConfig(ctx, graph.Thread("conv-1"))
out, _ := agent.Invoke(ctx, graph.State{"messages": "Send the summary to Bob"})
if reqs := graph.Interrupts(out); len(reqs) > 0 {
	// show reqs[0].Value to the user; then:
	out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
		Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	}})
}
```

---

Previous: [7. LangGraph: StateGraph](07-langgraph.md) · Next: [9. Agents and middleware](09-agents-e-middleware.md) · [Manual index](README.md)
