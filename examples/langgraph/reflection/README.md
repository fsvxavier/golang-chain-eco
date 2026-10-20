# reflection

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A generator and a critic loop until the critic approves the draft.

## What it does

- `generate` writes a one-sentence description; `reflect` critiques it and sends the critique back as a human message; the loop ends on `APPROVED` or when the conversation exceeds a length cap.
- The scripted first draft is too short, the second passes.

## How to apply it

```go
AddConditionalEdges("reflect", func(_ context.Context, s graph.State) (any, error) {
    if strings.Contains(demo.Last(s), "APPROVED") { return graph.End, nil }
    return "generate", nil
})
```

Use it when a second pass reliably improves the output (writing, code, summaries). Give the critic concrete criteria; a vague critic approves everything or nitpicks forever.

## Run it

```bash
go run ./examples/langgraph/reflection
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use real models; a different (often stronger) model as critic works better than self-critique.
2. Keep the length/iteration cap and measure how often the cap is hit.
3. Compare cost against quality: sample 20 tasks with and without reflection and have a human or an eval pick the better one.
4. Make the approval signal structured instead of matching a string.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
