# extraction_retries

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Structured extraction that validates the result and feeds errors back to the model until it is right.

## What it does

- `extract` asks for JSON `{name, age}`; `validate` unmarshals and calls `Person.Validate()`.
- The scripted model first returns `age: 311` (a typo). The validation error goes back as a human message and the second attempt succeeds. `graph.Sum()` counts attempts, capped at 3.

## How to apply it

```go
type Person struct{ Name string; Age int }
func (p Person) Validate() error { /* business rules */ }
// on error: append messages.Human("Validation failed: " + err.Error() + ". Try again.") and loop to extract
```

Put the rules in `Validate`, not in the prompt: the model can be wrong, the validator cannot. For constrained output, also see `examples/langgraph/structured_output`.

## Run it

```bash
go run ./examples/langgraph/extraction_retries
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real model; add realistic noisy inputs (typos, missing fields, two people in one sentence).
2. Keep the attempt cap and decide what happens after it: return an error, route to human review, or store the raw text for later.
3. Log every validation error; recurring ones point to a prompt or schema problem.
4. Validate for security as well as shape (length limits, allowed values) before the data reaches a database.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 4: Prompts, chains and parsers](../../../docs/en-US/manual/04-chains-prompts-parsers.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
