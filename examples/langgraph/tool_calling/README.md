# tool_calling

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Bind tools to a model, inspect the tool calls it makes and run them concurrently with `ToolNode`.

## What it does

- Three tools (`add`, `multiply`, `divide`) bound with `models.BindTools`.
- The scripted model requests four tool calls at once, including `divide` by zero and an unknown tool (`sqrt`).
- `prebuilt.NewToolNode` runs them concurrently; the example prints each result and status, showing that errors and unknown tools come back as error messages instead of crashing.

## How to apply it

```go
model := models.BindTools(chatModel, tools...)
ai, _ := model.Generate(ctx, msgs)                // ai.ToolCalls
out, _ := prebuilt.NewToolNode(tools...).Node()(ctx, graph.State{"messages": []messages.Message{ai}})
```

Return `tools.Errorf(...)` for expected failures so the model can recover. Tool-call ids must be unique within a conversation.

## Run it

```bash
go run ./examples/langgraph/tool_calling
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real model and check that it produces valid arguments for your schemas; improve the `description` tags where it does not.
2. Set a timeout per tool and a concurrency limit if tools hit rate-limited APIs.
3. Never put secrets in tool results; they go back to the model and into traces.
4. Test the unknown-tool and invalid-arguments paths; they happen in production.

## See also

- [Manual, chapter 5: Tools](../../../docs/en-US/manual/05-tools.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
