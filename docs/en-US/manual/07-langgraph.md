# 7. LangGraph: StateGraph

**English** | [Português](../../pt-BR/manual/07-langgraph.md) | [Español](../../es-ES/manual/07-langgraph.md)

Use `langgraph/graph` when the flow has state, branches, loops or pauses. A graph is made of **nodes** (Go functions) connected by **edges**, working on a shared **state**.

## 7.1 Anatomy of a graph

```go
import (
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

schema := graph.Schema{
	"messages": graph.Messages(),      // accumulates with the add_messages reducer
	"steps":    graph.Sum(),           // sums integers
	"log":      graph.Append(),        // concatenates lists
	"topic":    channels.LastValue(),  // last value (default for undeclared keys)
}

g := graph.NewStateGraph(schema).
	AddNode("research", func(ctx context.Context, s graph.State) (any, error) {
		return graph.State{"log": []any{"researched " + s["topic"].(string)}, "steps": 1}, nil
	}).
	AddNode("write", writeNode).
	AddNode("review", reviewNode).
	AddEdge(graph.Start, "research").
	AddEdge("research", "write").
	AddConditionalEdges("write", func(ctx context.Context, s graph.State) (any, error) {
		if s["steps"].(int) > 3 {
			return graph.End, nil
		}
		return "review", nil
	}).
	AddEdge("review", "write")

app, err := g.Compile()
out, err := app.Invoke(ctx, graph.State{"topic": "Go generics"})
fmt.Println(app.Mermaid()) // Mermaid diagram of the graph
```

Steps:

1. **Schema**: declares the channels and their reducers.
2. **Nodes**: `AddNode(name, func(ctx, State) (any, error))`.
3. **Edges**: `AddEdge(from, to)` (use `graph.Start` and `graph.End`), `AddConditionalEdges(from, router)`.
4. **Compile**: validates and returns a `*graph.Compiled`, which offers `Invoke`, `Stream`, `StreamEvents`, `GetState`, `GetStateHistory`, `UpdateState`, `Mermaid` and `Nodes`.

Shortcuts: `SetEntryPoint(n)` is equivalent to `AddEdge(Start, n)`; `SetFinishPoint(n)` to `AddEdge(n, End)`; `AddSequence(names, funcs)` chains nodes in order.

## 7.2 Nodes

Signature: `func(ctx context.Context, s graph.State) (any, error)`.

- The node receives a **copy** of the state; changing it does not affect the graph.
- Return only what changed, as `graph.State{...}`. Updates go through the schema's reducers.
- Accepted return values: `nil`, `graph.State`, `graph.Command`, `[]graph.Send`.
- A returned error aborts the execution (use `graph.WithRetry` to try again).

### Node options

```go
g.AddNode("fetch", fetchFn,
	graph.WithRetry(runnables.RetryOptions{MaxAttempts: 3}),
	graph.WithCachePolicy(graph.CachePolicy{TTL: time.Minute}),
	graph.Deferred(),
)
app, _ := g.Compile(graph.WithCache(graph.NewMemoryCache()))
```

| Option | Effect |
|---|---|
| `graph.WithRetry(opts)` | Repeats the node with backoff when it fails |
| `graph.WithCachePolicy(p)` | Memoizes the result by TTL (requires `Compile(graph.WithCache(...))`) |
| `graph.Deferred()` | The node runs only when there are no other pending tasks (useful for consolidation) |

## 7.3 Reducers and channels

If two writes to the same key happen in the same step, the reducer decides the result. Without a reducer, the last value wins and simultaneous writes may conflict. To accumulate results from parallel branches, use `Append()`, `Sum()`, `Messages()` or a reducer of your own:

```go
maxCh := channels.BinOp(func(cur, upd any) (any, error) {
	if upd.(int) > cur.(int) {
		return upd, nil
	}
	return cur, nil
}, func() any { return 0 })

schema := graph.Schema{"best": maxCh}
```

`channels.Overwrite{Value: v}` forces the replacement, ignoring the reducer. Other channels: `AnyValue`, `Topic(accumulate)`, `NamedBarrier(names...)`, `Ephemeral(guard)`.

### Typed state with a struct (`graph.NewTyped`)

