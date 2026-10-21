# Arquitectura

[English](../en-US/ARCHITECTURE.md) | [Português](../pt-BR/ARCHITECTURE.md) | **Español**

## Capas

```
deepagents ──► agents (create_agent + middleware) ──► langgraph/graph (Pregel)
                    │                                      │
                    └──► langgraph/prebuilt (ToolNode)     ├─ channels  (slots de estado + reducers)
                                                           ├─ checkpoint (Saver)
core (messages, models, tools, runnables, …)  ◄────────────┴─ store (memoria a largo plazo)
```

## Runtime de LangGraph (`langgraph/graph`)

* **El estado** es un `map[string]any`; cada clave es un canal declarado en un `Schema`
  (`LastValue` por defecto, o `graph.Messages()`, `graph.Append()`, `graph.Sum()`,
  cualquier reducer `channels.BinOp`). `graph.NewTyped[T]` es una capa tipada opcional:
  el schema sale de las tags de la struct (`graph:"key,reducer=messages|append|sum"`),
  los nodos reciben `T` y devuelven actualizaciones parciales verificadas contra las tags.
* **La ejecución** es bulk-síncrona: cada super-paso ejecuta todas las tasks agendadas
  en concurrencia sobre el mismo snapshot del estado, aplica sus escrituras mediante los reducers
  en el orden de las tasks y luego enruta (aristas estáticas, barreras `AddJoin`,
  routers condicionales, `Command{Goto,Send}`, fan-out con `Send`).
* **Los nodos** devuelven `nil`, una actualización de `State` o un `Command`.
* **Persistencia**: con `WithCheckpointer`, se guarda un checkpoint tras la entrada y
  en cada paso. Los threads se seleccionan con `runnables.WithConfig(ctx, graph.Thread(id))`;
  están disponibles `GetState`, `GetStateHistory`, `UpdateState` (y `checkpoint_id` para hacer fork /
  viaje en el tiempo). Los checkpoints se indexan por (thread, namespace): el grafo
  raíz usa `""`, un subgrafo ejecutado por la task `node:idx` usa `parent|node:idx`.
  Durabilidad (`WithDurability` / `WithRunDurability`): `sync` (predeterminado, guarda en cada paso),
  `async` (guardados en segundo plano, ordenados, vaciados antes de que `Invoke` retorne), `exit` (guarda
  solo en la pausa, al final o al fallar).
* **Human-in-the-loop**: `graph.Interrupt(ctx, v)` pausa; la ejecución devuelve el estado
  con `graph.Interrupts(state)`. Reanuda con `Invoke(ctx, graph.Command{Resume: x})`.
  El nodo se reejecuta desde su inicio, así que los efectos secundarios antes de `Interrupt` deben ser
  idempotentes. `InterruptBefore/After` pausan de forma estática. Las tasks hermanas ya terminadas de un
  paso interrumpido no se vuelven a ejecutar.
* **Streaming**: `StreamEvents(ctx, in, ModeValues|ModeUpdates|ModeCustom|ModeMessages)`.
  Los nodos emiten eventos personalizados con `graph.StreamWriter(ctx)`. Con `graph.Subgraphs()`,
  los eventos de los grafos anidados se reenvían con `Event.Namespace` establecido.
* **Subgrafos**: invoca un grafo compilado dentro de un nodo. Un `Interrupt` interno se propaga
  a la ejecución externa y se reanuda correctamente, con o sin el checkpointer propio del subgrafo
  (su checkpoint vive en su namespace).

## Agents

`agents.Create` construye `before_agent → [before_model → model → after_model] ⇄ tools →
after_agent`. El middleware implementa cualquiera de `BeforeAgent`, `BeforeModel`, `WrapModel`,
`AfterModel`, `WrapTool`, `AfterAgent`, más `Tools()` y `StateSchema()` opcionales.
Los hooks redirigen el flujo estableciendo `jump_to` (`agents.JumpEnd|JumpTools|JumpModel`).
Incluidos: límites de llamadas a modelo/tools, fallback, retry, resumen, edición de contexto,
lista de tareas, eliminación de PII, human-in-the-loop.

## Deep Agents

`deepagents.Create` = planificación (`write_todos`) + tools de filesystem virtual
(`ls read_file write_file edit_file glob grep`, `execute` para backends de sandbox) +
subagents mediante `task` + resumen + memoria opcional (`AGENTS.md`), skills
(`SKILL.md`) y HITL. Backends: `State` (estado del grafo), `Store` (entre threads),
`Filesystem` (disco, confinado a la raíz, con verificación de symlinks), `LocalShell` (añade `execute`,
**ejecuta comandos en el host — solo para uso confiable**), `Composite` (enruta por prefijo).

