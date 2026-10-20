# basic

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Score a system over a JSONL dataset with exact, contains, embedding and LLM-judge evaluators and print a text and JSON report.

## What it does

- Writes and reloads a three-question JSONL dataset (`evals.SaveJSONL`/`LoadJSONL`).
- Runs the target (a model here) on each example with concurrency 3 and scores it with `ExactMatch`, `Contains`, `EmbeddingDistance` and `LLMJudge`.
- Prints `evals.WriteText` and `evals.WriteJSON`: exact match fails on "It is 4." while contains and the judge pass, which shows why you combine evaluators.

## How to apply it

```go
report, _ := evals.Evaluate(ctx, run, dataset, []evals.Evaluator{
    evals.ExactMatch("answer"), evals.Contains("answer"),
    evals.EmbeddingDistance(embedder, "answer", 0.8),
    evals.LLMJudge(judge, "The answer must be a correct, direct reply."),
}, evals.WithConcurrency(8))
```

`run` can be any function from inputs to outputs: a chain, an agent, an HTTP call. Keep the dataset in version control next to the prompts.

## Run it

```bash
go run ./examples/evals/basic
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Build a dataset from real questions (a few dozen to start) with outputs checked by a human.
2. Point `run` at the system you deploy (the same agent, prompt and model) and use a different model as judge.
3. Use a real embedder for `EmbeddingDistance`.
4. Run it in CI on every prompt or model change; fail the build when a metric drops below a threshold, and save the JSON report as an artifact.
5. Re-run flaky cases several times and look at variance before trusting a delta.

## See also

- [EVALS.md](../../../docs/en-US/EVALS.md)
- [Manual, chapter 12: Observability](../../../docs/en-US/manual/12-observabilidade.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
