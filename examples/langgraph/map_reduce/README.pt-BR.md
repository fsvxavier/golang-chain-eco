# map_reduce

[English](README.md) | **Português** | [Español](README.es-ES.md)

Distribui com `Send`, roda workers em paralelo e reduz os resultados.

## O que faz

- Uma aresta condicional a partir de `start` devolve um `graph.Send` por assunto, cada um com sua própria entrada.
- Cada `worker` roda em paralelo e acrescenta a `jokes` (`graph.Append()`); `pick_best` roda uma vez quando todos os workers terminam e reduz a lista.

## Como aplicar

```go
AddConditionalEdges("start", func(_ context.Context, s graph.State) (any, error) {
    var sends []graph.Send
    for _, subj := range s["subjects"].([]string) {
        sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"subject": subj}})
    }
    return sends, nil
})
```

O padrão para resumir muitos documentos, pontuar muitos candidatos ou chamar uma tool por item. Workers paralelos terminam em qualquer ordem, então o reducer não pode depender da ordem de chegada (o exemplo ordena).

## Como rodar

```bash
go run ./examples/langgraph/map_reduce
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Troque o worker por uma chamada real de modelo ou tool e limite quantos rodam ao mesmo tempo.
2. Coloque timeouts e retries nos workers; um item lento não deve travar o lote indefinidamente.
3. Torne a etapa de redução independente da ordem e trate resultados vazios e parciais.
4. Em fan-outs grandes, use um checkpointer para que uma queda não refaça o trabalho já terminado.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
