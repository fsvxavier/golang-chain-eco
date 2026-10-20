# react_from_scratch

[English](README.md) | **Português** | [Español](README.es-ES.md)

O loop ReAct montado à mão com um `StateGraph`, para mostrar do que um agent é feito.

## O que faz

- Dois nós: `agent` (chama o modelo vinculado às tools) e `tools` (`prebuilt.NewToolNode`); `prebuilt.ToolsCondition` roteia para `tools` quando a última mensagem tem chamadas de tool e, senão, termina.
- Imprime o grafo como diagrama Mermaid e faz streaming dos updates de uma pergunta.

## Como aplicar

```go
g.AddNode("agent", callModel).
  AddNode("tools", prebuilt.NewToolNode(weather).Node()).
  AddEdge(graph.Start, "agent").
  AddConditionalEdges("agent", prebuilt.ToolsCondition).
  AddEdge("tools", "agent")
```

Monte o loop você mesmo quando precisar de um nó que `agents.Create` não oferece (roteamento customizado, estado extra, um passo humano). Caso contrário prefira `agents.Create`, que adiciona a cadeia de middleware.

## Como rodar

```bash
go run ./examples/langgraph/react_from_scratch
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Rode com um modelo real vinculado com `models.BindTools`.
2. Defina um `RecursionLimit` em `runnables.Config`: um loop montado à mão não tem limite de chamadas a menos que você o adicione.
3. Adicione um checkpointer para conversas e tratamento de erros no nó do modelo.
4. Use `app.Mermaid()` na sua documentação para que a topologia continue revisável.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Manual, capítulo 9: Agents e middleware](../../../docs/pt-BR/manual/09-agents-e-middleware.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
