# rewoo

Reasoning WithOut Observation: plan every tool call up front with placeholders (`#E1`, `#E2`), execute them in order, then solve.

## What it does

- The planner emits `Plan:` / `#E1 = Search[...]` pairs in one go; a regexp parses them into steps.
- `tool` runs one step at a time, substituting earlier evidence (`#E1`) into later inputs, and merges results with a `BinOp` reducer; `solve` writes the final answer from the evidence.

## How to apply it

Compared with ReAct, the model is called twice (plan and solve) instead of once per tool call, which saves tokens when the plan does not depend on intermediate surprises.

```go
var stepRe = regexp.MustCompile(`Plan:\s*(.+)\n\s*(#E\d+)\s*=\s*(\w+)\[(.+)\]`)
```

Validate the parsed plan (known tools, variables defined before use) before running anything.

## Run it

```bash
go run ./examples/langgraph/rewoo
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use real models and real tools; model output will drift from the exact format, so tighten the prompt and reject plans that do not parse.
2. Cap the number of steps and set timeouts per tool.
3. Never substitute evidence into a shell command or query without escaping; treat tool results as untrusted.
4. Compare tokens and accuracy with a ReAct agent on the same tasks.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
