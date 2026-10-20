# text_to_sql

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um deep agent que planeja, valida uma query com um subagent e a executa por uma tool SQL somente-leitura.

## O que faz

- O líder escreve uma lista de todos, anota o schema em `/schema.md`, pede a um subagent `query-validator` que revise o SQL e então roda `sql_query`.
- `StreamEvents` com `graph.ModeUpdates` imprime cada chamada de tool e a resposta final conforme acontecem.

## Como aplicar

Separe "escrever a query", "checar a query" e "rodar a query" para que uma revisão de segurança fique entre a geração e a execução.

```go
deepagents.Create(deepagents.Options{
    Model: lead, Tools: []tools.Tool{sqlQuery},
    SubAgents: []deepagents.SubAgentSpec{{Name: "query-validator", Description: "Reviews SQL for safety.", Model: validator}},
})
```

Combine com a guarda somente-leitura mostrada em `examples/langgraph/sql_agent`.

## Como rodar

```bash
go run ./examples/deepagents/text_to_sql
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Aponte `sql_query` para uma réplica somente-leitura com uma role de banco que só tenha `SELECT`; o prompt não é uma fronteira de segurança, a role é.
2. Rejeite em código instruções que não sejam `SELECT`, múltiplas instruções e `LIMIT` ausente antes de a query chegar ao banco, e defina um statement timeout.
3. Coloque as notas do schema numa skill ou arquivo de memória para que o agent não redescubra o schema a cada pergunta.
4. Monte um conjunto pequeno de perguntas com resultados conhecidos e compare os números, não a redação.
5. Registre toda query executada com o usuário e a thread, para auditoria.

## Veja também

- [Manual, capítulo 10: Deep Agents](../../../docs/pt-BR/manual/10-deep-agents.md)
- [Manual, capítulo 5: Tools](../../../docs/pt-BR/manual/05-tools.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
