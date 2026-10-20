# basic

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Puntúa un sistema sobre un dataset JSONL con evaluadores de igualdad exacta, contains, embedding y LLM-as-judge e imprime un informe en texto y JSON.

## Qué hace

- Escribe y recarga un dataset JSONL de tres preguntas (`evals.SaveJSONL`/`LoadJSONL`).
- Ejecuta el objetivo (un modelo aquí) en cada ejemplo con concurrencia 3 y lo puntúa con `ExactMatch`, `Contains`, `EmbeddingDistance` y `LLMJudge`.
- Imprime `evals.WriteText` y `evals.WriteJSON`: la igualdad exacta falla en "It is 4." mientras contains y el juez pasan, lo que muestra por qué se combinan evaluadores.

## Cómo aplicarlo

```go
report, _ := evals.Evaluate(ctx, run, dataset, []evals.Evaluator{
    evals.ExactMatch("answer"), evals.Contains("answer"),
    evals.EmbeddingDistance(embedder, "answer", 0.8),
    evals.LLMJudge(judge, "The answer must be a correct, direct reply."),
}, evals.WithConcurrency(8))
```

`run` puede ser cualquier función de entradas a salidas: una chain, un agent, una llamada HTTP. Mantén el dataset en control de versiones junto a los prompts.

## Cómo ejecutarlo

```bash
go run ./examples/evals/basic
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Arma un dataset con preguntas reales (unas decenas para empezar) y salidas comprobadas por una persona.
2. Apunta `run` al sistema que despliegas (el mismo agent, prompt y modelo) y usa un modelo distinto como juez.
3. Usa un embedder real en `EmbeddingDistance`.
4. Ejecútalo en CI con cada cambio de prompt o modelo; rompe el build cuando una métrica caiga bajo un umbral y guarda el informe JSON como artefacto.
5. Vuelve a ejecutar los casos inestables varias veces y mira la varianza antes de confiar en un delta.

## Ver también

- [EVALS.md](../../../docs/es-ES/EVALS.md)
- [Manual, capítulo 12: Observabilidad](../../../docs/es-ES/manual/12-observabilidade.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