## Persistencia y operación

* `checkpoint.Memory` para pruebas; `checkpoint/sqlsaver` para SQLite/PostgreSQL mediante
  `database/sql` (el driver lo aportas tú). Los valores son JSON: registra los tipos de estado
  propios con `checkpoint.Register` (mensajes, `graph.Send`, archivos, todos, tasks
  asíncronas y evaluaciones de rúbrica ya vienen preregistrados). Los escalares de Go (`int64`, `uint*`,
  `float32`, `time.Time`, `time.Duration`, `[]byte`) conservan su tipo; los números JSON sin tipo
  vuelven como `int` o `float64`. `checkpoint.Strict(true)` hace que guardar una struct
  no registrada sea un error, en lugar de un `map[string]any` silencioso.
  `internal/sqlitetest` ejecuta el saver contra SQLite real
  (`go test ./internal/sqlitetest/...`; el driver es una dependencia solo de pruebas).
* **Caché de nodos**: `graph.WithCachePolicy` en un nodo + `graph.WithCache(graph.NewMemoryCache())`.
* **Nodos diferidos**: `graph.Deferred()` ejecuta un nodo solo cuando no hay otra task pendiente.
* **LangSmith**: `tracing/langsmith` es un `callbacks.Handler` que envía runs
  (`callbacks.WithHandlers(ctx, tracer)`), en modo best effort, con cola asíncrona; `Close()` vacía la cola.
* **Grafos remotos**: `remote.Graph` ejecuta un assistant en un Agent Server (LangGraph
  Platform) tras la misma API `Invoke/Stream`; `remote.Client` expone threads y runs.
* **Extras de Deep Agents**: `deepagents/middleware.Rubric` (bucle con evaluador),
  `backends.WithPermissions` / `Options.Permissions` (permitir/denegar por operación y
  glob de ruta, también aplicados a los subagents), `middleware.AsyncSubagents` con
  `LocalRunner` (goroutines) o `RemoteRunner` (Agent Server) y las tools de tareas
  `start/check/update/cancel/list`.

## Integraciones (`integrations/`, `providers/`)

* **Un contrato pequeño por tipo de backend**, así que un servicio nuevo son unos pocos métodos:
  `vectorstore.Backend` (`Upsert`, `Query`, `Delete`; `vectorstore.New(embedder, backend)` añade
  embedding, filtros y MMR), `checkpoint.Saver` (4 métodos), `store.Store`, `retrievers.Retriever`,
  `backends.Backend` / `backends.Executor` para Deep Agents.
* **Router.** `providers/router` es un `ChatModel` sobre una lista ordenada de destinos (proveedores nativos o gateways compatibles con OpenAI) con failover ante errores recuperables, entrada round-robin/random/ponderada y continuación del streaming; `chatmodels.Init("router:<model>")` lo construye desde las variables `ROUTER_*`.
* **Sin SDKs.** Los servicios HTTP/JSON usan `net/http`; los protocolos de cable que no tenían forma HTTP se implementan
  directamente con la stdlib: RESP (Redis), MongoDB OP_MSG + BSON + SCRAM-SHA-256, Bolt/PackStream
  (Memgraph), el WebSocket de exec de Kubernetes, autenticación Digest (Infinispan) y AWS SigV4 (`providers/bedrock.Sign`,
  reutilizado por DynamoDB, S3, Kendra, Lambda). Los backends SQL reciben un `*sql.DB`; el driver lo elige quien llama.
* **Una suite de conformidad** (`internal/conformance`: `Saver`, `Store`, `Vector`) se ejecuta contra cada backend,
  en memoria o real, para que todos se comporten como las implementaciones de referencia. Las pruebas que necesitan un servicio real
  están controladas por variables de entorno y se omiten solas; las que necesitan un driver SQL viven en el paquete `internal/pgtest`
  (dependencias de driver solo para pruebas), para que el código de la biblioteca en sí no importe paquetes de terceros.
* **`openai.Chat` también es la base de dialectos** de la API Chat Completions. `Path`, `StreamPath`, `Query`,
  `TokenFunc` (token OAuth/IAM por solicitud) y `Rewrite` (editar el cuerpo) permiten que IBM watsonx.ai reutilice
  todo el pipeline de solicitud/stream/tool-call en lugar de duplicarlo.
