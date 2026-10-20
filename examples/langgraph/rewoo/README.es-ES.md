# rewoo

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Reasoning WithOut Observation: planifica todas las llamadas a tools de antemano con placeholders (`#E1`, `#E2`), ejecuta en orden y luego resuelve.

## Qué hace

- El planner emite pares `Plan:` / `#E1 = Search[...]` de una vez; una regexp los convierte en pasos.
- `tool` ejecuta un paso a la vez, sustituyendo evidencia anterior (`#E1`) en las entradas siguientes, y une los resultados con un reducer `BinOp`; `solve` escribe la respuesta final a partir de la evidencia.

## Cómo aplicarlo

En comparación con ReAct, el modelo se llama dos veces (planificar y resolver) en lugar de una vez por llamada a tool, lo que ahorra tokens cuando el plan no depende de sorpresas intermedias.

```go
var stepRe = regexp.MustCompile(`Plan:\s*(.+)\n\s*(#E\d+)\s*=\s*(\w+)\[(.+)\]`)
```

Valida el plan interpretado (tools conocidas, variables definidas antes de usarse) antes de ejecutar nada.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/rewoo
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa modelos y tools reales; la salida del modelo se alejará del formato exacto, así que ajusta el prompt y rechaza los planes que no se interpreten.
2. Limita el número de pasos y define timeouts por tool.
3. Nunca sustituyas evidencia en un comando de shell o en una consulta sin escapar; trata los resultados de las tools como no confiables.
4. Compara tokens y precisión con un agent ReAct en las mismas tareas.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
