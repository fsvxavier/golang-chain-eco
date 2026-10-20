# map_reduce

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Distribuye con `Send`, ejecuta workers en paralelo y reduce los resultados.

## Qué hace

- Una arista condicional desde `start` devuelve un `graph.Send` por tema, cada uno con su propia entrada.
- Cada `worker` corre en paralelo y añade a `jokes` (`graph.Append()`); `pick_best` corre una vez cuando todos los workers terminan y reduce la lista.

## Cómo aplicarlo

```go
AddConditionalEdges("start", func(_ context.Context, s graph.State) (any, error) {
    var sends []graph.Send
    for _, subj := range s["subjects"].([]string) {
        sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"subject": subj}})
    }
    return sends, nil
})
```

El patrón para resumir muchos documentos, puntuar muchos candidatos o llamar a una tool por elemento. Los workers paralelos terminan en cualquier orden, así que el reducer no puede depender del orden de llegada (el ejemplo ordena).

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/map_reduce
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Cambia el worker por una llamada real a un modelo o tool y limita cuántos corren a la vez.
2. Pon timeouts y reintentos en los workers; un elemento lento no debe bloquear el lote indefinidamente.
3. Haz que el paso de reducción sea independiente del orden y gestiona resultados vacíos y parciales.
4. En fan-outs grandes, usa un checkpointer para que una caída no rehaga el trabajo ya terminado.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