If you prefer a struct to `map[string]any`, `graph.NewTyped[T]()` builds the `Schema` from the `graph:"key[,reducer=name]"` tags of `T`. The low-level API stays the same; the typed one is just a layer on top (`langgraph/graph/typed.go`):

```go
type OrderState struct {
	Messages []messages.Message `graph:"messages,reducer=messages"`
	Total    int                `graph:"total,reducer=sum"`
	Log      []string           `graph:"log,reducer=append"`
	Approved bool               `graph:"approved"`
	Items    int                `graph:"items"`
}

g := graph.NewTyped[OrderState]().
	AddNode("price", func(ctx context.Context, s OrderState) (graph.Update, error) {
		return graph.Update{"total": s.Items * 10, "log": []string{"priced"}}, nil
	}).
	SetEntryPoint("price").SetFinishPoint("price")

c, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
out, interrupts, err := c.Invoke(ctx, OrderState{Items: 3}) // out is OrderState
out, _, err = c.Invoke(ctx, graph.Command{Resume: true})    // resume the same thread
```

Rules:

- Only exported fields with a tag enter the state. Untagged fields are ignored; `graph:"-"` ignores them explicitly.
- Reducers: `last` (default), `messages` (add_messages), `append` (concatenates the elements of each update; slice field), `sum` (numeric field). An unknown reducer, a duplicate key, an unexported field or a type incompatible with the reducer are `Compile` errors.
- A typed node receives `T` and returns a `graph.Update` (partial map). A struct cannot say "this field was not updated", because the zero value is also valid; that is why the node names the keys it writes. A key that is not a tag of `T` fails the node.
- `Invoke(ctx, in)` accepts `T` (writes all tagged fields, zeros included) or `graph.Command` (resume or partial update). It returns `(T, []graph.InterruptInfo, error)`.
- `Stream(ctx, in)` returns `iter.Seq2[T, error]`. For full events (`StreamEvents`), `GetState` and the `Saver`, use `c.Unwrap()`, which returns the `*graph.Compiled`.
- If `T` (or `*T`) has `Validate() error`, it runs on the input of each node; a returned error fails the execution.
- Struct field types are registered in `checkpoint.Register` with the name `package.Type` when `NewTyped` runs, so they survive `checkpoint.Strict(true)`. Anonymous structs cannot be registered.
- Edges, conditional edges (the router receives `T`) and `AddJoin` exist in the typed builder; anything missing is available in `g.Graph()`.

Complete offline example: `go run ./examples/langgraph/typed_state`.

## 7.4 Conditional routing

The router returns the name of the next node, `graph.End`, or a list of `graph.Send`:

```go
AddConditionalEdges("classify", func(ctx context.Context, s graph.State) (any, error) {
	switch s["kind"] {
	case "billing":
		return "billing_agent", nil
	case "tech":
		return "tech_agent", nil
	}
	return graph.End, nil
})
```

With an optional map, the router returns labels and the map translates them into nodes: `AddConditionalEdges("classify", router, map[string]string{"b": "billing_agent", "t": "tech_agent"})`.

## 7.5 Parallelism and joins

```go
AddEdge("plan", "search_web").
AddEdge("plan", "search_db").          // search_web and search_db run in the same super-step
AddJoin([]string{"search_web", "search_db"}, "merge") // merge only runs when both finish
```

Without `AddJoin`, the target node would run as soon as *any* branch finishes. Use a reducer (`Append`) on the channel where both branches write.

## 7.6 Dynamic fan-out with `Send` (map-reduce)

```go
AddConditionalEdges("plan", func(ctx context.Context, s graph.State) (any, error) {
	var sends []graph.Send
	for _, topic := range s["topics"].([]string) {
		sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"topic": topic}})
	}
	return sends, nil
})
```

Each `Send` creates a `worker` task with the `Arg` as its input state, in parallel. The results return to the main state through the reducers (for example `graph.Append()` on `"results"`). Examples: `examples/langgraph/map_reduce`, `llm_compiler`.

## 7.7 `Command`: update and redirect in the same return

```go
return graph.Command{
	Update: graph.State{"status": "approved"},
	Goto:   []string{"publish"},
}, nil
```

