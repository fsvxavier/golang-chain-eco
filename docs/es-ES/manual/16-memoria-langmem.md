# 16. Memoria a largo plazo (LangMem)

[English](../../en-US/manual/16-memoria-langmem.md) | [Português](../../pt-BR/manual/16-memoria-langmem.md) | **Español**

El paquete `langmem` es el port de [LangMem](https://github.com/langchain-ai/langmem): hace que el agent **recuerde entre conversaciones**, aprenda con feedback y quepa en contextos largos. Este capítulo es el tutorial; la referencia completa, el mapa Python → Go y las diferencias están en [docs/es-ES/LANGMEM.md](../LANGMEM.md).

Requisitos previos: capítulos 5 (tools), 8 (store) y 9 (agents y middleware).

## 16.1 Elige el tipo de memoria

| Quieres que el agent… | Tipo | Usa |
|---|---|---|
| recuerde hechos y preferencias del usuario | semántica (colección) | `StoreManager` o las tools `ManageMemory`/`SearchMemory` |
| mantenga un registro estructurado y actual del usuario | semántica (perfil) | `StoreManager` con `WithInserts(false)` + `WithDefault` |
| reaproveche "cómo lo resolví antes" | episódica | `StoreManager` + `EpisodeSchema` |
| mejore su propio system prompt con feedback | procedural | `PromptOptimizer` |
| no desborde el contexto en una conversación larga | corto plazo | `SummarizeMessages` / `SummarizationMiddleware` |

Y *cuándo* formar la memoria:

- **Hot path**: el agent decide durante la conversación (tools). Inmediato, pero cuesta latencia y una decisión más por turno.
- **Background**: un extractor reflexiona después de que la conversación se enfría (`ReflectionExecutor`). Sin latencia y con mayor recall; la memoria solo existe después del retraso.

## 16.2 Prepara store y namespace

```go
import (
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

// Sirve cualquier store.Store (memory, sqlstore, redis, mongo, dynamodb).
// IndexedStore agrega búsqueda semántica; en producción cambia embeddings.Fake por un embedder real.
st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})

ns := langmem.Namespace{"memories", "{langgraph_user_id}"} // es langmem.DefaultNamespace
ctx := langmem.WithConfigurable(ctx, "langgraph_user_id", "alice")
```

El namespace aísla usuarios: `alice` nunca ve lo que `bob` guardó. Si la variable no está en `Configurable`, la llamada falla con `langmem.ErrConfiguration` en vez de escribir en un lugar equivocado.

Para que el `StoreManager` y las tools funcionen **sin** pasar el store, compila el grafo con él (`Compile(graph.WithStore(st))`); el paquete usa `graph.StoreFrom(ctx)`.

## 16.3 Hot path: el agent cuida su propia memoria

```go
cfg := langmem.ToolConfig{Namespace: ns}
agent, _ := agents.Create(agents.Options{
	Model:   model,
	Tools:   []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
	Compile: []graph.CompileOption{graph.WithStore(st)},
})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Recuerda que odio el cilantro"})
```

`manage_memory` crea/actualiza/borra (el modelo recibe el `id` mediante `search_memory`); `search_memory` ordena por similitud. Restringe acciones con `ToolConfig{Actions: []string{"create","update"}}` y personaliza el texto de cuándo usarla con `Instructions`. Ejemplo: `go run ./examples/langmem/hot_path`.

Atajo con inyección automática de las memorias en el system prompt y las tools de una vez:

```go
mw := langmem.Middleware{Store: st, Inject: true, ExposeTools: true}
agent, _ := agents.Create(agents.Options{Model: model, Middleware: []agents.Middleware{mw}})
```

## 16.4 Background: extraer después de la conversación

```go
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st))

// directo (bloquea):
puts, err := sm.Process(ctx, msgs)

// en background, con debounce:
ex := &langmem.ReflectionExecutor{Manager: sm}
fut, _ := ex.Submit(ctx, langmem.Payload{Messages: msgs},
	langmem.SubmitOptions{After: 30 * time.Minute, ThreadID: "t1"})
```

Tres mensajes rápidos en el mismo thread generan **una** extracción, con la conversación completa. Al apagar, llama a `ex.Shutdown(true, false)` para no perder memorias pendientes. Para conectar todo al agent: `Middleware{Manager: sm, Executor: ex, Delay: time.Minute}`. Ejemplo: `go run ./examples/langmem/background`.

Lo que hace el `StoreManager` en cada pasada: busca memorias relacionadas → pide al modelo insertar/actualizar/borrar → guarda solo lo que cambió. Ajustes útiles: `WithQueryLimit(n)`, `WithQueryModel(m)` (un LLM escribe las búsquedas), `WithPhases(...)` (una pasada extra de consolidación), `Payload{MaxSteps: 3}` (refinamiento en varias rondas).

## 16.5 Perfil y episodios

Un **perfil** es un único documento, con campos fijos, refinado en cada conversación:

```go
type UserProfile struct {
	Name          string   `json:"name"`
	PreferredName string   `json:"preferred_name"`
	Skills        []string `json:"skills"`
}
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st),
	langmem.WithSchemas(langmem.SchemaFor[UserProfile]("UserProfile", "Save the user's profile")),
	langmem.WithInserts(false), langmem.WithDeletes(false),
	langmem.WithDefault(langmem.Seed{Content: UserProfile{}}))
// ...
it, _ := sm.Get(ctx, langmem.DefaultKey)
p, _ := langmem.Decode[UserProfile](it.Value["content"])
```

Los **episodios** guardan "situación → razonamiento → acción → resultado" para few-shot futuro: `WithSchemas(langmem.EpisodeSchema)` con instrucciones como "extrae ejemplos de interacciones exitosas". Se pueden mezclar tipos en el mismo manager: `WithSchemas(langmem.MemorySchema, langmem.EpisodeSchema)`. Ejemplo: `go run ./examples/langmem/profile_episodic`.

Usa `Manager` (sin store) cuando solo quieras la extracción en memoria: `NewManager(model).Extract(ctx, msgs, existing, 1)`.

## 16.6 Recuperar memorias

| Necesidad | Llamada |
|---|---|
| por clave | `sm.Get(ctx, key)` |
| por similitud | `sm.Search(ctx, "pregunta", nil, 5)` o `langmem.Query(ctx, st, ns, q, nil, 5)` |
| el modelo decide qué buscar | `langmem.MemorySearcher{Model: m, Store: st}.Search(ctx, msgs)` |
| en el prompt, automático | `Middleware{Inject: true}` |
| resumen de una conversación completa | `langmem.ExtractThread[langmem.ThreadSummary](ctx, model, msgs, "")` |

## 16.7 Memoria procedural: el prompt que aprende

```go
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 3})
nuevo, err := opt.Optimize(ctx, []langmem.Trajectory{{
	Messages: conversacion,
	Feedback: map[string]any{"score": 0, "comment": "quería código, llegó teoría"},
}}, langmem.Prompt{Name: "main", Prompt: "Eres un asistente útil."})
```

- `PromptMemory`: 1 llamada, barato. `Metaprompt`: reflexiona y reescribe. `Gradient`: separa el diagnóstico de la reescritura (el más cuidadoso, el más caro).
- Sin evidencia de fallo, el prompt vuelve **igual**. Las variables `{x}` del prompt original se preservan (si el modelo las pierde, hay error).
- Varios prompts que cooperan: `MultiPromptOptimizer` descubre cuáles causaron el fallo (`Prompt.WhenToUpdate` ayuda) y reescribe solo esos. Ejemplo: `go run ./examples/langmem/prompt_optimizer`.

Flujo recomendado: recoge conversaciones con feedback (nota, comentario, edición humana) → optimiza en lote, sin conexión → revisa la diferencia → publica el nuevo prompt. No optimices en el camino de la solicitud.

## 16.8 Memoria de corto plazo: resumen incremental

```go
node := langmem.SummarizationNode{SummarizeOptions: langmem.SummarizeOptions{
	Model: modeloBarato, MaxTokens: 4000, MaxSummaryTokens: 256,
	// Counter: tu contador de tokens real (predeterminado: aproximación ~4 caracteres/token)
}}
schema := langmem.SummarizationSchema()   // canales "context" y "summarized_messages"
schema["messages"] = graph.Messages()
g := graph.NewStateGraph(schema).AddNode("summarize", node.Node()) // ... antes del nodo del modelo
```

El nodo escribe la vista lista para el modelo en `summarized_messages` (el historial completo queda en `messages`) y el `RunningSummary` en `context`, persistido por el checkpointer, así que cada resumen extiende el anterior en vez de empezar de cero. Dentro de `agents.Create`, usa `Middleware: []agents.Middleware{langmem.SummarizationMiddleware{...}}`. Ejemplo: `go run ./examples/langmem/summarization`.

Para limitar de verdad el tamaño del resumen, pasa un modelo con `max_tokens` fijo (`models.Bind(m, models.WithMaxTokens(256))`): `MaxSummaryTokens` solo reserva presupuesto.

## 16.9 Probar sin red

Guioniza el modelo con `models.Fake`. Para el extractor, devuelve *tool calls* con el nombre del schema (`"Memory"`, `"Episode"`, el tuyo) y, para actualizar, `json_doc_id`:

```go
fake := models.NewFake(messages.AI("", messages.ToolCall{
	Name: "Memory", Args: map[string]any{"content": "le gusta el sushi"}}))
sm := langmem.NewStoreManager(fake, nil, langmem.WithStore(store.NewMemory()))
puts, _ := sm.Process(ctx, msgs)
```

`embeddings.Fake{}` da similitud por palabras en común, suficiente para probar el ranking. Las pruebas del paquete (`langmem/langmem_test.go`) son buenos modelos.

## 16.10 Producción y cuidados

- **Privacidad**: las memorias son datos personales. Namespace por usuario/organización, política de retención y un camino de borrado (`sm.Delete`, `ManageMemory` con `delete`). Considera el middleware `PII` (cap. 9) antes de extraer.
- **Prompt injection**: el texto que el usuario (o una página web) pone en la conversación puede convertirse en memoria y luego inyectarse en el prompt. Trata las memorias como entrada **no confiable** (`Inject` las coloca dentro de `<memories>`), y restringe `Actions` cuando el agent no necesita borrar.
- **Costo**: cada pasada es ≥ 1 llamada al LLM; usa `Executor` con retraso, un `WithQueryLimit` bajo y un modelo más pequeño para extraer.
- **Escala de la búsqueda**: `IndexedStore` escanea el prefijo (O(n) por búsqueda). Para muchas memorias por usuario, indéxalas en un vector store dedicado (cap. 6/13) e implementa `SemanticSearcher` sobre él.
- **Consistencia**: dos extractores simultáneos en el mismo namespace pueden sobrescribir el mismo elemento (gana el último); serializa por usuario/thread (el debounce del executor ya ayuda).
- **Observabilidad**: las llamadas pasan por los callbacks de `core/callbacks` como cualquier modelo/tool (cap. 12).

---

Anterior: [15. Ejemplos y recetas](15-exemplos-e-receitas.md) · Siguiente: [17. Agent Protocol](17-agent-protocol.md) · [Índice del manual](README.es-ES.md)
