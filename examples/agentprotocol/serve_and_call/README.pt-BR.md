# serve_and_call

[English](README.md) | **Português** | [Español](README.es-ES.md)

Sirva um agent pelo Agent Protocol e controle-o com o cliente do protocolo: threads, runs, streaming, viagem no tempo e o store de longo prazo.

O agent roda com `middleware.Compaction()`: turnos antigos são comprimidos no estilo caveman, uma nota Ponytail é adicionada quando a conversa tem uma parcela real de código, resultados JSON de tools viram TOON, e tudo é decidido por regras, sem precisar de modelo.

## O que faz

- Serve um agent de clima com `agentprotocol.New` num servidor `httptest` e conversa com ele por `agentprotocol.NewClient`.
- Cobre descoberta de agents e schemas, uma conversa de vários turnos numa thread, histórico e viagem no tempo (`PatchThread` a partir de um checkpoint anterior, `CopyThread`), uma run sem estado com streaming SSE e o store chave-valor (`PutItem`, `SearchItems`, `ListNamespaces`).

## Como aplicar

```go
srv, _ := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "weather-bot", Graph: agent}}})
http.ListenAndServe(":8080", srv)
c := agentprotocol.NewClient("http://localhost:8080", apiKey)
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{ThreadID: id, AgentID: "weather-bot", Messages: msgs})
```

Qualquer cliente compatível (os SDKs de Python e JS, UIs) passa a poder usar o seu agent em Go. Dê um checkpointer ao agent: threads, histórico e viagem no tempo dependem dele.

## Como rodar

```bash
go run ./examples/agentprotocol/serve_and_call
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Sirva num endereço real atrás de TLS e de um proxy reverso que autentique; o segundo argumento de `NewClient` é a chave de API que o seu proxy confere.
2. Troque `checkpoint.NewMemory()` e o run store em memória por versões duráveis (`agentprotocol.NewStoreRunStore` sobre um `store.Store` durável) para que threads e runs sobrevivam a reinícios e a várias réplicas.
3. Rode duas réplicas do servidor e verifique que uma thread iniciada numa pode continuar na outra.
4. Use um modelo real; mantenha os ids de chamadas de tool únicos por conversa.
5. Faça teste de carga das conexões de streaming e defina timeouts no servidor.

## Veja também

- [Manual, capítulo 17: Agent Protocol: servir e consumir agents por uma API aberta](../../../docs/pt-BR/manual/17-agent-protocol.md)
- [AGENT-PROTOCOL.md](../../../docs/pt-BR/AGENT-PROTOCOL.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
