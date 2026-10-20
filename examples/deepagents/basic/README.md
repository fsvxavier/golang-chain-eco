# basic

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

The smallest deep agent: planning tools, a virtual filesystem and subagent delegation out of the box.

## What it does

- Creates a deep agent with a system prompt and a model that writes `/notes.md` through the built-in `write_file` tool.
- The file lives in the agent's state (virtual filesystem), not on disk; the example prints it with `backends.FilesFromState`.

## How to apply it

`deepagents.Create` gives an agent `write_todos`, the file tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`) and a `task` tool for subagents with no extra wiring.

```go
agent, _ := deepagents.Create(deepagents.Options{Model: model, SystemPrompt: "You are a research assistant."})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Save a note about deep agents."})
files := backends.FilesFromState(out)
```

Start here for long tasks that need a scratchpad; add `Backend`, `SubAgents`, `Memory` and `Skills` as the task grows (the other deepagents examples).

## Run it

```bash
go run ./examples/deepagents/basic
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Run with a real key; the model now chooses whether to write the file, so check `/notes.md` exists and is sensible.
2. Choose the backend deliberately: the default state backend vanishes with the run. Use `backends.Filesystem{Root: dir}` for real files (confined to `dir`) or `backends.Remote` for an isolated runner.
3. Never expose `backends.LocalShell` to untrusted input; use `kubernetes.Sandbox` or another isolated runner.
4. Add a checkpointer and a `thread_id` so long runs survive restarts, and set an explicit recursion limit.

## See also

- [Manual, chapter 10: Deep Agents](../../../docs/en-US/manual/10-deep-agents.md)
- [Manual, chapter 14: Production, security and troubleshooting](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
