# human_in_the_loop

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El Agent Streaming Protocol: suscríbete a un thread, sigue tokens y actividad de tools, responde a una solicitud de aprobación e inspecciona checkpoints.

## Qué hace

- El agent debe preguntar antes de llamar a `pay` (`HumanInTheLoop`). Un cliente se suscribe a los canales `lifecycle`, `messages`, `tools` e `input`, inicia una run con `run.start` e imprime los eventos ordenados (`seq`).
- Cuando el servidor emite `input.requested`, el cliente responde con `input.respond` (`approve`), la run termina y el ejemplo lee `state.get`, `state.listCheckpoints` y `agent.getTree`.

## Cómo aplicarlo

```go
for ev, err := range c.Events(ctx, thread, agentprotocol.Filter{Channels: []string{"lifecycle", "messages", "tools", "input"}}) { ... }
c.Command(ctx, thread, "run.start", map[string]any{"assistantId": "payer", "input": ...})
c.Command(ctx, thread, "input.respond", map[string]any{"interruptId": id, "response": []map[string]any{{"type": "approve"}}})
```

Construye UIs de aprobación sobre esto: muestra los payloads de `input.requested` y devuelve la decisión. Los eventos llevan un `seq`, así que un cliente que se reconecta puede reproducir lo que perdió.

## Cómo ejecutarlo

```bash
go run ./examples/agentprotocol/human_in_the_loop
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Pon el servidor detrás de autenticación y TLS; el endpoint de aprobación es una acción privilegiada, así que autoriza quién puede responder a qué thread.
2. Usa un checkpointer duradero; el interrupt debe sobrevivir a un reinicio.
3. Reconecta un cliente a mitad de una run y confirma que reproduce desde su último `seq` sin duplicados.
4. Añade una expiración a las aprobaciones pendientes y prueba la ruta del timeout.
5. Usa un modelo real y una implementación real e idempotente de `pay`.

## Ver también

- [Manual, capítulo 17: Agent Protocol: servir y consumir agents mediante una API abierta](../../../docs/es-ES/manual/17-agent-protocol.md)
- [AGENT-PROTOCOL.md](../../../docs/es-ES/AGENT-PROTOCOL.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
