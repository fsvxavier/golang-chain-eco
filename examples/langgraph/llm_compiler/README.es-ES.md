# llm_compiler

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un planner emite un DAG de llamadas a tools; cada llamada cuyas dependencias terminaron corre en paralelo con `Send`, y luego un joiner arma la respuesta.

## Qué hace

- El plan tiene tres tareas: población de Francia, población de Alemania (independientes) y su suma (depende de ambas).
- `schedule` encuentra las tareas con dependencias completas y las distribuye con `graph.Send`; los resultados se unen en `done` mediante un reducer `BinOp` propio.
- Las dos llamadas independientes corren en paralelo (el ejemplo imprime el tiempo transcurrido), la suma corre después y `join` formatea la respuesta.

## Cómo aplicarlo

Úsalo cuando un modelo puede planificar varias llamadas a tools independientes de antemano: la latencia pasa a ser la profundidad del DAG, no el número de llamadas.

```go
sends = append(sends, graph.Send{Node: "exec", Arg: graph.State{"task": t, "deps": done}})
return sends, nil // a conditional edge returning []Send fans out
```

Aquí el planner es fijo; en un sistema real un modelo devuelve el plan en JSON, que validas (ids únicos, dependencias existentes, sin ciclos) antes de programar.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/llm_compiler
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Deja que un modelo real produzca el plan y valídalo con rigor; rechaza ciclos y tools desconocidas.
2. Cambia el sleep por llamadas a tools reales, cada una con timeout, retry y tope de tamaño del resultado.
3. Limita el paralelismo (un semáforo) para que un plan grande no inunde una API aguas abajo.
4. Decide qué significa una tarea fallida para las dependientes (omitir, replanificar o abortar) y pruébalo.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
