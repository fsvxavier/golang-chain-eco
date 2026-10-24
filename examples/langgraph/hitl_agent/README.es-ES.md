# hitl_agent

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Pausa una llamada a tool para aprobación humana y luego reanuda la misma ejecución. Los agents corren con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, los resultados JSON de tools pasan a TOON, todo decidido por reglas, sin necesidad de modelo.

## Qué hace

- `wire_money` tiene compuerta en `middleware.HumanInTheLoop`. El primer `Invoke` regresa con un interrupt (`graph.Interrupts(out)`).
- Un `graph.Command{Resume: HITLResponse{Decisions: [Approve]}}` en el mismo thread continúa la ejecución y la tool corre.

## Cómo aplicarlo

El flujo de aprobación más pequeño. Úsalo como plantilla para cualquier acción irreversible: pagos, correos, borrados, despliegues.

```go
ctx := runnables.WithConfig(ctx, graph.Thread("demo"))
out, _ := agent.Invoke(ctx, graph.State{"messages": "Wire $500 to Bob"})
// show graph.Interrupts(out) to a human ...
out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: []middleware.HITLDecision{{Type: middleware.Approve}}}})
```

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/hitl_agent
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Cambia `checkpoint.NewMemory()` por un saver duradero y entrega la solicitud de aprobación por tu canal real (UI, Slack, ticket).
2. Añade una política de timeout/expiración: una solicitud que nadie responde no debe ejecutarse después, por sorpresa.
3. Permite `Edit` solo donde editar argumentos sea seguro, y revalida los argumentos editados.
4. Registra en un log de auditoría la solicitud, la decisión, el aprobador y el resultado.
5. Prueba la reanudación tras un reinicio del proceso para demostrar que la pausa es realmente duradera.

## Ver también

- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
