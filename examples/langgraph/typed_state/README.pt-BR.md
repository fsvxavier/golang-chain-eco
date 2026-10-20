# typed_state

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um grafo cujo estado é uma struct Go com tags de reducer e validação, com uma etapa de aprovação humana.

## O que faz

- `OrderState` usa tags `graph` para nomear canais e reducers (`sum`, `append`, `messages`) e implementa `Validate()`.
- `price` calcula um total, `approve` pausa com `graph.Interrupt` para uma pessoa, `confirm` finaliza.
- O exemplo retoma a thread com `Command{Resume: true}` e mostra que uma quantidade negativa é rejeitada por `Validate` antes de qualquer nó rodar.

## Como aplicar

```go
type OrderState struct {
    Messages []messages.Message `graph:"messages,reducer=messages"`
    Total    int                `graph:"total,reducer=sum"`
    Log      []string           `graph:"log,reducer=append"`
}
g := graph.NewTyped[OrderState]().AddNode("price", func(ctx context.Context, s OrderState) (graph.Update, error) { ... })
```

Prefira estado tipado em grafos maiores: o compilador pega erros de digitação em campos que `map[string]any` esconde.

## Como rodar

```bash
go run ./examples/langgraph/typed_state
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Use um checkpointer durável e registre tipos de estado customizados com `checkpoint.Register` para que sobrevivam ao round trip em JSON.
2. Coloque as invariantes de negócio em `Validate()`: ele roda antes de cada nó.
3. Derive o `thread_id` do usuário autenticado.
4. Teste a retomada após um reinício do processo.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
