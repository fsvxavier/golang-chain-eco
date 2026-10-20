# multi_agent_supervisor

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um supervisor roteia o trabalho para agents especialistas até decidir que a tarefa acabou.

## O que faz

- Dois especialistas (`researcher`, `writer`) são agents compilados (`agents.Create`) chamados dentro de nós do grafo.
- O modelo supervisor responde com o próximo papel ou `FINISH`; uma aresta condicional segue a escolha e cada worker volta ao supervisor.
- As contribuições são acrescentadas a uma lista de mensagens compartilhada, de modo que cada agent vê o que os outros fizeram.

## Como aplicar

Cada especialista é um agent completo, com seu prompt e suas tools, envolto num nó:

```go
g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
    out, err := w.Invoke(ctx, graph.State{"messages": s["messages"]})
    // append the specialist's last message to the shared list
})
```

Restrinja o supervisor a um conjunto fechado de próximos passos e trate qualquer outra coisa como erro. Para estruturas mais profundas veja `hierarchical_teams`; para um pipeline completo com paralelismo e portões de revisão veja `engineering_team`.

## Como rodar

```bash
go run ./examples/langgraph/multi_agent_supervisor
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use modelos reais; dê a cada especialista apenas as tools de que o papel precisa.
2. Adicione um limite de passos ao loop do supervisor e um `RecursionLimit`; um supervisor que nunca diz `FINISH` é a falha clássica.
3. Registre as decisões de roteamento; avalie em tarefas em que você sabe qual especialista deveria agir.
4. Use um checkpointer durável se uma execução puder demorar ou precisar de revisão humana entre os passos.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
