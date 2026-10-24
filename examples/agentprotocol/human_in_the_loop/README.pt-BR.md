# human_in_the_loop

[English](README.md) | **Português** | [Español](README.es-ES.md)

O Agent Streaming Protocol: assine uma thread, acompanhe tokens e atividade de tools, responda a um pedido de aprovação e inspecione checkpoints.

O agent roda com `middleware.Compaction()`: turnos antigos são comprimidos no estilo caveman, uma nota Ponytail é adicionada quando a conversa tem uma parcela real de código, resultados JSON de tools viram TOON, e tudo é decidido por regras, sem precisar de modelo.

## O que faz

- O agent precisa perguntar antes de chamar `pay` (`HumanInTheLoop`). Um cliente assina os canais `lifecycle`, `messages`, `tools` e `input`, inicia uma run com `run.start` e imprime os eventos ordenados (`seq`).
- Quando o servidor emite `input.requested`, o cliente responde com `input.respond` (`approve`), a run termina e o exemplo lê `state.get`, `state.listCheckpoints` e `agent.getTree`.

## Como aplicar

```go
for ev, err := range c.Events(ctx, thread, agentprotocol.Filter{Channels: []string{"lifecycle", "messages", "tools", "input"}}) { ... }
c.Command(ctx, thread, "run.start", map[string]any{"assistantId": "payer", "input": ...})
c.Command(ctx, thread, "input.respond", map[string]any{"interruptId": id, "response": []map[string]any{{"type": "approve"}}})
```

Construa UIs de aprovação sobre isto: mostre os payloads de `input.requested` e devolva a decisão. Os eventos carregam um `seq`, então um cliente que se reconecta pode reproduzir o que perdeu.

## Como rodar

```bash
go run ./examples/agentprotocol/human_in_the_loop
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Coloque o servidor atrás de autenticação e TLS; o endpoint de aprovação é uma ação privilegiada, então autorize quem pode responder a qual thread.
2. Use um checkpointer durável; o interrupt precisa sobreviver a um reinício.
3. Reconecte um cliente no meio de uma run e confirme que ele reproduz a partir do último `seq` sem duplicatas.
4. Adicione uma expiração às aprovações pendentes e teste o caminho do timeout.
5. Use um modelo real e uma implementação real e idempotente de `pay`.

## Veja também

- [Manual, capítulo 17: Agent Protocol: servir e consumir agents por uma API aberta](../../../docs/pt-BR/manual/17-agent-protocol.md)
- [AGENT-PROTOCOL.md](../../../docs/pt-BR/AGENT-PROTOCOL.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
