# async_subagent_server

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent worker servido pela API HTTP do Agent Server e controlado por um supervisor por meio de um `RemoteRunner`.

## O que faz

- Serve um grafo `researcher` com `remote/server` num servidor `httptest` (150 ms de trabalho simulado).
- O supervisor inicia uma tarefa assíncrona por `dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "researcher"}`; o cliente só usa HTTP.
- O exemplo consulta a execução até ela terminar com sucesso e imprime os resultados.

## Como aplicar

Separe o worker em seu próprio serviço e mantenha o supervisor enxuto:

```go
// worker process
http.ListenAndServe(":8123", server.New(map[string]*graph.Compiled{"researcher": worker}))
// supervisor process
runner := dmw.RemoteRunner{Client: remote.NewClient("http://worker:8123"), AssistantID: "researcher"}
```

O mesmo servidor fala o subconjunto do LangGraph Platform, então um cliente Python ou JS também pode chamá-lo.

## Como rodar

```bash
go run ./examples/deepagents/async_subagent_server
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Rode worker e supervisor como dois processos (ou contêineres) e troque a URL do `httptest` pelo endereço do worker.
2. Coloque autenticação e TLS na frente do worker (proxy reverso ou middleware); `remote/server` não autentica por conta própria.
3. Dê ao worker um checkpointer durável (aqui ele usa `checkpoint.NewMemory()`) para que execuções e threads sobrevivam a reinícios.
4. Defina timeouts no cliente, trate `remote.APIError` e adicione um health check para o worker.
5. Faça teste de carga com várias tarefas concorrentes e observe a memória do worker.

## Veja também

- [Manual, capítulo 11: MCP, servidor e graphs remotos](../../../docs/pt-BR/manual/11-mcp-remote-servidor.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
