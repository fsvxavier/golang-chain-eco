# rubric

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A grader scores the final answer against criteria and sends the agent back until it passes (or the limit is reached).

## What it does

- The writer answers without a citation; the `Rubric` middleware's grader says `needs_revision` with the gap "add a [source: url] citation".
- The agent revises, the grader says `satisfied`. The example prints each evaluation and the final answer.

## How to apply it

```go
dmw.Rubric{Grader: graderModel, Criteria: []string{"cites a source"}, MaxIterations: 3}
```

Write criteria that a grader can answer pass/fail, one per line. Use a different (often stronger) model as grader than as writer, and keep `MaxIterations` small: each loop costs a full model call plus a grading call.

## Run it

```bash
go run ./examples/deepagents/rubric
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use real writer and grader models; the grader must return the JSON shape `{"result": ..., "criteria": [...]}`, so test that a real model follows it.
2. Calibrate the rubric on 10-20 known good and bad answers before trusting it; log disagreements.
3. Decide what happens when `MaxIterations` is reached without `satisfied` (ship with a warning, escalate to a human) and test that path.
4. Store `rubric_evaluations` with each answer for later analysis.

## See also

- [Manual, chapter 10: Deep Agents](../../../docs/en-US/manual/10-deep-agents.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
