# compress_prompt_caveman

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Caveman-compresses the old turns of a long history on the per-call copy before each model call, using `middleware.NewCompression`.

## What it does

- A 12-turn history (Human and AI, with a Go code block in each AI turn) goes into `agents.Create`.
- `middleware.NewCompression(decider)` wraps the decider in `compress.NewGuarded`. The scripted decider says yes: compress the old turns at `full`, and ask for a terse reply.
- The last 4 turns are kept as they are. Older turns lose filler and articles; code fences, inline code, paths, URLs and numbers stay byte-identical.
- A small `sniff` middleware placed after compression prints what the model actually received.
- Token estimates (`messages.ApproxTokens`) are printed before and after. The stored history is never rewritten.

## How to apply it

```go
agent, _ := agents.Create(agents.Options{
    Model:      model,
    Middleware: []agents.Middleware{middleware.NewCompression(decider)},
})
```

Put it before summarization middleware; set the summarization `MaxTokens` above the decider's threshold so it acts only as a backstop. Use `compress.RuleDecider{}` when you have no model to decide.

## Run it

```bash
go run ./examples/langgraph/compress_prompt_caveman
```

With no API key the example uses a scripted fake model, so it runs offline. With `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` set, `examples/internal/demo` uses that provider for the reply, and the printed compression is the same.

## Test it in production mode

1. Replace the scripted decider with `compress.HTTPDecider` or a small model; keep `Floor` at `0.7`, so a weak answer falls back to rules.
2. Check that tool calls and tool results are not changed (they never are, but confirm with your own tool messages).
3. Compare answer quality with and without compression on your real conversations before you use `ultra`.
4. Watch the token counts in your traces; the savings depend on how much of the history is prose.

## See also

- [Compression package](../../../compress/)
- [Example: compress_shell_rtk](../compress_shell_rtk/README.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
