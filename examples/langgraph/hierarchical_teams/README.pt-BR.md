# hierarchical_teams

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um supervisor de topo delega a subgrafos de times, cada um com seu próprio supervisor e workers.

## O que faz

- Dois times (pesquisa, redação) são grafos compilados com um supervisor que roda seus workers em ordem.
- O supervisor de topo roteia para um time, o time roda como um nó e devolve só as mensagens que adicionou; então o controle volta ao supervisor de topo até ele dizer `FINISH`.
- Nenhum modelo é usado; os supervisores são regras para que a estrutura seja o foco.

## Como aplicar

Componha times como subgrafos e faça de cada supervisor uma chamada de LLM quando as decisões exigirem julgamento:

```go
g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
    out, _ := sub.Invoke(ctx, graph.State{"messages": s["messages"]})
    return graph.State{"messages": ms[len(prev):]}, nil // only what the team added
})
```

Devolver só o delta mantém o estado do pai livre de duplicatas. Para um único nível veja `multi_agent_supervisor`.

## Como rodar

```bash
go run ./examples/langgraph/hierarchical_teams
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Troque os supervisores baseados em regras por chamadas de modelo restritas a um conjunto fechado de rotas; adicione limites de loop nos dois níveis.
2. Use agents reais (`agents.Create`) como workers, cada um com as próprias tools.
3. Adicione um checkpointer para que execuções longas retomem; faça streaming com `graph.Subgraphs()` para acompanhar os times internos.
4. Faça o trace por nome do time (`graph.WithName`) e defina orçamentos por time.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
