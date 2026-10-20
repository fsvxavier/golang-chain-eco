# subgraph

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um grafo compilado usado como nó de um grafo pai, com o estado mapeado na entrada e na saída.

## O que faz

- O grafo filho limpa e conta palavras; o nó `analyse` do pai chama `child.Invoke` com os campos de que precisa e copia de volta apenas `words`.
- `summarise` então formata o resultado.

## Como aplicar

```go
AddNode("analyse", func(ctx context.Context, s graph.State) (any, error) {
    out, err := child.Invoke(ctx, graph.State{"text": s["text"]})
    return graph.State{"words": out["words"]}, err
})
```

Use subgrafos para manter schemas separados e reaproveitar um workflow. Um `Interrupt` dentro do filho propaga ao pai e retoma corretamente; use `graph.Subgraphs()` para fazer streaming dos eventos do filho.

## Como rodar

```bash
go run ./examples/langgraph/subgraph
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Dê ao filho e ao pai os próprios schemas de estado e mapeie de forma explícita; não vaze campos internos do filho.
2. Com um checkpointer, os checkpoints do subgrafo vivem no namespace `node:index`; teste a retomada após um reinício.
3. Versione um subgrafo reutilizável como uma biblioteca.
4. Dê nomes aos grafos com `graph.WithName` para que os traces fiquem legíveis.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
