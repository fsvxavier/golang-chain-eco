# hot_path

An agent that curates its own long-term memory with `manage_memory` and `search_memory` tools, isolated per user.

## What it does

- `langmem.NewIndexedStore` adds semantic search to a key-value store; the memory namespace is `["memories", "{langgraph_user_id}"]`, so each user has a private space.
- The scripted model saves what Alice asks it to remember, then recalls it later. Asking the same question as Bob finds nothing: memories do not leak across users.

## How to apply it

```go
st := langmem.NewIndexedStore(store.NewMemory(), embedder)
cfg := langmem.ToolConfig{Namespace: langmem.Namespace{"memories", "{langgraph_user_id}"}}
agent, _ := agents.Create(agents.Options{
    Model: model, Tools: []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
    Compile: []graph.CompileOption{graph.WithStore(st)},
})
ctx = langmem.WithConfigurable(ctx, "langgraph_user_id", userID)
```

"Hot path" means the agent decides to remember during the conversation, at the cost of a tool call in the user's latency.

## Run it

```bash
go run ./examples/langmem/hot_path
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real model and embedder and a durable store (SQL, Redis, MongoDB, DynamoDB or S3 stores all implement `store.Store`).
2. Set `langgraph_user_id` from the authenticated user, never from the request body; test that user B cannot read user A's memories.
3. Decide what the agent may store (no secrets, no sensitive categories without consent) and provide a way to list and delete a user's memories.
4. Watch the memory count per user and prune or consolidate; see `background` for forming memories off the hot path.

## See also

- [Manual, chapter 16: Long-term memory](../../../docs/en-US/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/en-US/LANGMEM.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
