# react_from_scratch

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El bucle ReAct armado a mano con un `StateGraph`, para mostrar de qué está hecho un agent.

## Qué hace

- Dos nodos: `agent` (llama al modelo vinculado a las tools) y `tools` (`prebuilt.NewToolNode`); `prebuilt.ToolsCondition` enruta a `tools` cuando el último mensaje tiene llamadas a tools y, si no, termina.
- Imprime el grafo como diagrama Mermaid y hace streaming de los updates de una pregunta.

## Cómo aplicarlo

```go
g.AddNode("agent", callModel).
  AddNode("tools", prebuilt.NewToolNode(weather).Node()).
  AddEdge(graph.Start, "agent").
  AddConditionalEdges("agent", prebuilt.ToolsCondition).
  AddEdge("tools", "agent")
```

Arma el bucle tú mismo cuando necesites un nodo que `agents.Create` no ofrece (enrutamiento personalizado, estado extra, un paso humano). En otro caso prefiere `agents.Create`, que añade la cadena de middleware.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/react_from_scratch
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Ejecuta con un modelo real vinculado con `models.BindTools`.
2. Define un `RecursionLimit` en `runnables.Config`: un bucle armado a mano no tiene límite de llamadas a menos que lo añadas.
3. Añade un checkpointer para conversaciones y gestión de errores en el nodo del modelo.
4. Usa `app.Mermaid()` en tu documentación para que la topología siga siendo revisable.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
