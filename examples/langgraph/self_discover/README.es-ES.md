# self_discover

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un pipeline de cuatro prompts: seleccionar módulos de razonamiento, adaptarlos a la tarea, armar una estructura de razonamiento y resolver con ella.

## Qué hace

- Cuatro nodos encadenados (`select`, `adapt`, `structure`, `reason`), cada uno un prompt armado con la salida del anterior guardada en el estado.
- Resuelve el acertijo de las dos cuerdas (medir 45 minutos) eligiendo primero un enfoque de razonamiento y luego siguiéndolo.

## Cómo aplicarlo

```go
step := func(key, tmpl string, from ...string) graph.NodeFunc { /* prompt = tmpl + values of the `from` keys */ }
AddNode("adapt", step("adapted", "ADAPT the selected modules to the task.", "selected", "task"))
```

Úsalo en tareas difíciles de razonamiento donde un pipeline fijo supera a un prompt gigante. Cada salida intermedia es inspeccionable, así que puedes ver de dónde salió una mala respuesta.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/self_discover
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo fuerte y tu propio catálogo de módulos de razonamiento.
2. Valida la estructura JSON del tercer paso antes de resolver; reintenta o falla si está mal formada.
3. Compara precisión y coste con un único prompt "piensa paso a paso" en 20-30 tareas; conserva el pipeline solo si gana.
4. Registra las cuatro salidas intermedias con cada respuesta.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
