# Evals

[English](../en-US/EVALS.md) | [Português](../pt-BR/EVALS.md) | **Español**

Paquete `evals/`: ejecuta un objetivo (agent, chain o modelo) sobre un dataset y puntúa cada salida con evaluadores.
Todo corre sin conexión con `models.Fake` y `embeddings.Fake`. Ejemplo: `go run ./examples/evals/basic`.

## API

```go
ds, _ := evals.LoadJSONL("qa.jsonl")          // un evals.Example por línea: id, inputs, outputs, metadata
rep, err := evals.Evaluate(ctx, target, ds, []evals.Evaluator{
    evals.ExactMatch("answer"),                // outputs["answer"] == referencia (strings sin espacios en los extremos)
    evals.Contains("answer"),                  // contiene la referencia
    evals.RegexMatch("answer", regexp.MustCompile(`\d+`)),
    evals.JSONEqual("data"),                   // mismo valor JSON, ignora el orden de las claves
    evals.EmbeddingDistance(emb, "answer", 0.85), // coseno >= umbral
    evals.LLMJudge(judgeModel, rubric),        // salida estructurada: score 0..1 + reasoning
    evals.TrajectoryMatch(evals.Subset, "tools"), // strict | unordered | subset | superset
}, evals.WithConcurrency(4), evals.WithTimeout(time.Minute))
evals.WriteText(os.Stdout, rep)                // tabla por ejemplo + promedio por clave
evals.WriteJSON(os.Stdout, rep)
```

- `target` es `func(ctx, inputs) (outputs, error)`. Un error del objetivo no interrumpe la ejecución: los evaluadores reciben `Run.Err` y el error va al informe.
- `Evaluator` es una interfaz con `Evaluate(ctx, Run, Example) (Result, error)`; `EvaluatorFunc` adapta una función. Un evaluador con error no genera nota (va a `EvalErrors`).
- `Report.Summary` es el promedio de cada clave (`Result.Key`) sobre los ejemplos que la produjeron.
- Trayectoria: el objetivo pone la lista de nombres de tools en `outputs[campo]`; `evals.ToolNames(msgs)` extrae eso de los mensajes.
- El orden del informe sigue al dataset, incluso con concurrencia.

## No portado

- Experimentos alojados, colas de anotación y comparación de experimentos de LangSmith (`langsmith.evaluation` en línea). Solo se portó la parte offline.
- Envío de resultados a LangSmith: el cliente en `tracing/langsmith` solo envía runs de tracing, no datasets ni experimentos. Sería un cliente nuevo para la API de experimentos.
