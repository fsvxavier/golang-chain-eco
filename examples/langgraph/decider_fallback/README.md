# decider_fallback

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Answers the history questions with `compress.RuleDecider`, then shows `compress.Guarded` falling back to those rules when the model decider fails.

## What it does

- Builds a short history (4 messages) and a long one (120 messages, about 12k estimated tokens).
- `compress.Summarize(...).State()` gives the JSON-object view the deciders read.
- `RuleDecider{}` answers `compress_history`, `level`, `compress_reply`, `ponytail` and `toon_tool_results` with fixed thresholds (compress above 4000 estimated tokens, level `full`, ponytail above 10% code, TOON above 2048 JSON tool bytes). No model is needed.
- `compress.Guarded` wraps a `FuncDecider` that always returns an error. Every question falls back to the rules, so the run does not fail.

## How to apply it

```go
st := compress.Summarize(msgs, compress.SummaryOptions{}).State()
g := compress.NewGuarded(modelDecider) // Floor 0.7, Timeout 2s, Fallback RuleDecider{}
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

Use `Guarded` whenever a model decides something on the request path. A slow, wrong or missing answer costs quality, never the run. Answers are used only when their id and kind match and their probability is at least the floor.

## Run it

```bash
go run ./examples/langgraph/decider_fallback
```

The output is the same on every run. The short history keeps the full transcript, the long one is compressed, and the guarded run prints the rule answers after the model failure.

## Test it in production mode

1. Replace `modelDecider` with a real `compress.HTTPDecider` (see `compress/httpdecider.go`) and check the floor and timeout against your model's latency.
2. Log which questions fell back. A rising fallback rate means the model decider is broken or too slow.
3. Keep `Guarded` around the decider. Do not call the model decider directly.
4. Run `go test -race -count=1 ./compress` after changing thresholds.

## See also

- [History summary example](../compress_history_summary/README.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
