# middleware_stack

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

An agent wrapped in the middleware you want in production: PII redaction, retry, model fallback and call limits. The agents run through `middleware.Compaction()`: old turns are caveman-compressed, a Ponytail note is added when the conversation has a real share of code, JSON tool results become TOON, all decided by rules with no model needed.

## What it does

- The primary model always fails with a 503; the backup model works.
- `PII` redacts the email before any model sees it. `ModelRetry` retries the primary twice, then `ModelFallback` switches to the backup.
- `ModelCallLimit` and `ToolCallLimit` cap how many calls one run may make.
- Prints the stored user message, the number of primary attempts and the backup's answer, which confirms the email arrived redacted.

## How to apply it

Compose `agents.Middleware` in order; the first one is the outermost.

```go
agents.Create(agents.Options{
    Model: primary, Tools: tools,
    Middleware: []agents.Middleware{
        middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail}},
        middleware.ModelRetry{Options: runnables.RetryOptions{MaxAttempts: 3}},
        middleware.ModelFallback{Fallbacks: []models.ChatModel{backup}},
        middleware.ModelCallLimit{RunLimit: 25},
        middleware.ToolCallLimit{RunLimit: 10},
    },
})
```

Order matters: redact first so retries and fallbacks never see raw PII, retry before falling back, limits last so they count real calls.

## Run it

```bash
go run ./examples/langchain/middleware_stack
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Replace both fakes with real models from two providers (for example Anthropic as primary, OpenAI as fallback).
2. Force a failure (invalid key or `httptest` returning 503 on the primary) and confirm the fallback answers and the log shows the retries.
3. Send real-looking PII (emails, card numbers) and inspect the request that reaches each provider to confirm it is redacted.
4. Set `RunLimit` from a measured p99 of calls per task, not a guess, and alert when a run hits it.
5. Add `Summarization` for long threads and an explicit `RecursionLimit` in `runnables.Config` (see the checklist).

## See also

- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Manual, chapter 14: Production, security and troubleshooting](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
