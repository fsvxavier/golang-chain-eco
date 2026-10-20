# prompt_optimizer

Procedural memory: a system prompt that improves itself from conversations plus feedback, and a multi-prompt optimizer that decides which prompt caused a failure.

## What it does

- A trajectory (conversation plus feedback: the user wanted code but got theory) goes to `langmem.NewPromptOptimizer` with the `Metaprompt` strategy; it reflects and rewrites the prompt.
- `MultiPromptOptimizer` does credit assignment across several cooperating prompts (`research`, `explain`): it classifies which one is responsible and updates only that one.

## How to apply it

```go
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 2})
better, _ := opt.Optimize(ctx, trajectories, langmem.Prompt{Name: "main", Prompt: current})

mo := &langmem.MultiPromptOptimizer{Model: model, Kind: langmem.PromptMemory}
updated, _ := mo.Optimize(ctx, trajectories, prompts)
```

Collect trajectories with scores or comments from users or evals, run the optimizer offline, and **review** the new prompt before deploying it.

## Run it

```bash
go run ./examples/langmem/prompt_optimizer
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a strong model for optimization and gather real trajectories with real feedback (thumbs, ratings, eval scores).
2. Never auto-deploy: evaluate the candidate prompt on a held-out dataset (see `examples/evals/basic`) and compare with the current one.
3. Version prompts in source control and keep the trajectory that justified each change.
4. Re-run periodically, not per request; optimization is a batch job.

## See also

- [Manual, chapter 16: Long-term memory](../../../docs/en-US/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/en-US/LANGMEM.md)
- [EVALS.md](../../../docs/en-US/EVALS.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
