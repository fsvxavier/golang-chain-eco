# compress_ponytail

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Appends the Ponytail directive, a nudge toward writing less code, to the last Human message of the per-call copy, using `middleware.NewCompression`.

## What it does

- A three-message history (Human, AI, Human) goes into `agents.Create` with the system prompt `You are a Go developer working in this repository.`
- `middleware.NewCompression(decider)` wraps the decider in `compress.NewGuarded`. The scripted decider says yes to `ponytail` and picks a level (`lite`, `full` or `ultra`). Every other question says no, so only the Ponytail note changes.
- The note goes after the last Human message only, once per call. The first Human message and the system prompt stay as they were.
- A `sniff` middleware placed after compression records the request the model receives. A fake model answers offline.
- The example prints the note length and its first line for each level, and fails if the note is not the directive text, appears twice, or reaches the system prompt or the stored history.

The directive is adapted from Ponytail (https://github.com/DietrichGebert/ponytail, MIT licensed, by Dietrich Gebert); see that repository for its license text.

## How to apply it

```go
agent, _ := agents.Create(agents.Options{
    Model:        model,
    SystemPrompt: "You are a Go developer working in this repository.",
    Middleware:   []agents.Middleware{middleware.NewCompression(decider)},
})
```

The decider must answer `compress.QPonytail` (a yes/no question, `KindNoul`). Optionally answer `compress.QLevel` to pick `lite`, `full` (default) or `ultra`. `compress.RuleDecider{}` answers `ponytail` yes when the conversation has code blocks in at least 10% of its bytes.

## Run it

```bash
go run ./examples/langgraph/compress_ponytail
```

With no API key the example uses a scripted fake model, so it runs offline and prints the same output every time. With `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` set, `examples/internal/demo` uses that provider for the reply; the note the model receives is the same.

## Test it in production mode

1. Replace the scripted decider with `compress.HTTPDecider` or a small model; keep `Floor` at `0.7`, so a weak answer falls back to the rules.
2. Measure the effect, not the text: run the same coding tasks with and without the note and count the lines the model adds, the new files and the dependencies it introduces. Ponytail only helps if the model follows it, and that depends on the model.
3. Check that the model still validates input, handles errors and writes tests for risky logic. The directive says not to cut those, but confirm it on your tasks.
4. Start with `full`; use `ultra` only after the measurements above show it helps.

## See also

- [Compression package](../../../compress/)
- [Example: compress_toon](../compress_toon/README.md)
- [Example: compress_prompt_caveman](../compress_prompt_caveman/README.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
