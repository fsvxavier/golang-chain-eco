# subgraph

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un grafo compilado usado como nodo de un grafo padre, con el estado mapeado a la entrada y a la salida.

## Qué hace

- El grafo hijo limpia y cuenta palabras; el nodo `analyse` del padre llama a `child.Invoke` con los campos que necesita y copia de vuelta solo `words`.
- `summarise` luego formatea el resultado.

## Cómo aplicarlo

```go
AddNode("analyse", func(ctx context.Context, s graph.State) (any, error) {
    out, err := child.Invoke(ctx, graph.State{"text": s["text"]})
    return graph.State{"words": out["words"]}, err
})
```

Usa subgrafos para mantener schemas separados y reutilizar un workflow. Un `Interrupt` dentro del hijo se propaga al padre y se reanuda correctamente; usa `graph.Subgraphs()` para hacer streaming de los eventos del hijo.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/subgraph
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Da al hijo y al padre sus propios schemas de estado y mapea de forma explícita; no filtres campos internos del hijo.
2. Con un checkpointer, los checkpoints del subgrafo viven en el namespace `node:index`; prueba la reanudación tras un reinicio.
3. Versiona un subgrafo reutilizable como una biblioteca.
4. Pon nombre a los grafos con `graph.WithName` para que los traces sean legibles.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
