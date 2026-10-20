# chain

The smallest chain: a chat prompt, a model and an output parser piped together (the LCEL equivalent).

## What it does

- Builds a two-message chat prompt (`system` + `human`) with a `{topic}` variable.
- Pipes `prompt | model | parser` with `runnables.Pipe3`, typed end to end: `prompts.Values` in, `string` out.
- Invokes it once with `topic = "goroutines"` and prints the one-sentence answer.

## How to apply it

Use this shape for any single-step LLM call that must stay testable and typed.

```go
prompt := prompts.Chat([2]string{"system", "..."}, [2]string{"human", "Explain {topic}."})
chain  := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
    prompt, models.AsRunnable(model), outputparsers.Str{})
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})
```

- Swap `outputparsers.Str{}` for a JSON or structured parser when the caller needs data, not text.
- Because every stage is a `Runnable`, you can add `WithRetry`, callbacks or a tracer to the whole chain without touching the stages.

## Run it

```bash
go run ./examples/langchain/chain
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Export a provider key and run the example; the answer should be one sentence about goroutines.
2. Wrap the call in `context.WithTimeout` and add `runnables.RetryOptions` so a slow or rate-limited provider does not hang the caller.
3. Attach a tracer (LangSmith, Langfuse or OTLP; see chapter 12) and confirm one trace per `Invoke` with token usage.
4. Put `models.NewRateLimiter` in front of the provider if several requests share one key.
5. Keep the prompt text in code review like any other source: it is behavior.

## See also

- [Manual, chapter 4: Prompts, chains and parsers](../../../docs/en-US/manual/04-chains-prompts-parsers.md)
- [Manual, chapter 3: Models and providers](../../../docs/en-US/manual/03-modelos-e-provedores.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
