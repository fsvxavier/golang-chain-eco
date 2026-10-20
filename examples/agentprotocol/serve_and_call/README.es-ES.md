# serve_and_call

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Sirve un agent con el Agent Protocol y contrólalo con el cliente del protocolo: threads, runs, streaming, viaje en el tiempo y el store de largo plazo.

## Qué hace

- Sirve un agent de clima con `agentprotocol.New` en un servidor `httptest` y habla con él mediante `agentprotocol.NewClient`.
- Cubre descubrimiento de agents y schemas, una conversación de varios turnos en un thread, historial y viaje en el tiempo (`PatchThread` desde un checkpoint anterior, `CopyThread`), una run sin estado con streaming SSE y el store clave-valor (`PutItem`, `SearchItems`, `ListNamespaces`).

## Cómo aplicarlo

```go
srv, _ := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "weather-bot", Graph: agent}}})
http.ListenAndServe(":8080", srv)
c := agentprotocol.NewClient("http://localhost:8080", apiKey)
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{ThreadID: id, AgentID: "weather-bot", Messages: msgs})
```

Cualquier cliente compatible (los SDK de Python y JS, UIs) puede entonces usar tu agent en Go. Da un checkpointer al agent: los threads, el historial y el viaje en el tiempo dependen de él.

## Cómo ejecutarlo

```bash
go run ./examples/agentprotocol/serve_and_call
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Sirve en una dirección real detrás de TLS y de un proxy inverso que autentique; el segundo argumento de `NewClient` es la clave de API que comprueba tu proxy.
2. Cambia `checkpoint.NewMemory()` y el run store en memoria por versiones duraderas (`agentprotocol.NewStoreRunStore` sobre un `store.Store` duradero) para que threads y runs sobrevivan a los reinicios y a varias réplicas.
3. Ejecuta dos réplicas del servidor y verifica que un thread iniciado en una puede continuar en la otra.
4. Usa un modelo real; mantén los ids de llamadas a tools únicos por conversación.
5. Haz una prueba de carga de las conexiones de streaming y define timeouts en el servidor.

## Ver también

- [Manual, capítulo 17: Agent Protocol: servir y consumir agents mediante una API abierta](../../../docs/es-ES/manual/17-agent-protocol.md)
- [AGENT-PROTOCOL.md](../../../docs/es-ES/AGENT-PROTOCOL.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
