# customer_support

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un asistente cuyas tools de solo lectura corren libremente, pero cuya tool sensible requiere aprobación humana. Los agents corren con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, los resultados JSON de tools pasan a TOON, todo decidido por reglas, sin necesidad de modelo.

## Qué hace

- `lookup_booking` es libre; `change_booking` se configura en `middleware.HumanInTheLoop` con las decisiones permitidas `Approve` y `Reject`.
- El modelo pide consultar y cambiar una reserva; la ejecución se pausa con un interrupt. El ejemplo corre dos threads: uno aprueba (la reserva cambia) y otro rechaza (la reserva queda intacta).

## Cómo aplicarlo

```go
middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{
    "change_booking": {Allowed: []string{middleware.Approve, middleware.Reject}},
}}
// pause: out := agent.Invoke(ctx, input); req := graph.Interrupts(out)[0].Value.(middleware.HITLRequest)
// resume: agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: ...}})
```

Hay que usar el mismo `thread_id` para reanudar. Clasifica las tools por efecto secundario: lecturas libres, escrituras y pagos con compuerta.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/customer_support
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un checkpointer **duradero** (`sqlsaver`, Redis, ...) en lugar de `checkpoint.NewMemory()`: una aprobación puede llegar horas después, tras un reinicio.
2. Deriva el `thread_id` de la sesión autenticada, nunca de entrada del cliente.
3. Construye la interfaz de aprobación a partir de `HITLRequest` (nombre y argumentos de la tool) y registra quién aprobó qué y cuándo.
4. Haz `change_booking` idempotente y revalida los permisos al ejecutar, no solo al aprobar.
5. Prueba el camino de rechazo: el agent debe decir al cliente que nada cambió.

## Ver también

- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
