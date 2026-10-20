# 17. Agent Protocol: servir y consumir agents mediante una API abierta

[English](../../en-US/manual/17-agent-protocol.md) | [Português](../../pt-BR/manual/17-agent-protocol.md) | **Español**

El paquete `agentprotocol` publica cualquier grafo compilado (`agents.Create`, `deepagents.Create`, un `StateGraph`) con el [Agent Protocol](https://github.com/langchain-ai/agent-protocol): una API REST **independiente del framework** para agentes en producción, más un protocolo de **streaming por thread** (eventos tipados, filtros, replay, aprobación humana). Referencia completa y límites: [docs/es-ES/AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md).

Requisitos previos: capítulos 7 y 8 (grafos, checkpointer, interrupts) y 9 (agents).

## 17.1 Cuándo usarlo

| Quieres… | Usa |
|---|---|
| que una UI/servicio externo hable con tu agent por HTTP | `agentprotocol.Server` |
| el dialecto de LangGraph Platform (SDKs oficiales) | `remote/server` (cap. 11) |
| llamar a un agent Go desde otro proceso Go | `agentprotocol.Client` |
| seguir tokens, tools y aprobaciones en vivo | canales de streaming (17.5) |

## 17.2 Servir un agent

```go
agent, _ := agents.Create(agents.Options{
	Model: model, Tools: tools,
	Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}, // estado de los threads
})
srv, err := agentprotocol.New(agentprotocol.Options{
	Agents: []agentprotocol.AgentSpec{{ID: "bot", Name: "Bot", Description: "responde preguntas", Graph: agent}},
	APIKey: os.Getenv("AP_KEY"), // opcional
})
http.ListenAndServe(":8123", srv)
```

- **Sin checkpointer** el agent funciona, pero los threads no guardan estado (sin `history`, `state.get` ni reanudación).
- En producción, cambia `checkpoint.NewMemory()` por `sqlsaver`, Redis, Mongo o DynamoDB (caps. 8 y 13) — y `Options.Store` por el store de largo plazo correspondiente.
- El servidor guarda threads/runs en memoria, pero reconstruye un thread desde el checkpointer en el primer acceso después de un reinicio.

## 17.3 Llamar con el cliente

```go
c := agentprotocol.NewClient("http://localhost:8123", os.Getenv("AP_KEY"))

th, _ := c.CreateThread(ctx, agentprotocol.ThreadCreate{Metadata: map[string]any{"user": "ana"}})
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{
	ThreadID: th.ThreadID, AgentID: "bot",
	Messages: []agentprotocol.Message{{Role: "user", Content: "¿Qué tiempo hace en Lisboa?"}},
})
fmt.Println(out.Run.Status, out.Messages[len(out.Messages)-1].Content)
```

Tres formas de ejecutar:

| Forma | Llamada | Cuándo |
|---|---|---|
| esperar | `CreateAndWait` (`POST /runs/wait`) | respuesta corta |
| background | `CreateRun` + `WaitRun`/`GetRun` | trabajo largo; `webhook` avisa al final |
| stream | `StreamRun` (`POST /runs/stream`) | UI incremental: `for ev, err := range c.StreamRun(ctx, req)` |

Sin `thread_id` la run es **stateless** (thread efímero, borrado al final). Con un thread ocupado la respuesta es `409`. `on_completion: "delete"`, `on_disconnect: "cancel"` e `if_not_exists: "create"` cambian el ciclo de vida.

## 17.4 Threads, historial y viaje en el tiempo

```go
hist, _ := c.ThreadHistory(ctx, th.ThreadID, 20, "")            // más nuevo primero
c.PatchThread(ctx, th.ThreadID, agentprotocol.ThreadPatch{
	Checkpoint: &agentprotocol.ThreadCheckpoint{CheckpointID: hist[3].Checkpoint.CheckpointID},
	Messages:   []agentprotocol.Message{{Role: "user", Content: "En realidad, ¿y en Oporto?"}},
})                                                                // hace fork desde ese punto
cp, _ := c.CopyThread(ctx, th.ThreadID)                           // copia independiente
found, _ := c.SearchThreads(ctx, agentprotocol.ThreadSearchRequest{Metadata: map[string]any{"user": "ana"}})
```

Cancelar: `c.CancelRun(ctx, id, "interrupt", true)` mantiene el progreso; `"rollback"` devuelve el estado al de antes de la run.

## 17.5 Streaming por thread: eventos tipados

El protocolo de streaming está **centrado en el thread**. Abres un stream filtrado y envías comandos; los eventos llegan con `seq` creciente.

```go
filter := agentprotocol.Filter{Channels: []string{"lifecycle", "messages", "tools", "input"}}
go c.Command(ctx, "t1", "run.start", map[string]any{"assistantId": "bot", "input": "hola"})
for ev, err := range c.Events(ctx, "t1", filter) {
	data := ev.Params["data"].(map[string]any)
	switch ev.Method {
	case "messages": // message-start, content-block-start/delta/finish, message-finish
	case "tools":    // tool-started, tool-finished, tool-error (toolCallId liga con el bloque del mensaje)
	case "lifecycle":// started, running, completed, failed, interrupted
	}
}
```

Para reconectar sin perder nada: `filter.Since = ultimoSeq` (o el header `Last-Event-ID`) reenvía desde el buffer y sigue en vivo. Ejemplo completo: `go run ./examples/agentprotocol/human_in_the_loop`.

## 17.6 Aprobación humana

Un agent con `HumanInTheLoop` (cap. 9) o `graph.Interrupt` (cap. 8) **se pausa**: el thread pasa a `interrupted` y el canal `input` emite `input.requested` con `interruptId` y `payload`. Responde con:

```go
c.Command(ctx, "t1", "input.respond", map[string]any{
	"namespace": []string{}, "interruptId": id,
	"response": []map[string]any{{"type": "approve"}},   // el valor que espera el interrupt
	// opcionales: "update": {...estado...}, "goto": "no" o {"node": "x", "input": {...}}
})
```

(`response` se entrega al `Interrupt`; los ids inválidos vuelven como error `no_such_interrupt`.) Por la API REST, enviar un nuevo `run` con `input` en un thread interrumpido también lo reanuda.

Los mensajes que llegan durante una run en curso (`run.start` o `input.inject`) quedan **en cola** y se ejecutan justo después de la actual.

## 17.7 WebSocket

`GET /threads/{id}/stream` con `Upgrade: websocket` intercambia comandos y eventos en la misma conexión (`subscription.subscribe`, `unsubscribe`, `reconnect` + `run.start`, `input.respond`, `state.*`). El servidor es un RFC 6455 mínimo (texto, fragmentación, ping/pong; sin compresión). Para el navegador usa cualquier cliente WebSocket y los mismos mensajes JSON `{"id": 1, "method": "...", "params": {...}}`.

## 17.8 Memoria a largo plazo (`/store`)

```go
c.PutItem(ctx, []string{"users", "ana"}, "prefs", map[string]any{"units": "metric"})
items, _ := c.SearchItems(ctx, agentprotocol.StoreSearchRequest{NamespacePrefix: []string{"users"}, Filter: map[string]any{"units": "metric"}})
```

Es el mismo `store.Store` de los grafos (`graph.WithStore`): pásalo en `Options.Store` y los nodos del agent leen lo que escribieron los clientes.

## 17.9 Producción

- **Autenticación**: `APIKey` única; para multiusuario pon un gateway delante y propaga la identidad en `config.configurable` (p. ej. `user_id`), que llega a las tools y a `langmem` (cap. 16).
- **Réplicas**: el buffer de eventos y el registro de runs son por proceso; usa afinidad por `thread_id`.
- **Límites**: el cuerpo de la solicitud está limitado a 16 MB; los consumidores de SSE lentos se desconectan en vez de bloquear la run.
- **Observabilidad**: las runs pasan por los callbacks de `core/callbacks` (cap. 12); cada run lleva `run_id`, `thread_id` y `agent_id` en la metadata.
- **Lo que falta**: el evento `tasks` y un buffer de eventos compartido entre réplicas. Los subgrafos ya aparecen como namespaces hijos, y las runs terminadas persisten con `Options.RunStore = agentprotocol.NewStoreRunStore(st)` (ver el documento de referencia).

Ejemplos: `go run ./examples/agentprotocol/serve_and_call` y `.../human_in_the_loop`.

---

Anterior: [16. Memoria a largo plazo](16-memoria-langmem.md) · Siguiente: [18. ciam](18-ciam-contexto.md) · [Índice del manual](README.md)
