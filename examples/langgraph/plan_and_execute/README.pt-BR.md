# plan_and_execute

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um planner escreve os passos, um executor os roda um a um e um replanner decide continuar ou responder.

## O que faz

- `plan` transforma a pergunta numa lista numerada; `execute` pega o primeiro passo, roda e acrescenta `(passo, resultado)` a `past_steps`; `replan` responde quando não há mais passos.
- A execução roteirizada responde "cidade natal do vencedor do Australian Open de 2024" em dois passos.

## Como aplicar

```go
AddEdge("execute", "replan").
AddConditionalEdges("replan", func(_ context.Context, s graph.State) (any, error) {
    if s["response"] != nil { return graph.End, nil }
    return "execute", nil
})
```

Use em perguntas de vários passos em que um plano ajuda mais do que um loop livre. Em produção o replanner também deve revisar o plano restante com as novas evidências, não só parar quando a lista esvazia.

## Como rodar

```bash
go run ./examples/langgraph/plan_and_execute
```

O exemplo escolhe o modelo por `examples/internal/demo`: com `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, com `OPENAI_API_KEY` usa `openai:gpt-4o-mini` e, sem chave, cai num fake com respostas roteirizadas, então roda offline.

## Como testar em modo produtivo

1. Use modelos reais e dê ao executor tools reais (busca, calculadoras); hoje ele só chama um modelo.
2. Limite o número de passos e valide o formato do plano antes de executar.
3. Persista o plano e `past_steps` com um checkpointer para que tarefas longas possam ser retomadas.
4. Avalie a qualidade do plano separadamente da qualidade da resposta.

## Veja também

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/pt-BR/manual/07-langgraph.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
