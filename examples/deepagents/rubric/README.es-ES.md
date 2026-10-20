# rubric

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un evaluador puntúa la respuesta final contra criterios y devuelve al agent hasta que pase (o hasta el límite).

## Qué hace

- El redactor responde sin cita; el evaluador del middleware `Rubric` dice `needs_revision` con la brecha "add a [source: url] citation".
- El agent revisa y el evaluador dice `satisfied`. El ejemplo imprime cada evaluación y la respuesta final.

## Cómo aplicarlo

```go
dmw.Rubric{Grader: graderModel, Criteria: []string{"cites a source"}, MaxIterations: 3}
```

Escribe criterios que un evaluador pueda responder con pasa/no pasa, uno por línea. Usa un modelo distinto (a menudo más fuerte) como evaluador que como redactor y mantén `MaxIterations` pequeño: cada vuelta cuesta una llamada completa al modelo más una de evaluación.

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/rubric
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa modelos reales para redactor y evaluador; el evaluador debe devolver el formato JSON `{"result": ..., "criteria": [...]}`, así que prueba que un modelo real lo siga.
2. Calibra la rúbrica con 10-20 respuestas buenas y malas conocidas antes de confiar en ella; registra los desacuerdos.
3. Decide qué ocurre cuando se alcanza `MaxIterations` sin `satisfied` (entregar con aviso, escalar a una persona) y prueba esa ruta.
4. Guarda `rubric_evaluations` con cada respuesta para análisis posterior.

## Ver también

- [Manual, capítulo 10: Deep Agents](../../../docs/es-ES/manual/10-deep-agents.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
