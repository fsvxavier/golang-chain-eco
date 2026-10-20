# research

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent líder planeja, delega duas perguntas de pesquisa a subagents em paralelo e escreve um relatório em arquivo.

## O que faz

- O líder chama `write_todos`, depois duas chamadas `task` ao subagent `researcher` ao mesmo tempo e, por fim, `write_file` para `/report.md`.
- O subagent tem o próprio modelo e a tool `internet_search` (um stub aqui).
- Imprime todos os arquivos do filesystem virtual, a lista de todos e a mensagem final.

## Como aplicar

```go
deepagents.Create(deepagents.Options{
    Model: lead, Tools: []tools.Tool{search},
    SubAgents: []deepagents.SubAgentSpec{{Name: "researcher", Description: "Focused researcher.", Model: sub}},
})
```

Dê a cada subagent uma descrição estreita (o líder a usa para decidir quem recebe o trabalho), um modelo próprio se for mais barato e apenas as tools de que precisa. Chamadas `task` paralelas num mesmo turno rodam de forma concorrente.

## Como rodar

```bash
go run ./examples/deepagents/research
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Troque o stub `internet_search` por uma tool de busca real (cliente HTTP com timeout, limite de resultados e lista de URLs permitidas) e use modelos reais para o líder e o subagent.
2. Use um modelo mais forte para o líder e um mais barato para os pesquisadores; compare o custo por relatório.
3. Limite a execução: `ModelCallLimit`, `ToolCallLimit` e um `RecursionLimit`, porque um agent de pesquisa pode entrar em loop de buscas.
4. Trate o texto buscado na web como entrada não confiável (prompt injection): mantenha a etapa de escrita do relatório sem tools que possam agir sobre o mundo externo.
5. Persista os relatórios com `backends.Filesystem` ou um object store em vez de lê-los do estado.

## Veja também

- [Manual, capítulo 10: Deep Agents](../../../docs/pt-BR/manual/10-deep-agents.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
