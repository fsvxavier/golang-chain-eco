# self_discover

A four-prompt pipeline: select reasoning modules, adapt them to the task, build a reasoning structure, then solve with it.

## What it does

- Four chained nodes (`select`, `adapt`, `structure`, `reason`), each a prompt built from the previous node's output stored in state.
- Solves the two-ropes puzzle (measure 45 minutes) by first choosing a reasoning approach and then following it.

## How to apply it

```go
step := func(key, tmpl string, from ...string) graph.NodeFunc { /* prompt = tmpl + values of the `from` keys */ }
AddNode("adapt", step("adapted", "ADAPT the selected modules to the task.", "selected", "task"))
```

Use it for hard reasoning tasks where a fixed pipeline beats one big prompt. Each intermediate output is inspectable, so you can see where a bad answer came from.

## Run it

```bash
go run ./examples/langgraph/self_discover
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a strong model and your own catalog of reasoning modules.
2. Validate the JSON structure from the third step before solving; retry or fail if it is malformed.
3. Compare accuracy and cost against a single "think step by step" prompt on 20-30 tasks; keep the pipeline only if it wins.
4. Log the four intermediate outputs with each answer.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
