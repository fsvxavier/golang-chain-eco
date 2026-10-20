# 16. Long-term memory (LangMem)

**English** | [Português](../../pt-BR/manual/16-memoria-langmem.md) | [Español](../../es-ES/manual/16-memoria-langmem.md)

The `langmem` package is the port of [LangMem](https://github.com/langchain-ai/langmem): it makes the agent **remember across conversations**, learn from feedback and fit long contexts. This chapter is the tutorial; the full reference, the Python → Go map and the differences are in [docs/en-US/LANGMEM.md](../LANGMEM.md).

Prerequisites: chapters 5 (tools), 8 (store) and 9 (agents and middleware).

## 16.1 Choose the type of memory

| You want the agent to… | Type | Use |
|---|---|---|
| remember the user's facts and preferences | semantic (collection) | `StoreManager` or the `ManageMemory`/`SearchMemory` tools |
| keep a structured, up-to-date record of the user | semantic (profile) | `StoreManager` with `WithInserts(false)` + `WithDefault` |
| reuse "how I solved this before" | episodic | `StoreManager` + `EpisodeSchema` |
| improve its own system prompt with feedback | procedural | `PromptOptimizer` |
| not blow up the context in a long conversation | short term | `SummarizeMessages` / `SummarizationMiddleware` |

And *when* to form the memory:

- **Hot path**: the agent decides during the conversation (tools). Immediate, but it costs latency and one more decision per turn.
- **Background**: an extractor reflects after the conversation cools down (`ReflectionExecutor`). No latency and higher recall; the memory only exists after the delay.

## 16.2 Prepare the store and namespace

```go
import (
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

// Any store.Store works (memory, sqlstore, redis, mongo, dynamodb).
// IndexedStore adds semantic search; in production, swap embeddings.Fake for a real embedder.
st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})

ns := langmem.Namespace{"memories", "{langgraph_user_id}"} // this is langmem.DefaultNamespace
ctx := langmem.WithConfigurable(ctx, "langgraph_user_id", "alice")
```

The namespace isolates users: `alice` never sees what `bob` wrote. If the variable is not in `Configurable`, the call fails with `langmem.ErrConfiguration` instead of writing to the wrong place.

For the `StoreManager` and the tools to work **without** passing the store, compile the graph with it (`Compile(graph.WithStore(st))`); the package uses `graph.StoreFrom(ctx)`.

## 16.3 Hot path: the agent manages its own memory

```go
cfg := langmem.ToolConfig{Namespace: ns}
agent, _ := agents.Create(agents.Options{
	Model:   model,
	Tools:   []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
	Compile: []graph.CompileOption{graph.WithStore(st)},
})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Remember that I hate coriander"})
```

`manage_memory` creates/updates/deletes (the model gets the `id` from `search_memory`); `search_memory` ranks by similarity. Restrict actions with `ToolConfig{Actions: []string{"create","update"}}` and customize when-to-use text with `Instructions`. Example: `go run ./examples/langmem/hot_path`.

A shortcut that injects memories into the system prompt and exposes the tools all at once:

```go
mw := langmem.Middleware{Store: st, Inject: true, ExposeTools: true}
agent, _ := agents.Create(agents.Options{Model: model, Middleware: []agents.Middleware{mw}})
```

## 16.4 Background: extract after the conversation

```go
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st))

// direct (blocks):
puts, err := sm.Process(ctx, msgs)

// in background, with debounce:
ex := &langmem.ReflectionExecutor{Manager: sm}
fut, _ := ex.Submit(ctx, langmem.Payload{Messages: msgs},
	langmem.SubmitOptions{After: 30 * time.Minute, ThreadID: "t1"})
```

Three quick messages in the same thread produce **one** extraction, with the whole conversation. On shutdown, call `ex.Shutdown(true, false)` so pending memories are not lost. To wire everything into the agent: `Middleware{Manager: sm, Executor: ex, Delay: time.Minute}`. Example: `go run ./examples/langmem/background`.

What the `StoreManager` does on each pass: retrieves related memories → asks the model to insert/update/delete → writes only what changed. Useful tweaks: `WithQueryLimit(n)`, `WithQueryModel(m)` (an LLM writes the searches), `WithPhases(...)` (an extra consolidation pass), `Payload{MaxSteps: 3}` (refinement over several rounds).

## 16.5 Profile and episodes

A **profile** is a single document with fixed fields, refined after each conversation:

```go
type UserProfile struct {
	Name          string   `json:"name"`
	PreferredName string   `json:"preferred_name"`
	Skills        []string `json:"skills"`
}
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st),
	langmem.WithSchemas(langmem.SchemaFor[UserProfile]("UserProfile", "Save the user's profile")),
	langmem.WithInserts(false), langmem.WithDeletes(false),
	langmem.WithDefault(langmem.Seed{Content: UserProfile{}}))
// ...
it, _ := sm.Get(ctx, langmem.DefaultKey)
p, _ := langmem.Decode[UserProfile](it.Value["content"])
```

**Episodes** store "situation → reasoning → action → result" for future few-shot use: `WithSchemas(langmem.EpisodeSchema)` with instructions such as "extract examples of successful interactions". Types can be mixed in the same manager: `WithSchemas(langmem.MemorySchema, langmem.EpisodeSchema)`. Example: `go run ./examples/langmem/profile_episodic`.

Use `Manager` (without a store) when you only want the extraction in memory: `NewManager(model).Extract(ctx, msgs, existing, 1)`.

## 16.6 Retrieving memories

| Need | Call |
|---|---|
| by key | `sm.Get(ctx, key)` |
| by similarity | `sm.Search(ctx, "question", nil, 5)` or `langmem.Query(ctx, st, ns, q, nil, 5)` |
| the model decides what to search | `langmem.MemorySearcher{Model: m, Store: st}.Search(ctx, msgs)` |
| in the prompt, automatic | `Middleware{Inject: true}` |
| summary of a whole conversation | `langmem.ExtractThread[langmem.ThreadSummary](ctx, model, msgs, "")` |

## 16.7 Procedural memory: the prompt that learns

```go
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 3})
updated, err := opt.Optimize(ctx, []langmem.Trajectory{{
	Messages: conversation,
	Feedback: map[string]any{"score": 0, "comment": "wanted code, got theory"},
}}, langmem.Prompt{Name: "main", Prompt: "You are a helpful assistant."})
```

- `PromptMemory`: 1 call, cheap. `Metaprompt`: reflects and rewrites. `Gradient`: separates diagnosis from rewriting (the most careful, the most expensive).
- With no evidence of failure the prompt comes back **unchanged**. The `{x}` variables of the original prompt are preserved (if the model drops them, you get an error).
- Several cooperating prompts: `MultiPromptOptimizer` figures out which ones caused the failure (`Prompt.WhenToUpdate` helps) and rewrites only those. Example: `go run ./examples/langmem/prompt_optimizer`.

Recommended flow: collect conversations with feedback (score, comment, human edit) → optimize in batch, offline → review the diff → publish the new prompt. Do not optimize on the request path.

## 16.8 Short-term memory: incremental summary

```go
node := langmem.SummarizationNode{SummarizeOptions: langmem.SummarizeOptions{
	Model: cheapModel, MaxTokens: 4000, MaxSummaryTokens: 256,
	// Counter: your real token counter (default: ~4 characters/token approximation)
}}
schema := langmem.SummarizationSchema()   // "context" and "summarized_messages" channels
schema["messages"] = graph.Messages()
g := graph.NewStateGraph(schema).AddNode("summarize", node.Node()) // ... before the model node
```

The node writes the view ready for the model into `summarized_messages` (the full history stays in `messages`) and the `RunningSummary` into `context`, persisted by the checkpointer, so each summary extends the previous one instead of starting over. Inside `agents.Create`, use `Middleware: []agents.Middleware{langmem.SummarizationMiddleware{...}}`. Example: `go run ./examples/langmem/summarization`.

To really limit the size of the summary, pass a model with a fixed `max_tokens` (`models.Bind(m, models.WithMaxTokens(256))`): `MaxSummaryTokens` only reserves budget.

## 16.9 Testing without network

Script the model with `models.Fake`. For the extractor, return *tool calls* with the schema name (`"Memory"`, `"Episode"`, yours) and, to update, `json_doc_id`:

```go
fake := models.NewFake(messages.AI("", messages.ToolCall{
	Name: "Memory", Args: map[string]any{"content": "likes sushi"}}))
sm := langmem.NewStoreManager(fake, nil, langmem.WithStore(store.NewMemory()))
puts, _ := sm.Process(ctx, msgs)
```

`embeddings.Fake{}` gives similarity by shared words, enough to test ranking. The package tests (`langmem/langmem_test.go`) are good models.

## 16.10 Production and cautions

- **Privacy**: memories are personal data. Namespace per user/organization, a retention policy and a deletion path (`sm.Delete`, `ManageMemory` with `delete`). Consider the `PII` middleware (chap. 9) before extracting.
- **Prompt injection**: text the user (or a web page) puts in the conversation may become a memory and later be injected into the prompt. Treat memories as **untrusted** input (`Inject` places them inside `<memories>`), and restrict `Actions` when the agent does not need to delete.
- **Cost**: each pass is ≥ 1 LLM call; use `Executor` with a delay, a low `WithQueryLimit` and a smaller model for extraction.
- **Search scale**: `IndexedStore` scans the prefix (O(n) per search). For many memories per user, index them in a dedicated vector store (chap. 6/13) and implement `SemanticSearcher` on top of it.
- **Consistency**: two simultaneous extractors in the same namespace may overwrite the same item (last one wins); serialize per user/thread (the executor's debounce already helps).
- **Observability**: the calls go through the `core/callbacks` callbacks like any model/tool (chap. 12).

---

Previous: [15. Examples and recipes](15-exemplos-e-receitas.md) · Next: [17. Agent Protocol](17-agent-protocol.md) · [Manual index](README.md)