Also used to resume paused executions: `app.Invoke(ctx, graph.Command{Resume: value})` (chapter 8).

## 7.8 Subgraphs

A compiled graph can be called inside a node:

```go
inner, _ := innerGraph.Compile()
outer := graph.NewStateGraph(schema).
	AddNode("team", func(ctx context.Context, s graph.State) (any, error) {
		return inner.Invoke(ctx, graph.State{"messages": s["messages"]})
	})
```

An inner `Interrupt` propagates to the parent graph and resumes correctly, with or without its own checkpointer in the subgraph (its checkpoint lives in the namespace `node:index`). If several nodes of the subgraph stop in the same super-step, the first pause (in task order) bubbles up to the parent, and each `Command{Resume}` answers the next one, always in the same order. To receive the subgraph's events in the parent's stream, use `graph.Subgraphs()`; each event comes with `Event.Namespace`. Examples: `subgraph`, `hierarchical_teams`.

## 7.9 Step limit

The default is 25 super-steps. When exceeded, `Invoke` returns `*graph.GraphRecursionError`. Adjust per execution:

```go
ctx = runnables.WithConfig(ctx, runnables.Config{RecursionLimit: 50})
```

Legitimate loops (agents that use many tools) may need a higher limit; infinite loops will be cut off.

## 7.10 Memory across threads (store)

```go
app, _ := g.Compile(graph.WithStore(store.NewMemory()))

// inside a node:
st := graph.StoreFrom(ctx)
st.Put(ctx, []string{"users", "ana"}, "prefs", map[string]any{"language": "pt-BR"})
item, _ := st.Get(ctx, []string{"users", "ana"}, "prefs")
```

The `store.Store` keeps **long-term** memory shared across threads, with namespaces (`[]string`). Chapter 8.

## 7.11 Prebuilt: ToolNode and minimal ReAct agent

```go
agent, err := prebuilt.CreateReactAgent(prebuilt.ReactOptions{
	Model: m, Tools: []tools.Tool{search}, Prompt: "Be objective.",
})
```

`prebuilt.NewToolNode(tools...)` runs the `ToolCalls` of the last AI message in parallel, and `prebuilt.ToolsCondition` is the default router "has a tool call? go to tools; otherwise, end". For a complete agent (middleware, limits, HITL), use `agents.Create` (chapter 9). The `react_from_scratch` example builds the loop by hand.

## 7.12 Functional API: `Entrypoint` and `Task`

Write the flow as ordinary Go code. Tasks are memoized in the checkpoint, so a resume does not repeat work already done.

```go
wf := functional.New("report", saver, func(ctx context.Context, topic string) (string, error) {
	data, err := functional.Task(ctx, "collect", func(ctx context.Context) (string, error) {
		return collect(topic) // side effects live inside tasks
	}).Result()
	if err != nil {
		return "", err
	}
	ok := graph.Interrupt(ctx, "Publish?").(string) // human-in-the-loop
	if ok != "yes" {
		return "discarded", nil
	}
	return publish(data)
})

ctx := runnables.WithConfig(ctx, graph.Thread("wf-1"))
r, _ := wf.Invoke(ctx, "batteries") // r.Interrupt != nil: waiting
r, _ = wf.Resume(ctx, "yes")        // "collect" does NOT run again
```

When to use it: linear flows with few branches and a lot of business logic, where drawing nodes and edges would be ceremony. Use `StateGraph` when you want to visualize the flow (Mermaid), parallelize branches or have several entry points.

## 7.13 Ready-made patterns in the examples

| Pattern | Example |
|---|---|
| ReAct (model/tools loop) | `react_from_scratch`, `react_agent_middleware` |
| Plan-and-execute, ReWOO, LLM Compiler | `plan_and_execute`, `rewoo`, `llm_compiler` |
| Reflection (generator + critic) | `reflection`, `code_assistant` |
| Agent supervisor | `multi_agent_supervisor`, `hierarchical_teams` |
| Corrective/adaptive RAG | `corrective_rag`, `self_rag`, `adaptive_rag` |
| Map-reduce | `map_reduce` |

---

Previous: [6. RAG](06-rag.md) · Next: [8. Persistence, HITL and streaming](08-persistencia-hitl-streaming.md) · [Manual index](README.md)
