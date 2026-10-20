# Agent Protocol en Go (`agentprotocol/`)

[English](../en-US/AGENT-PROTOCOL.md) | [Português](../pt-BR/AGENT-PROTOCOL.md) | **Español**

Implementación del [Agent Protocol](https://github.com/langchain-ai/agent-protocol) (API v0.1.6 + *Agent Streaming Protocol*) sobre los grafos compilados de este proyecto. Un grafo servido aquí puede ser operado por cualquier cliente del protocolo, incluido el `agentprotocol.Client` de este paquete. Solo stdlib.

Tutorial: [manual/17-agent-protocol.md](manual/17-agent-protocol.md). Ejemplos: [`examples/agentprotocol/`](../../examples/agentprotocol/).

El paquete `remote/server` (ya existente) sirve el dialecto de LangGraph Platform (`/threads/{id}/runs/...`). `agentprotocol` sirve el protocolo abierto (`/runs`, `/threads`, `/agents`, `/store`, streaming por thread). Los dos coexisten; elige según el cliente que necesites atender.

## Lo que define el protocolo

| Recurso | Para qué | Endpoints |
|---|---|---|
| **Agents** | introspección: nombre, capacidades, schemas JSON | `POST /agents/search`, `GET /agents/{id}`, `GET /agents/{id}/schemas` |
| **Threads** | conversaciones multi-turno con estado persistente | `POST /threads`, `POST /threads/search`, `GET/PATCH/DELETE /threads/{id}`, `GET /threads/{id}/history`, `POST /threads/{id}/copy` |
| **Runs** | ejecuciones de agent (en background, esperando o en stream) | `POST /runs`, `/runs/wait`, `/runs/stream`, `/runs/search`, `GET/DELETE /runs/{id}`, `GET /runs/{id}/wait`, `GET /runs/{id}/stream`, `POST /runs/{id}/cancel` |
| **Store** | memoria a largo plazo entre threads | `PUT/GET/DELETE /store/items`, `POST /store/items/search`, `POST /store/namespaces` |
| **Streaming** | eventos ordenados por thread, filtrables, con replay | `POST /threads/{id}/stream` (SSE), `GET /threads/{id}/stream` (WebSocket), `POST /threads/{id}/commands` |

Los 27 endpoints del OpenAPI están implementados.

## Servir

```go
srv, _ := agentprotocol.New(agentprotocol.Options{
    Agents: []agentprotocol.AgentSpec{{ID: "bot", Name: "Bot", Description: "...", Graph: compiled}},
    APIKey: os.Getenv("AP_KEY"),   // opcional: x-api-key o Authorization: Bearer
    Store:  store.NewMemory(),      // /store/*; reemplázalo por sqlstore/redis/mongo
})
http.ListenAndServe(":8123", srv)
```

El grafo necesita un **checkpointer** para tener estado de thread (`graph.WithCheckpointer`). Sin él, las runs funcionan pero los threads no guardan estado, y `history`/`state.*`/resume no tienen nada que leer.

`AgentSpec` acepta `InputSchema/OutputSchema/StateSchema/ConfigSchema`; sin ellos el servidor responde un schema de chat (`messages`). Varios agents: el cuerpo de la run indica `agent_id` (con un solo agent registrado es el predeterminado).

## Modelo de ejecución

- **Thread** = identidad duradera (estado en el checkpointer) + registro en memoria (metadatos, estado, buffer de eventos). Estados: `idle`, `busy`, `interrupted`, `error`.
- **Run** en background en una goroutine; estado `pending` hasta terminar, luego `success`, `error` o `interrupted` (cancelada o pausada por `Interrupt`).
- **Una run activa por thread**: otra `POST /runs` en el mismo thread responde `409`. (Mediante el protocolo de streaming, `run.start` durante una run **encola** la entrada para ejecutarla justo después.)
- **Thread interrumpido**: la siguiente `run` con `input` **reanuda** el grafo (`Command{Resume}`). El valor de reanudación es `input.resume`, si existe; si no, las `messages`, si eso es todo; si no, el objeto `input` completo. Para responder por `interruptId` (con `update`/`goto`), usa el comando `input.respond`.
- **Run sin `thread_id`** (stateless): thread efímero, borrado (con su checkpoint) al final.
- `on_completion: delete` borra la run; `on_disconnect: cancel` la cancela si el cliente desaparece (predeterminado `continue`); `webhook` recibe `POST {run, values, messages}` al terminar; `if_not_exists: create` crea el thread en el momento (predeterminado `reject`).
- **Persistencia tras reinicio**: un `GET /threads/{id}` reconstruye el thread desde el checkpointer. Los registros de runs se escriben en cada cambio de estado en un `RunStore` (`Options.RunStore`): el predeterminado (`NewMemoryRunStore`) los mantiene solo en memoria, y `NewStoreRunStore(store)` los escribe en un `langgraph/store.Store` (namespace `["agentprotocol","runs",threadID]`), que sobrevive al reinicio si el store es durable. Tras un reinicio, `GET /runs/{id}` y `POST /runs/search` devuelven esas runs; `DELETE` las quita del store. Una run que seguía `pending` cuando murió el proceso se marca como `error` (con el mensaje `server stopped before the run finished`) al iniciar, porque el protocolo no tiene estado de fallo por caída. El buffer de eventos (`GET /runs/{id}/stream`, streaming de thread) sigue solo en memoria: tras un reinicio, una run restaurada no tiene eventos que reenviar. Cancelar o hacer rollback de una run restaurada no actúa sobre el grafo (la run ya no está en ejecución).

### Mensajes

`Message{role, content, id, metadata}` con `content` string o bloques. La entrada acepta `user|human`, `assistant|ai`, `system`, `tool`; los bloques que no son texto se convierten en su texto. La salida usa los roles de LangChain (`human`, `ai`, `system`, `tool`) y `metadata.tool_calls` en los mensajes `ai`.

### Viaje en el tiempo

- `GET /threads/{id}/history?limit&before` — revisiones, de la más nueva a la más antigua (`before` = id de checkpoint).
- `PATCH /threads/{id}` con `checkpoint` + `values`/`messages` — **hace fork** desde ese checkpoint (pasa a ser la nueva punta).
- `POST /threads/{id}/copy` — copia independiente del historial.
- `POST /runs/{id}/cancel?action=rollback` — cancela y restaura el estado de antes de la run (fork al checkpoint anterior; **los checkpoints de la run no se borran**).

## Streaming por thread

Canales: `values`, `updates`, `messages`, `tools`, `lifecycle`, `input`, `checkpoints`, `custom` (y `custom:*`); `tasks` se acepta pero nunca se emite.

| Canal | Eventos (`params.data.event`) |
|---|---|
| `lifecycle` | `started`, `running`, `completed`, `failed`, `interrupted` (+ `graphName`, `error`) |
| `messages` | `message-start` → `content-block-start` → `content-block-delta` (`text-delta`) → `content-block-finish` → … → `message-finish` (con `usage`); bloques `tool_call` finalizados |
| `tools` | `tool-started` (`toolCallId`, `toolName`, `input`), `tool-finished` (`output`), `tool-error` |
| `input` | método `input.requested` con `interruptId` y `payload` |
| `values` / `updates` | estado completo / `{node, values}` por nodo |
| `checkpoints` | `{id, parentId, step, source}` en cada super-paso |

Garantías: cada evento tiene `seq` monótono y `eventId` (`evt_<seq>`); **los mensajes nunca se entrelazan** y todo `message-start` se cierra con `message-finish`; el `toolCallId` de `tool-started/finished` es el **mismo id** del bloque `tool_call` del mensaje, incluso cuando la herramienta corre después de una aprobación humana (reanudación); una run reanudada **no reenvía** los mensajes ya anunciados. Los modelos que no hacen streaming (solo responden entero) también generan los eventos de mensaje, a partir de `updates`.

**SSE**: `POST /threads/{id}/stream` con `{channels, namespaces?, depth?, since?}`. `since` (o el header `Last-Event-ID`) reenvía desde el buffer circular (predeterminado 2000 eventos por thread) y sigue en vivo, sin perder ni duplicar. El thread se crea bajo demanda si el stream se abre antes de la primera run. Comentario `: keepalive` cada 15 s. Un consumidor lento se desconecta (en lugar de bloquear la run).

**Comandos** (`POST /threads/{id}/commands` o en el WebSocket): `run.start`, `input.respond` (por `interruptId`, con `update` y `goto`), `input.inject`, `state.get`, `state.listCheckpoints`, `state.fork`, `agent.getTree`. Respuestas `{"type":"success","id","result","meta":{"appliedThroughSeq"}}` o `{"type":"error","id","error": <código>}` con los códigos del protocolo (`invalid_argument`, `unknown_command`, `no_such_interrupt`, `no_such_checkpoint`, `no_such_subscription`, `not_supported`...).

**WebSocket** (`GET /threads/{id}/stream`): RFC 6455 mínimo — texto, fragmentación, ping/pong, close; sin extensiones ni compresión. Comandos `subscription.subscribe/unsubscribe/reconnect` en la misma conexión; las suscripciones persisten entre runs.

## Cliente

`agentprotocol.NewClient(url, apiKey)`: `SearchAgents`, `GetAgent`, `GetAgentSchemas`, `CreateThread`/`GetThread`/`SearchThreads`/`PatchThread`/`DeleteThread`/`CopyThread`/`ThreadHistory`, `CreateRun`/`CreateAndWait`/`WaitRun`/`GetRun`/`SearchRuns`/`CancelRun`/`DeleteRun`, `StreamRun` (SSE mediante `iter.Seq2`), `Events(thread, Filter)`, `Command(thread, method, params)`, `PutItem`/`GetItem`/`DeleteItem`/`SearchItems`/`ListNamespaces`. Los errores HTTP se convierten en `*StatusError{Status, Code, Message}`.

## Diferencias y límites (sin maquillaje)

- **Streaming de run (REST)**: `POST /runs/stream` emite `event: <modo>` (`metadata`, `values`, `updates`, `messages`, `custom`, `end`); en el modo `messages` el dato es `{chunk, node}`. El formato exacto del SDK de Python de LangGraph Platform puede diferir.
- **Namespaces**: el streaming pide `graph.Subgraphs()`, así que los eventos de subgrafos llegan con `namespace` = ruta del subgrafo (`["sub:0"]`, un elemento `nodo:id` por nivel; raíz = `[]`). Los filtros `namespaces` y `depth` del SSE aplican sobre esa ruta, y `agent.getTree` arma `children` a partir de los namespaces vistos en las runs del thread. Límites: el `status` de cada hijo es el de la última run del thread (no hay estado por subgrafo), y el historial de eventos sigue el buffer circular del thread.
- **`run.start` con una run activa** encola (no inyecta input en el grafo en ejecución: el runtime no lo soporta).
- **`state.fork`** crea un **nuevo thread** con el historial hasta el checkpoint, y se ejecuta si hay `input`.
- **Timestamps de checkpoint** son los del momento en que el servidor los vio (no los persiste el checkpointer); los checkpoints anteriores a un reinicio usan la fecha de creación del thread.
- **Sin multiproceso**: el buffer de eventos y las runs en ejecución viven en el proceso, y la recuperación marca como `error` todo registro `pending` del store al iniciar, lo que asume un único proceso por store. Varias réplicas sobre el mismo store se estorbarían; usa afinidad por thread o un servidor por thread. Aquí no hay Redis ni otro backend distribuido. Para varias réplicas faltarían tres piezas: un log de eventos compartido (con `seq` global por thread), un lease de las runs en ejecución (para que la recuperación no marque como error runs vivas de otra réplica) y enrutar la cancelación y los comandos a la réplica dueña de la run.
- **Retención**: como máximo 5000 runs finalizadas por proceso; al superarlo, la mitad más antigua se borra también del `RunStore`. Las runs de un proceso anterior no entran en esa cuenta y no se podan.
- **Sin métricas ni límites de tasa**; `APIKey` es una clave única. Pon un gateway delante para multi-tenant.
- `GET /store/items` acepta `namespace` repetido (`?namespace=a&namespace=b`) y el formato `a.b` de los SDKs; los nombres de namespace con punto necesitan el primer formato.

## Pruebas

`go test -race ./agentprotocol/`: 100% offline (`httptest`, modelos *fake*). Cubren introspección, ciclo de vida de threads (history/patch/fork/copy/search/delete), runs (wait, background, stateless, 409, cancel, webhook, `on_completion`), SSE de run, store, API key, el protocolo de streaming de extremo a extremo (orden y cierre de mensajes, `toolCallId`, filtro por canal, replay, interrupción + `input.respond`, comandos y códigos de error), run encolada, **WebSocket** con cliente de bajo nivel (fragmentación, subscribe/unsubscribe/reconnect) y estado que sobrevive a un reinicio.