* **`backends.Remote` convierte "ejecutar un comando de shell en algún lugar" en un backend completo de Deep Agents**
  (read, write, edit, ls, glob, grep, execute) con fragmentos de shell POSIX. `kubernetes.Sandbox` aporta el runner
  para un pod desechable; cualquier otro runner aislado (contenedor, VM, sandbox en la nube) se conecta de la misma forma.
* **Recuperación consciente de permisos** (`spicedb.FilterRetriever`, `permit.FilterRetriever`) comprueba cada
  documento recuperado contra el motor de políticas y falla en modo cerrado: un id ausente o una comprobación
  fallida nunca filtran contenido.
* **Protocolo de frontend**: `agui.Handler` mapea los eventos de stream del grafo (`messages`, `updates`, `values`) a
  eventos AG-UI por SSE (`RUN_STARTED`, `TEXT_MESSAGE_*`, `STEP_*`, `STATE_SNAPSHOT`, `RUN_FINISHED`/`RUN_ERROR`).
* **Niveles de verificación honestos** se registran por integración en `docs/es-ES/INTEGRATIONS.md` (servicio real
  frente a prueba de contrato) y la cobertura de proveedores en `docs/es-ES/PROVIDERS.md` / `docs/es-ES/PROVIDERS-ANALISE.md`.

## Diferencias respecto a las bibliotecas de Python

* El estado es un `map[string]any`, no un modelo TypedDict/Pydantic; usa `graph.NewTyped[T]`
  o `tools.Decode` para trabajar con structs.
* No portado: Studio, build/deploy de la CLI, los backends gestionados de sandbox/context-hub de LangSmith y
  experimentos alojados. Los evals offline están en `evals/`; `langgraph/dev`
  sirve grafos mediante Agent Protocol para desarrollo.

## langmem

`langmem/` está junto a `agents/`: depende de `langgraph/store`, `langgraph/graph`, `core/{models,tools,embeddings}` y `agents` (para `Middleware`), y nada depende de él.

- **Los tipos de memoria son tools.** Cada `Schema` se ofrece al modelo como una tool (el equivalente en Go de los schemas Pydantic de LangMem entregados a trustcall); las actualizaciones repiten `json_doc_id`, las eliminaciones usan `RemoveDoc`. Así se mantiene un único protocolo de extracción para todo tipo de memoria y todo proveedor con tool calling.
- **Núcleo puro, envoltorio con estado.** `Manager.Extract` recibe y devuelve memorias; `StoreManager` lo envuelve con recuperación, seeding, fases y escrituras mínimas (solo los elementos cambiados se escriben con `Put`).
- **Búsqueda semántica sin cambiar `store.Store`.** `IndexedStore` guarda el vector dentro del valor del elemento, así que funciona sobre cualquier backend existente (memory, sql, redis, mongo, dynamodb); `Query` degrada a un ranking por palabras clave cuando el store no es un `SemanticSearcher`.
- **Los namespaces son plantillas** que se resuelven desde `runnables.Config.Configurable`; una variable ausente es un error, nunca un namespace compartido.
- **Los resúmenes son incrementales y no destructivos**: el modelo ve `summarized_messages`, `messages` sigue completo, y `RunningSummary` viaja en el estado con checkpoint.
- **La formación en background** es un temporizador con debounce por thread, no un worker de cola; ver docs/es-ES/LANGMEM.md sobre la brecha del ejecutor remoto.

## knowledge, rag, cag, kag

- **knowledge** define `Answerer` (entra una pregunta, sale una respuesta con fuentes) y el `Router` que elige una técnica por pregunta. No importa ninguna técnica, así que cualquier `Answerer` puede enrutarse.
- **rag** compone `core/retrievers` e `integrations/retrieval`: un `Indexer` incremental (hash de contenido por fuente) y un `Pipeline` que fusiona resultados vectoriales y BM25 con RRF.
- **cag** pone el corpus completo en un prefijo de sistema estable en bytes y deja que el proveedor lo cachee (`cache_control` para Anthropic, caché automática de prefijo en los demás); `messages.Usage` informa `CacheReadTokens` y `CacheCreationTokens`.
- **kag** mantiene un grafo de conocimiento detrás de la interfaz `GraphStore` (implementación de referencia en memoria, suite de conformidad compartida). Un extractor con LLM lo construye; el `Engine` planifica los saltos, recorre el grafo y sintetiza a partir de las tripletas. Los tipos de relación que llegan del modelo se normalizan antes de poder llegar a un lenguaje de consulta. `integrations/graphstore` implementa `GraphStore` para Neo4j (Query API por HTTP) y Memgraph (Bolt); ambos validan etiquetas y tipos de relación antes de interpolarlos en Cypher y se comprueban con la misma suite de conformidad que el store en memoria.
- **knowledge.Toolkit** convierte los answerers en valores `tools.Tool` y los sirve por `mcp/server`, siguiendo el patrón de `ciam.Toolkit`; `cmd/knowledge` es la CLI encima.

