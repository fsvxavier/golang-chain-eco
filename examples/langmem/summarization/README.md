# summarization

Short-term memory: a running summary keeps a long chat inside a token budget while the full history stays in state.

## What it does

- `langmem.SummarizationNode` runs before the model. When the history exceeds `MaxTokens` (60 here), older messages are replaced in the **view** by a summary (`MaxSummaryTokens: 15`); the full history stays in `messages`.
- The model node reads `summarized_messages`. The example prints the history length and the model's view length per question, showing the view stays small.

## How to apply it

```go
node := langmem.SummarizationNode{SummarizeOptions: langmem.SummarizeOptions{Model: summarizer, MaxTokens: 2000, MaxSummaryTokens: 300}}
schema := langmem.SummarizationSchema(); schema["messages"] = graph.Messages()
g.AddNode("summarize", node.Node()).AddEdge(graph.Start, "summarize").AddEdge("summarize", "call_model")
```

Use it for long conversations on one thread. Summarize with a cheaper model; keep the budget well under the model's window. For agents, the `Summarization` middleware does the same inside `agents.Create`.

## Run it

```bash
go run ./examples/langmem/summarization
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use real models and real budgets (`MaxTokens` below the model's window minus the reply and tool output).
2. Use a durable checkpointer: the running summary lives in state and must survive restarts.
3. Test facts that sit only in old messages ("What's my name?"): they must survive in the summary.
4. Watch cost: each summarization is a model call; summarize in larger steps rather than every turn.

## See also

- [Manual, chapter 16: Long-term memory](../../../docs/en-US/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/en-US/LANGMEM.md)
- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
