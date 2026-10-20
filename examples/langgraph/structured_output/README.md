# structured_output

An agent that finishes with a typed, validated answer instead of free text.

## What it does

- `ResponseFormat` carries a JSON Schema generated from the Go struct `WeatherReport` with `jsonschema.For[T]()`.
- After the tool call, the agent returns JSON that is parsed into `state["structured_response"]` and printed as a typed value.

## How to apply it

```go
agents.Create(agents.Options{
    Model: model, Tools: tools,
    ResponseFormat: &models.ResponseFormat{Name: "WeatherReport", Schema: jsonschema.For[WeatherReport]().Map()},
})
// out["structured_response"] holds the parsed object
```

Use it whenever another program, not a person, consumes the answer. Keep the struct tags (`json`, `description`) accurate: they are the instructions the model sees.

## Run it

```bash
go run ./examples/langgraph/structured_output
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a provider that supports structured output; check `structured_response` is present on every path, including tool errors.
2. Validate the decoded struct (ranges, enums) after decoding; schema compliance is not business correctness. See `extraction_retries`.
3. Version the schema; changing a field is an API change for every consumer.
4. Monitor parse failures as a rate and alert on increases.

## See also

- [Manual, chapter 4: Prompts, chains and parsers](../../../docs/en-US/manual/04-chains-prompts-parsers.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
