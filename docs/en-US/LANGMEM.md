# LangMem in Go (`langmem/`)

**English** | [Português](../pt-BR/LANGMEM.md) | [Español](../es-ES/LANGMEM.md)

Port of [LangMem](https://github.com/langchain-ai/langmem) (v0.0.30, MIT) integrated into this project's ecosystem: it uses the existing `langgraph/store`, `agents.Create` with middleware, `graph.StateGraph`, tools and chat models. Stdlib only.

Step-by-step tutorial: [manual/16-memoria-langmem.md](manual/16-memoria-langmem.md). Examples: [`examples/langmem/`](../../examples/langmem/).

## Mental model

Every LangMem memory operation follows the same pattern: **takes conversations + current memory state → asks an LLM to expand/consolidate → returns the new state.** Three questions shape your system: *what* to remember, *when* to form the memory, and *where* to store it.

| Type | Used for | Representation | API |
|---|---|---|---|
| Semantic | facts, preferences | collection of items or a **profile** (1 document) | `StoreManager`, `ManageMemory` |
| Episodic | past experiences, few-shot | collection of `Episode` | `StoreManager` + `EpisodeSchema` |
| Procedural | how the agent behaves | the system prompt itself | `PromptOptimizer`, `MultiPromptOptimizer` |
| Short term | fitting into context | incremental summary | `SummarizeMessages`, `SummarizationNode` |

| When to form | Latency | How |
|---|---|---|
| **Hot path** (the agent decides, during the conversation) | higher | `ManageMemory` + `SearchMemory` tools |
| **Background** (after the conversation, with debounce) | none | `StoreManager` + `ReflectionExecutor` |

## Python → Go map

| LangMem (Python) | Go (`langmem`) | Note |
|---|---|---|
| `create_memory_manager` | `NewManager(model, opts...)` → `Extract(ctx, msgs, existing, maxSteps)` | pure function, no store |
| `create_memory_store_manager` | `NewStoreManager(model, namespace, opts...)` → `Process` / `Invoke` | persists only what changed |
| `create_manage_memory_tool` | `ManageMemory(ToolConfig)` | `Actions`, `Schema`, `Instructions`, `Name` |
| `create_search_memory_tool` | `SearchMemory(ToolConfig)` | `Artifact: true` = `content_and_artifact` |
| `create_memory_searcher` | `MemorySearcher{...}.Search` | the LLM writes the queries |
| `create_thread_extractor` | `ExtractThread[T]` | generic schema (`ThreadSummary` default) |
| `create_prompt_optimizer(kind=...)` | `NewPromptOptimizer(model, kind, cfg)` | `Gradient`, `Metaprompt`, `PromptMemory` |
| `create_multi_prompt_optimizer` | `MultiPromptOptimizer` | classifier + concurrent optimization |
| `Prompt` (TypedDict) | `Prompt{Name, Prompt, UpdateInstructions, WhenToUpdate}` | |
| `AnnotatedTrajectory` | `Trajectory{Messages, Feedback}` | feedback: string, map or nil |
| `summarize_messages` | `SummarizeMessages` | algorithm ported faithfully |
| `SummarizationNode` | `SummarizationNode{...}.Node()` + `SummarizationSchema()` | |
| `RunningSummary` | `RunningSummary` | |
| `ReflectionExecutor` (local) | `ReflectionExecutor` | `Submit` → `*Future`; `Shutdown` |
| `NamespaceTemplate` | `Namespace.Resolve(ctx)` | variables come from `Configurable` |
| `InMemoryStore(index=...)` | `NewIndexedStore(store, embedder)` | see "Differences" |
| `pydantic` schemas | `SchemaFor[T](name, desc)` | JSON Schema by struct reflection |
| — (recipe from the guides) | `Middleware` | injects memories + tools + background formation into `agents.Create` |

## API in detail

### Schemas and memories

`Schema{Name, Description, Parameters}` describes a *kind* of memory; each schema becomes a **tool** offered to the model (as in LangMem, where Pydantic models become trustcall tools). A manager can mix kinds.

- `MemorySchema` (default): free text, stored as `{"content": "..."}`.
- `EpisodeSchema` (`Episode{Observation, Thoughts, Action, Result}`), `TripleSchema` (`Triple{Subject, Predicate, Object, Context}`).
- `SchemaFor[T]("Name", "description")` for your own: `SchemaFor[UserProfile]("UserProfile", "Save the user's preferences")`.

`Extracted{ID, Kind, Content, Deleted}` is the memory that goes into and comes out of the `Manager`. `Decode[T](content)` converts to the struct; `Text(content)` extracts the text of a note; `Note(id, text)` and `ExistingNotes(texts...)` build entries.

### Manager

```go
m := langmem.NewManager(model,
    langmem.WithSchemas(langmem.MemorySchema, langmem.EpisodeSchema),
    langmem.WithInstructions("..."),     // default: langmem.DefaultInstructions
    langmem.WithInserts(true), langmem.WithUpdates(true), langmem.WithDeletes(false))
out, err := m.Extract(ctx, msgs, existing, 3) // maxSteps: up to 3 rounds
```

Protocol with the model (same as LangMem): one tool call per new memory; repeating `json_doc_id` updates; `RemoveDoc` deletes; from the 2nd round onward there is a `Done` tool. Between rounds the model receives "Memory X inserted/updated." and the current list. `Extract` returns the full list (existing memories changed or not, new ones, and deleted ones with `Deleted: true`). Content that does not validate against the schema is discarded and the error goes back to the model as the tool result.

Defaults: inserts and updates **on**, deletes **off** in `Manager`; deletes **on** in `StoreManager` (as in LangMem).

### StoreManager

```go
sm := langmem.NewStoreManager(model, langmem.Namespace{"memories", "{langgraph_user_id}"},
    langmem.WithStore(st),            // or compile the graph with graph.WithStore
    langmem.WithQueryLimit(5),
    langmem.WithQueryModel(qm),       // the LLM writes the searches (otherwise: widened windows of the conversation)
    langmem.WithDefault(langmem.Seed{Content: UserProfile{}}), // seed → profile
    langmem.WithPhases(langmem.Phase{IncludeMessages: false}))  // extra consolidation
puts, err := sm.Invoke(ctx, langmem.Payload{Messages: msgs, MaxSteps: 2})
```

`Invoke` flow: resolve namespace → **retrieve** relevant memories (windows of the last 1, 2, 4… messages; or the queries from `QueryModel`; fill up with unranked memories until `QueryLimit`) → seed `default` if empty → `Extract` → phases → write **only what changed** → delete what was removed. Items are stored as `{"kind": <schema>, "content": <content>}`. There are also `Search`, `Get`, `Put`, `Delete` on the resolved namespace.

**Profile** = a single document refined in place: `WithInserts(false)`, `WithDeletes(false)`, `WithDefault(...)`; the key is `langmem.DefaultKey` (`"default"`). **Collection** = the defaults.

### Tools (hot path)

- `manage_memory`: `{content, action: create|update|delete, id}`. `create` with `id` is an error; `update`/`delete` without `id` is an error; ids are UUIDs; stores `{"content": ...}`.
- `search_memory`: `{query, limit=10, offset=0, filter}`; returns JSON `[{namespace,key,value,created_at,updated_at,score}]`.
- Errors become messages for the model (`tools.Error`), except missing store/configuration (`ErrConfiguration`).

### Namespaces and store

`Namespace{"memories", "{langgraph_user_id}"}` (the default, `DefaultNamespace`) resolves `{variables}` from `runnables.Config.Configurable` — use `langmem.WithConfigurable(ctx, "langgraph_user_id", "u1")` or `Config{Configurable: ...}`. A missing variable = `ErrConfiguration` error listing the available keys (it never writes to the wrong namespace).

`store.Store` has no semantic search; `IndexedStore` adds it on top of **any** backend (memory, sqlstore, redis, mongo, dynamodb): the vector goes inside the item (`_embedding`, hidden in results) and the search is a cosine scan of the prefix. `Query(ctx, store, prefix, query, filter, limit)` uses semantic search if the store implements `SemanticSearcher`, otherwise ranking by shared words, and lists everything with an empty query.

### Prompt optimization (procedural memory)

| Kind | LLM calls | How |
|---|---|---|
| `PromptMemory` | 1 | asks for `GeneralResponse{logic, update_prompt, new_prompt}` |
| `Metaprompt` | 1 to `MaxReflectionSteps` | `think`/`critique` loop and, at the end, `OptimizedPromptOutput` |
| `Gradient` | 2 to `MaxReflectionSteps`+1 | loop until `recommend{warrants_adjustment, hypotheses, full_recommendations}`; if `false`, returns the prompt unchanged; otherwise one rewrite call |

Defaults: `MaxReflectionSteps` 5, `MinReflectionSteps` 1. Guarantees: no trajectories → prompt unchanged, **zero** calls; the `{x}` variables of the original prompt **must** survive (otherwise error); stray braces become `{{ }}`; "No recommendations" keeps the original. The `MultiPromptOptimizer` asks a classifier which prompts (validated names) caused the failure, using `WhenToUpdate`, and optimizes only those, in parallel.

### Summarization (short-term memory)

`SummarizeMessages(ctx, msgs, running, opts)` — the LangMem algorithm: when `MaxTokensBeforeSummary` (default `MaxTokens`) is accumulated, it summarizes from the start up to that point, as long as the rest fits in `MaxTokens − MaxSummaryTokens − system`; tool results from a summarized call are carried along; input larger than the summarizer's window is cut (starting at a human message). It returns `[system?, "Summary of the conversation so far: …", recent…]` and a `RunningSummary` (summarized ids + last id) so the next call is incremental. Every message needs an ID; `MaxSummaryTokens` must be smaller than `MaxTokens`.

`SummarizationNode` (graph node): reads `messages`, writes `summarized_messages` and `context.running_summary`; with `InputKey == OutputKey` it overwrites the list (`RemoveAll`). `SummarizationMiddleware`: the same inside `agents.Create` — the model sees the summarized view, the stored history stays complete, and the summary is incremental (unlike `agents/middleware.Summarization`, which rewrites the history).

### Background

```go
ex := &langmem.ReflectionExecutor{Manager: sm}
fut, _ := ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 30 * time.Minute})
...
ex.Shutdown(true, false) // runs pending jobs now and waits
```

The same `thread_id` (from `Configurable` or `SubmitOptions.ThreadID`) cancels the pending job (`*Future` resolves with `ErrCancelled`) and reschedules with the larger conversation. The job inherits the context *values*, not its cancellation.

### Middleware

```go
langmem.Middleware{Store: st, Namespace: ns,
    Inject: true, Limit: 5,       // relevant memories in the system prompt on every call
    ExposeTools: true,            // manage_memory + search_memory
    Manager: sm, Executor: ex, Delay: time.Minute} // formation at the end of the run (in background if Executor)
```

## Differences from Python LangMem

Deliberate or due to limitation — no sugarcoating:

- **Replacement updates by default; patch with `WithPatchUpdates()`.** Without the option, the model resends the full content with `json_doc_id` (works with any model with tool calling; spends more tokens on large documents). With `langmem.WithPatchUpdates()`, the tool `PatchDoc{json_doc_id, planned_edits, patches[]}` applies RFC 6902 operations to the content (RFC 6901 pointers, `internal/jsonpatch` package), like LangMem's `trustcall`. A patch that fails goes back to the model as the tool result for a retry; if it fails again, the model resends the full content with `json_doc_id` (replacement).
- **Remote `ReflectionExecutor`**: `langmem.NewRemoteReflectionExecutor(client, graphID)` runs the memory graph on the server (`remote/server`) with `after_seconds` and cancellation by `rollback` on the same thread. Differences: an already started run is also rolled back by a newer submit; the thread is required; `Future.Wait` does not return `Put`s (the writes happen on the server); the store API (`Search`) was not ported, so search remains local.
- **Semantic search outside `store.Store`.** The project's `Store` is CRUD + equality filter. By default `IndexedStore` does a cosine scan (O(n) per search). With `ix.Index = vectorstore.NewHNSW()` the search becomes approximate (HNSW in pure Go; recall@10 measured in tests). `Put` and `Delete` keep the index up to date; `Reindex` indexes items written before. Limits: the index is in memory and is not saved together with the store; writes made directly to the underlying store do not enter it. Filters: `$eq $ne $gt $gte $lt $lte $in` over value fields. The operators are evaluated in Go: the search asks the backend for k×4 items, filters, and falls back to a full scan if results are missing. For large scale, use a dedicated vector store and adapt `SemanticSearcher`.
- **No async variants**: everything takes a `context.Context`; use goroutines.
- **Not ported:** `langmem.graphs.*` (hosted LangGraph Platform service graphs), `prompts/_layers.MemoryLayer` (internal API, "not public" upstream) and `graph_rag.py` (commented out upstream).
- **Metaprompt:** if the model already returns the final prompt in an intermediate round, Go accepts it; Python ignores it and continues the loop.
- **Retrieval:** it fills up the memories seen by the extractor with unranked items until `QueryLimit`, so that a lexical-only store does not make the model duplicate memories.
- **Memory IDs:** the store's own key (128-bit hex), not a `uuid5` derived from the namespace.
- **Order of `Search` results** is by score; ties, by key.
- The prompt texts (`DefaultInstructions`, gradient, metaprompt, prompt_memory, summary) are adapted from LangMem, MIT license.

## Tests

`go test -race ./langmem/` — 100% offline (and `IndexedStore` over real SQLite in `internal/sqlitetest`; Redis/Mongo/DynamoDB were not exercised with it), with a scripted `models.Fake` (tool calls) and `embeddings.Fake`. Covers namespaces, semantic/lexical search, manager permissions, multi-step, store manager lifecycle (insert → update → delete, per-user isolation), profile with seed, phases, query model, tools, searcher, optimizers (3 kinds + multi + variable curation), incremental summary, executor debounce, and the middleware inside an `agents.Create`.
