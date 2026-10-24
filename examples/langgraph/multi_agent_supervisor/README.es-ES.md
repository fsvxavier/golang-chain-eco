# multi_agent_supervisor

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un supervisor enruta el trabajo a agents especialistas hasta que decide que la tarea terminó. El agent se ejecuta con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, y los resultados JSON de tools se recodifican en TOON, todo decidido por reglas, sin necesitar modelo.

## Qué hace

- Dos especialistas (`researcher`, `writer`) son agents compilados (`agents.Create`) llamados dentro de nodos del grafo.
- El modelo supervisor responde con el siguiente rol o `FINISH`; una arista condicional sigue su elección y cada worker vuelve al supervisor.
- Las contribuciones se añaden a una lista de mensajes compartida, de modo que cada agent ve lo que hicieron los otros.

## Cómo aplicarlo

Cada especialista es un agent completo, con su prompt y sus tools, envuelto en un nodo:

```go
g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
    out, err := w.Invoke(ctx, graph.State{"messages": s["messages"]})
    // append the specialist's last message to the shared list
})
```

Restringe el supervisor a un conjunto cerrado de siguientes pasos y trata cualquier otra cosa como error. Para estructuras más profundas mira `hierarchical_teams`; para un pipeline completo con paralelismo y compuertas de revisión mira `engineering_team`.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/multi_agent_supervisor
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa modelos reales; da a cada especialista solo las tools que su rol necesita.
2. Añade un límite de pasos al bucle del supervisor y un `RecursionLimit`; un supervisor que nunca dice `FINISH` es el fallo clásico.
3. Registra las decisiones de enrutamiento; evalúa en tareas donde sabes qué especialista debería actuar.
4. Usa un checkpointer duradero si una ejecución puede tardar o necesitar revisión humana entre pasos.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
