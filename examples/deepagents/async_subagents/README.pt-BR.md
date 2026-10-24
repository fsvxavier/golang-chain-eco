# async_subagents

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um deep agent lança uma tarefa longa em segundo plano e a consulta depois.

O agent roda com `middleware.Compaction()`: turnos antigos são comprimidos no estilo caveman, uma nota Ponytail é adicionada quando a conversa tem uma parcela real de código, resultados JSON de tools viram TOON, e tudo é decidido por regras, sem precisar de modelo.

## O que faz

- O middleware `AsyncSubagents` dá ao agent principal `start_async_task` (e as tools de checar/cancelar).
- O `researcher` roda em segundo plano por um `LocalRunner` em processo enquanto o agent principal continua respondendo.
- O exemplo consulta o handle até a tarefa terminar e imprime o relatório.

## Como aplicar

```go
runner := &dmw.LocalRunner{Agent: researcher}
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{{Name: "researcher", Description: "long-running research", Runner: runner}}}
```

A interface `AsyncRunner` (`Start`, `Poll`, `Send`, `Cancel`) é o que você implementa ou troca: `LocalRunner` fica em processo, `RemoteRunner` conversa com um Agent Server (veja `async_subagent_server`).

## Como rodar

```bash
go run ./examples/deepagents/async_subagents
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Use modelos reais; pesquisas reais levam minutos, então consulte com backoff em vez de um loop apertado.
2. Limite as tarefas em segundo plano simultâneas e dê um prazo a cada uma; cancele com `Cancel` quando o usuário sair.
3. Persista os handles das tarefas (o estado `async_tasks`) com um checkpointer durável para que um reinício não deixe trabalho órfão.
4. Mova workers pesados para processos separados com `RemoteRunner` quando precisarem de isolamento ou escala.

## Veja também

- [Manual, capítulo 10: Deep Agents](../../../docs/pt-BR/manual/10-deep-agents.md)
- [Manual, capítulo 11: MCP, servidor e graphs remotos](../../../docs/pt-BR/manual/11-mcp-remote-servidor.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
