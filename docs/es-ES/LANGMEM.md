# LangMem en Go (`langmem/`)

[English](../en-US/LANGMEM.md) | [Português](../pt-BR/LANGMEM.md) | **Español**

Port de [LangMem](https://github.com/langchain-ai/langmem) (v0.0.30, MIT) integrado al ecosistema de este proyecto: usa el `langgraph/store`, el `agents.Create` con middleware, el `graph.StateGraph`, las tools y los chat models ya existentes. Solo stdlib.

Tutorial paso a paso: [manual/16-memoria-langmem.md](manual/16-memoria-langmem.md). Ejemplos: [`examples/langmem/`](../../examples/langmem/).

## Modelo mental

Toda operación de memoria de LangMem sigue el mismo patrón: **recibe conversaciones + estado actual de la memoria → pide a un LLM que expanda/consolide → devuelve el nuevo estado.** Tres preguntas definen tu sistema: *qué* recordar, *cuándo* formar la memoria y *dónde* guardarla.

| Tipo | Sirve para | Representación | API |
|---|---|---|---|
| Semántica | hechos, preferencias | colección de ítems o **perfil** (1 documento) | `StoreManager`, `ManageMemory` |
| Episódica | experiencias pasadas, few-shot | colección de `Episode` | `StoreManager` + `EpisodeSchema` |
| Procedural | cómo se comporta el agent | el propio system prompt | `PromptOptimizer`, `MultiPromptOptimizer` |
| Corto plazo | caber en el contexto | resumen incremental | `SummarizeMessages`, `SummarizationNode` |

| Cuándo formar | Latencia | Cómo |
|---|---|---|
| **Hot path** (el agent decide, durante la conversación) | mayor | tools `ManageMemory` + `SearchMemory` |
| **Background** (después de la conversación, con debounce) | ninguna | `StoreManager` + `ReflectionExecutor` |

## Mapa Python → Go

| LangMem (Python) | Go (`langmem`) | Observación |
|---|---|---|
| `create_memory_manager` | `NewManager(model, opts...)` → `Extract(ctx, msgs, existing, maxSteps)` | función pura, sin store |
| `create_memory_store_manager` | `NewStoreManager(model, namespace, opts...)` → `Process` / `Invoke` | persiste solo lo que cambió |
| `create_manage_memory_tool` | `ManageMemory(ToolConfig)` | `Actions`, `Schema`, `Instructions`, `Name` |
| `create_search_memory_tool` | `SearchMemory(ToolConfig)` | `Artifact: true` = `content_and_artifact` |
| `create_memory_searcher` | `MemorySearcher{...}.Search` | el LLM escribe las consultas |
| `create_thread_extractor` | `ExtractThread[T]` | schema genérico (`ThreadSummary` por defecto) |
| `create_prompt_optimizer(kind=...)` | `NewPromptOptimizer(model, kind, cfg)` | `Gradient`, `Metaprompt`, `PromptMemory` |
| `create_multi_prompt_optimizer` | `MultiPromptOptimizer` | clasificador + optimización concurrente |
| `Prompt` (TypedDict) | `Prompt{Name, Prompt, UpdateInstructions, WhenToUpdate}` | |
| `AnnotatedTrajectory` | `Trajectory{Messages, Feedback}` | feedback: string, map o nil |
| `summarize_messages` | `SummarizeMessages` | algoritmo portado fielmente |
| `SummarizationNode` | `SummarizationNode{...}.Node()` + `SummarizationSchema()` | |
| `RunningSummary` | `RunningSummary` | |
| `ReflectionExecutor` (local) | `ReflectionExecutor` | `Submit` → `*Future`; `Shutdown` |
| `NamespaceTemplate` | `Namespace.Resolve(ctx)` | las variables vienen de `Configurable` |
| `InMemoryStore(index=...)` | `NewIndexedStore(store, embedder)` | véase "Diferencias" |
| schemas `pydantic` | `SchemaFor[T](name, desc)` | JSON Schema por reflexión de struct |
| — (receta de las guías) | `Middleware` | inyecta memorias + tools + formación en background en el `agents.Create` |

## API en detalle

### Schemas y memorias

`Schema{Name, Description, Parameters}` describe un *tipo* de memoria; cada schema se convierte en una **tool** que se ofrece al modelo (como en LangMem, donde los modelos Pydantic se vuelven tools de trustcall). Un manager puede mezclar tipos.

- `MemorySchema` (predeterminado): texto libre, guardado como `{"content": "..."}`.
- `EpisodeSchema` (`Episode{Observation, Thoughts, Action, Result}`), `TripleSchema` (`Triple{Subject, Predicate, Object, Context}`).
- `SchemaFor[T]("Nombre", "descripción")` para los tuyos: `SchemaFor[UserProfile]("UserProfile", "Save the user's preferences")`.

`Extracted{ID, Kind, Content, Deleted}` es la memoria que entra y sale del `Manager`. `Decode[T](content)` convierte a la struct; `Text(content)` extrae el texto de una nota; `Note(id, texto)` y `ExistingNotes(textos...)` construyen entradas.

### Manager

```go
m := langmem.NewManager(model,
    langmem.WithSchemas(langmem.MemorySchema, langmem.EpisodeSchema),
    langmem.WithInstructions("..."),     // predeterminado: langmem.DefaultInstructions
    langmem.WithInserts(true), langmem.WithUpdates(true), langmem.WithDeletes(false))
out, err := m.Extract(ctx, msgs, existing, 3) // maxSteps: hasta 3 rondas
```

Protocolo con el modelo (igual que LangMem): una llamada de tool por memoria nueva; repetir `json_doc_id` actualiza; `RemoveDoc` borra; desde la 2.ª ronda existe la tool `Done`. Entre rondas el modelo recibe "Memory X inserted/updated." y la lista actual. `Extract` devuelve la lista completa (memorias existentes cambiadas o no, nuevas, y las borradas con `Deleted: true`). El contenido que no valida contra el schema se descarta y el error vuelve al modelo como resultado de la tool.

Predeterminados: inserts y updates **activados**, deletes **desactivados** en el `Manager`; deletes **activados** en el `StoreManager` (como en LangMem).

### StoreManager

```go
sm := langmem.NewStoreManager(model, langmem.Namespace{"memories", "{langgraph_user_id}"},
    langmem.WithStore(st),            // o compila el grafo con graph.WithStore
    langmem.WithQueryLimit(5),
    langmem.WithQueryModel(qm),       // el LLM escribe las búsquedas (si no: ventanas ampliadas de la conversación)
    langmem.WithDefault(langmem.Seed{Content: UserProfile{}}), // semilla → perfil
    langmem.WithPhases(langmem.Phase{IncludeMessages: false}))  // consolidación extra
puts, err := sm.Invoke(ctx, langmem.Payload{Messages: msgs, MaxSteps: 2})
```

Flujo de `Invoke`: resuelve el namespace → **recupera** memorias relevantes (ventanas de los últimos 1, 2, 4… mensajes; o las consultas de `QueryModel`; completa con memorias no ordenadas hasta `QueryLimit`) → siembra `default` si está vacío → `Extract` → fases → guarda **solo lo que cambió** → borra lo eliminado. Los ítems quedan como `{"kind": <schema>, "content": <contenido>}`. También hay `Search`, `Get`, `Put`, `Delete` en el namespace resuelto.

**Perfil** = un único documento refinado en el lugar: `WithInserts(false)`, `WithDeletes(false)`, `WithDefault(...)`; la clave es `langmem.DefaultKey` (`"default"`). **Colección** = los predeterminados.

### Tools (hot path)

- `manage_memory`: `{content, action: create|update|delete, id}`. `create` con `id` es error; `update`/`delete` sin `id` es error; los ids son UUID; guarda `{"content": ...}`.
- `search_memory`: `{query, limit=10, offset=0, filter}`; devuelve JSON `[{namespace,key,value,created_at,updated_at,score}]`.
- Los errores se convierten en mensajes para el modelo (`tools.Error`), salvo la falta de store/configuración (`ErrConfiguration`).

### Namespaces y store

`Namespace{"memories", "{langgraph_user_id}"}` (el predeterminado, `DefaultNamespace`) resuelve `{variables}` desde `runnables.Config.Configurable` — usa `langmem.WithConfigurable(ctx, "langgraph_user_id", "u1")` o `Config{Configurable: ...}`. Una variable ausente = error `ErrConfiguration` que lista las claves disponibles (nunca escribe en un namespace equivocado).

`store.Store` no tiene búsqueda semántica; `IndexedStore` la añade sobre **cualquier** backend (memory, sqlstore, redis, mongo, dynamodb): el vector va dentro del ítem (`_embedding`, oculto en los resultados) y la búsqueda es un coseno por escaneo del prefijo. `Query(ctx, store, prefix, query, filter, limit)` usa búsqueda semántica si el store implementa `SemanticSearcher`; si no, ranking por palabras en común, y lista todo con consulta vacía.

### Optimización de prompts (memoria procedural)

| Kind | Llamadas al LLM | Cómo |
|---|---|---|
| `PromptMemory` | 1 | pide `GeneralResponse{logic, update_prompt, new_prompt}` |
| `Metaprompt` | 1 a `MaxReflectionSteps` | bucle `think`/`critique` y, al final, `OptimizedPromptOutput` |
| `Gradient` | 2 a `MaxReflectionSteps`+1 | bucle hasta `recommend{warrants_adjustment, hypotheses, full_recommendations}`; si es `false`, devuelve el prompt intacto; si no, una llamada de reescritura |

Predeterminados: `MaxReflectionSteps` 5, `MinReflectionSteps` 1. Garantías: sin trayectorias → prompt sin cambios, **cero** llamadas; las variables `{x}` del prompt original **tienen** que sobrevivir (si no, error); las llaves sueltas se convierten en `{{ }}`; "No recommendations" mantiene el original. El `MultiPromptOptimizer` pregunta a un clasificador qué prompts (nombres validados) causaron el fallo, usando `WhenToUpdate`, y optimiza solo esos, en paralelo.

### Resumen (memoria de corto plazo)

`SummarizeMessages(ctx, msgs, running, opts)` — algoritmo de LangMem: al acumular `MaxTokensBeforeSummary` (predeterminado `MaxTokens`) resume desde el inicio hasta ese punto, siempre que el resto quepa en `MaxTokens − MaxSummaryTokens − system`; los resultados de tools de una llamada resumida se mantienen; una entrada mayor que la ventana del resumidor se corta (empezando en un mensaje humano). Devuelve `[system?, "Summary of the conversation so far: …", recientes…]` y un `RunningSummary` (ids resumidos + último id) para que la siguiente llamada sea incremental. Todo mensaje necesita un ID; `MaxSummaryTokens` tiene que ser menor que `MaxTokens`.

`SummarizationNode` (nodo de grafo): lee `messages`, escribe `summarized_messages` y `context.running_summary`; con `InputKey == OutputKey` sobrescribe la lista (`RemoveAll`). `SummarizationMiddleware`: lo mismo dentro del `agents.Create` — el modelo ve la vista resumida, el historial guardado queda completo y el resumen es incremental (a diferencia de `agents/middleware.Summarization`, que reescribe el historial).

### Background

```go
ex := &langmem.ReflectionExecutor{Manager: sm}
fut, _ := ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 30 * time.Minute})
...
ex.Shutdown(true, false) // ejecuta los pendientes ahora y espera
```

El mismo `thread_id` (de `Configurable` o `SubmitOptions.ThreadID`) cancela el job pendiente (`*Future` resuelve con `ErrCancelled`) y lo reprograma con la conversación más grande. El job hereda los *valores* del contexto, no su cancelación.

### Middleware

```go
langmem.Middleware{Store: st, Namespace: ns,
    Inject: true, Limit: 5,       // memorias relevantes en el system prompt en cada llamada
    ExposeTools: true,            // manage_memory + search_memory
    Manager: sm, Executor: ex, Delay: time.Minute} // formación al terminar la ejecución (en background si hay Executor)
```

## Diferencias respecto a LangMem de Python

Deliberadas o por limitación — sin maquillaje:

- **Actualización por reemplazo por defecto; parche con `WithPatchUpdates()`.** Sin la opción, el modelo reenvía el contenido completo con `json_doc_id` (funciona con cualquier modelo con tool calling; gasta más tokens en documentos grandes). Con `langmem.WithPatchUpdates()`, la tool `PatchDoc{json_doc_id, planned_edits, patches[]}` aplica operaciones RFC 6902 sobre el contenido (punteros RFC 6901, paquete `internal/jsonpatch`), como el `trustcall` de LangMem. Un parche que falla vuelve al modelo como resultado de la tool para un nuevo intento; si vuelve a fallar, el modelo reenvía el contenido completo con `json_doc_id` (reemplazo).
- **`ReflectionExecutor` remoto**: `langmem.NewRemoteReflectionExecutor(client, graphID)` ejecuta el grafo de memoria en el servidor (`remote/server`) con `after_seconds` y cancelación por `rollback` en el mismo thread. Diferencias: una run ya iniciada también se revierte con un submit más nuevo; el thread es obligatorio; `Future.Wait` no devuelve los `Put`s (las escrituras ocurren en el servidor); la API de store (`Search`) no se portó, así que la búsqueda sigue siendo local.
- **Búsqueda semántica fuera de `store.Store`.** El `Store` del proyecto es CRUD + filtro por igualdad. Por defecto `IndexedStore` hace coseno por escaneo (O(n) por búsqueda). Con `ix.Index = vectorstore.NewHNSW()` la búsqueda pasa a ser aproximada (HNSW en Go puro; recall@10 medido en pruebas). `Put` y `Delete` mantienen el índice al día; `Reindex` indexa los ítems escritos antes. Límites: el índice vive en memoria y no se guarda junto con el store; las escrituras hechas directamente en el store de abajo no entran en él. Filtros: `$eq $ne $gt $gte $lt $lte $in` sobre campos del valor. Los operadores se evalúan en Go: la búsqueda pide k×4 ítems al backend, filtra y cae en el escaneo completo si faltan resultados. Para escala grande, usa un vector store dedicado y adapta `SemanticSearcher`.
- **Sin variantes async**: todo recibe `context.Context`; usa goroutines.
- **No portado:** `langmem.graphs.*` (grafos de servicio alojados en LangGraph Platform), `prompts/_layers.MemoryLayer` (API interna, "not public" en upstream) y `graph_rag.py` (comentado en upstream).
- **Metaprompt:** si el modelo ya devuelve el prompt final en una ronda intermedia, Go lo acepta; Python lo ignora y continúa el bucle.
- **Recuperación:** completa las memorias vistas por el extractor con ítems no ordenados hasta `QueryLimit`, para que un store solo léxico no haga que el modelo duplique memorias.
- **IDs de memoria:** la propia clave del store (hex de 128 bits), no un `uuid5` derivado del namespace.
- **Orden de los resultados de `Search`** es por score; empates, por clave.
- Los textos de prompt (`DefaultInstructions`, gradient, metaprompt, prompt_memory, resumen) están adaptados de LangMem, licencia MIT.

## Pruebas

`go test -race ./langmem/` — 100% offline (y `IndexedStore` sobre SQLite real en `internal/sqlitetest`; Redis/Mongo/DynamoDB no se ejercitaron con él), con `models.Fake` guionado (tool calls) y `embeddings.Fake`. Cubre namespaces, búsqueda semántica/léxica, permisos del manager, multi-step, ciclo de vida del store manager (insertar → actualizar → borrar, aislamiento por usuario), perfil con semilla, fases, query model, tools, searcher, optimizadores (3 tipos + multi + curación de variables), resumen incremental, debounce del executor y el middleware dentro de un `agents.Create`.
