# Evals

**English** | [Português](../pt-BR/EVALS.md) | [Español](../es-ES/EVALS.md)

Package `evals/`: runs a target (agent, chain or model) over a dataset and scores each output with evaluators.
Everything runs offline with `models.Fake` and `embeddings.Fake`. Example: `go run ./examples/evals/basic`.

## API

```go
ds, _ := evals.LoadJSONL("qa.jsonl")          // one evals.Example per line: id, inputs, outputs, metadata
rep, err := evals.Evaluate(ctx, target, ds, []evals.Evaluator{
    evals.ExactMatch("answer"),                // outputs["answer"] == reference (strings without leading/trailing spaces)
    evals.Contains("answer"),                  // contains the reference
    evals.RegexMatch("answer", regexp.MustCompile(`\d+`)),
    evals.JSONEqual("data"),                   // same JSON value, ignores key order
    evals.EmbeddingDistance(emb, "answer", 0.85), // cosine >= threshold
    evals.LLMJudge(judgeModel, rubric),        // structured output: score 0..1 + reasoning
    evals.TrajectoryMatch(evals.Subset, "tools"), // strict | unordered | subset | superset
}, evals.WithConcurrency(4), evals.WithTimeout(time.Minute))
evals.WriteText(os.Stdout, rep)                // table per example + average per key
evals.WriteJSON(os.Stdout, rep)
```

- `target` is `func(ctx, inputs) (outputs, error)`. A target error does not stop the run: evaluators receive `Run.Err` and the error goes to the report.
- `Evaluator` is an interface with `Evaluate(ctx, Run, Example) (Result, error)`; `EvaluatorFunc` adapts a function. An evaluator that fails produces no score (it goes to `EvalErrors`).
- `Report.Summary` is the average of each key (`Result.Key`) over the examples that produced it.
- Trajectory: the target puts the list of tool names in `outputs[field]`; `evals.ToolNames(msgs)` extracts it from the messages.
- Report order follows the dataset, even with concurrency.

## Not ported

- Hosted experiments, annotation queues and experiment comparison from LangSmith (online `langsmith.evaluation`). Only the offline part was ported.
- Sending results to LangSmith: the client in `tracing/langsmith` only sends tracing runs, not datasets or experiments. It would be a new client for the experiments API.