## agentprotocol, ciam, openswe

- **agentprotocol** sirve grafos compilados. La unidad es el thread: un `hub` por thread mantiene un ring buffer de eventos de streaming tipados (`seq` monótono, replay por `since`), alimentado por un traductor que mapea eventos del grafo y callbacks a los canales del protocolo; los handlers REST, SSE, los comandos y un pequeño WebSocket RFC 6455 leen todos el mismo hub. Los registros de thread viven en memoria y se reconstruyen desde el checkpointer; los registros de run pasan por un `RunStore` (memoria por defecto, `NewStoreRunStore` sobre cualquier `store.Store` para sobrevivir a reinicios). Los eventos de subgrafos conservan su namespace, así que los filtros `namespaces`/`depth` y `agent.getTree` ven los grafos hijos. Se añadió `graph.Compiled.Saver()` para que el servidor pueda listar, copiar y borrar checkpoints.
- **ciam** guarda chunks, hashes de archivos y memorias en cualquier `store.Store`; la búsqueda carga el corpus del proyecto una sola vez (estadísticas de BM25 + vectores) y fusiona coseno y BM25 con RRF (coseno por escaneo, o mediante un índice vectorial opcional como `vectorstore.NewHNSW`). `ciam api` sirve las mismas tools por HTTP simple. Las memorias son memorias de `langmem`. Las capacidades son primero valores `tools.Tool` y después se sirven por `mcp/server`, así que los agents en proceso y los editores comparten una sola implementación.
- **openswe** es composición: `deepagents` + un backend de sandbox por thread + middleware de guardas + tools de GitHub. Las reglas se aplican dos veces (en el prompt y en el código). El estado duradero (hallazgos, vigilancias, automatizaciones, vínculos PR<->thread, ids de sandbox) pasa por `store.Store`, nunca por una base de datos propia; el trabajo de un thread lo serializa el `triggers.Dispatcher` (una run activa por thread, cola para el resto). Slack y Linear son adaptadores sobre el mismo dispatcher; sus respuestas pasan por envoltorios `Notifier` alrededor del runner.

## compress

`compress/` es un paquete hoja (solo biblioteca estándar). Responde cuánto comprimir y cuándo; los agents aplican la respuesta.

- **Un decider, no un umbral.** Un `Decider` responde preguntas tipadas sobre un `State` construido a partir de un `Summary`, nunca sobre el historial bruto, así que un modelo de ventana pequeña puede decidir. `RuleDecider` implementa la misma interfaz con umbrales. `Guarded` envuelve cualquier decider para que errores, timeouts y respuestas de baja confianza caigan en las reglas, pregunta por pregunta.
- **La compresión es una copia.** `middleware.Compression` (`agents/middleware`) transforma la copia por llamada en un `WrapModelCall`: Caveman sobre los turnos Human y AI antiguos, la directiva Ponytail y la directiva de respuesta breve tras el último mensaje del usuario, y resultados JSON de herramientas reescritos en TOON cuando son estrictamente más cortos. Las llamadas a herramientas nunca se tocan, Caveman nunca toca resultados de herramientas, y el estado guardado nunca se reescribe.
- **Ponytail y TOON.** `compress.PonytailDirective` es un texto de reglas (sin modelo, sin estado); `compress.TOONFromJSON` y `TOONShorter` codifican JSON (solo biblioteca estándar, sin decodificador). Ambos los decide el mismo `Decider` que las otras palancas. Vea [COMPRESSION.md](COMPRESSION.md).
- **Ruta del shell aparte.** `compress.RTK` decide antes de que se ejecute un comando y llega a `backends.LocalShell` por su hook `Rewrite`, tras la comprobación de `AllowedCommands` sobre el comando original.
- **Módulos.** `HTTPDecider` habla con el servidor Laya por `net/http`. El decider laya-go nativo necesita cgo y ONNX Runtime. El `go.mod` raíz requiere laya-go, pero solo la build tag `laya_native` lo compila (`examples/langgraph/full_team/native`); `examples/langgraph/decider_laya_native` es un módulo Go separado. Véase [COMPRESSION.md](COMPRESSION.md).
