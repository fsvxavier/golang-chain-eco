# code_assistant

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Generate code, check it, feed the error back and retry until the check passes.

## What it does

- `generate` asks the coder model; `check` validates the result (here a Go-side stand-in that requires `func Add(` and the right behavior).
- On failure the error is added to the conversation and the graph loops back, up to 4 iterations (`graph.Sum()` counts them).
- The scripted coder fails twice (wrong name, wrong operator) and succeeds on the third attempt.

## How to apply it

Replace the stand-in `check` with a real one: write the code to a temp module, run `go build ./... && go test ./...` with a timeout, and return the compiler or test output as the error.

```go
AddConditionalEdges("check", func(_ context.Context, s graph.State) (any, error) {
    if s["error"] != "" && s["iterations"].(int) < 4 { return "generate", nil }
    return graph.End, nil
})
```

## Run it

```bash
go run ./examples/langgraph/code_assistant
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real coder model and a real checker. Run the checker in an isolated sandbox (container or `kubernetes.Sandbox`) with no network, a CPU/memory limit and a wall-clock timeout: you are executing model-written code.
2. Keep the iteration cap; log each attempt and the error it triggered.
3. Truncate long compiler output before feeding it back so the context does not fill with noise.
4. Never merge generated code without review; use this as a draft generator.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
