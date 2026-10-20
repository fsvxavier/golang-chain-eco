# ralph_mode

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A loop of fresh agents with no shared conversation: progress is carried only through a file until the task is done.

## What it does

- Seeds `/progress.md` with three unchecked items.
- Each iteration creates a **new** agent and a new conversation, reads the file, completes exactly one item and checks it off.
- The loop stops when the agent answers `ALL DONE`; the example prints the number of checked items per iteration.

## How to apply it

Use it for work too long for one context window: each pass starts clean, so context never overflows, and the file is the source of truth.

```go
for i := 0; i < maxIterations; i++ {
    agent, _ := deepagents.Create(deepagents.Options{Model: model, Backend: be})
    out, _ := agent.Invoke(ctx, graph.State{"messages": "Read /progress.md and complete exactly one unchecked item."})
    if strings.Contains(lastText(out), "ALL DONE") { break }
}
```

Write the progress file so any fresh agent can resume: one item per line, status, and a pointer to the artifact.

## Run it

```bash
go run ./examples/deepagents/ralph_mode
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Use a real model and a real `backends.Filesystem` directory under version control.
2. Always cap iterations (`maxIterations`) and add a wall-clock timeout; an agent that never marks an item done would otherwise loop forever.
3. Verify "done" outside the model: run the tests or a checker after each iteration and revert the item if it fails.
4. Commit after each iteration so a bad pass can be undone with one revert.
5. Track tokens per iteration; if cost grows, the progress file is too large or too vague.

## See also

- [Manual, chapter 10: Deep Agents](../../../docs/en-US/manual/10-deep-agents.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
