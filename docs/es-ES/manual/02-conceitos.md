# 2. Conceptos fundamentales

[English](../../en-US/manual/02-conceitos.md) | [Português](../../pt-BR/manual/02-conceitos.md) | **Español**

## 2.1 Capas

```
deepagents ──► agents (create_agent + middleware) ──► langgraph/graph (Pregel)
                    │                                      │
                    └──► langgraph/prebuilt (ToolNode)     ├─ channels  (slots de estado + reducers)
                                                           ├─ checkpoint (Saver)
core (messages, models, tools, runnables, …)  ◄────────────┴─ store (memoria a largo plazo)
```

Cada capa usa la de abajo. Puedes quedarte en cualquier nivel:

- **core**: chains simples (`prompt | modelo | parser`), RAG, extracción estructurada.
- **langgraph**: flujos con estado, ramificaciones, loops, pausas y persistencia.
- **agents**: loop listo de "el modelo decide, la tool ejecuta" con middleware.
- **deepagents**: agent con planificación, archivos y delegación.

## 2.2 Mensajes (`core/messages`)

Un `messages.Message` representa un mensaje de conversación. El tipo es único; el campo `Role` los diferencia:

| Role | Constructor | Significado |
|---|---|---|
| `RoleSystem` | `messages.System(texto)` | Instrucciones al modelo |
| `RoleHuman` | `messages.Human(texto)` | Habla del usuario |
| `RoleAI` | `messages.AI(texto, llamadas...)` | Respuesta del modelo, opcionalmente con `ToolCalls` |
| `RoleTool` | `messages.Tool(texto, toolCallID)` | Resultado de una tool |
| `RoleRemove` | `messages.Remove(id)` | Marca la eliminación de un mensaje por su ID, aplicada por el reducer |

Campos relevantes: `ID`, `Content`, `Parts` (contenido multimodal), `ToolCalls`, `ToolCallID`, `Status` (`success`/`error`), `Artifact`, `Usage` (tokens), `ResponseMetadata`.

Funciones útiles:

```go
msg.Text()                          // texto del mensaje
messages.Coerce(v)                  // string | Message | []Message -> []Message
messages.AddMessages(a, b)          // reducer: añade, actualiza por ID, aplica Remove
messages.Filter(msgs, include, exclude)
messages.Trim(msgs, messages.TrimOptions{...})
messages.MergeRuns(msgs)            // une mensajes consecutivos del mismo rol
messages.ApproxTokens(msgs)         // conteo aproximado de tokens
messages.Last(msgs)                 // último mensaje
```

## 2.3 Runnable (`core/runnables`)

`Runnable[I, O]` es la abstracción central: algo que recibe `I` y devuelve `O`.

```go
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, in I) (O, error)
}
```

Los prompts, los modelos (vía `models.AsRunnable`), los parsers, las chains y los retrievers son runnables. Se componen con `Pipe`, `Pipe3`, `Pipe4`, `Parallel`, `Branch`, `WithRetry`, `WithFallbacks`. Capítulo 4.

Capacidades opcionales, detectadas por interfaz: `Streamer[I, O]` (streaming nativo) y `Batcher[I, O]` (lote). Usa las funciones genéricas `runnables.Stream` y `runnables.Batch`; usan la implementación nativa cuando existe y caen a `Invoke` cuando no.

## 2.4 Configuración por contexto

La configuración de ejecución viaja en el `context.Context`:

```go
ctx = runnables.WithConfig(ctx, runnables.Config{
	RunName:        "mi-chain",
	Tags:           []string{"prod"},
	Metadata:       map[string]any{"user": "ana"},
	Callbacks:      []callbacks.Handler{tracer},
	MaxConcurrency: 4,
	RecursionLimit: 50,
	Configurable:   map[string]any{"thread_id": "usuario-42"},
})
```

`WithConfig` **mezcla** con la configuración que ya esté presente. Atajo para threads de grafo: `graph.Thread("usuario-42")` devuelve una `Config` con `thread_id`.

## 2.5 Estado y canales (LangGraph)

El estado de un grafo es un `graph.State`, que es `map[string]any`. Cada clave es un **canal** con una regla de actualización (reducer):

| Canal | Constructor | Comportamiento |
|---|---|---|
| Último valor | `channels.LastValue()` (predeterminado) | La escritura reemplaza el valor |
| Mensajes | `graph.Messages()` | Reducer `AddMessages` |
| Acumular lista | `graph.Append()` | Concatena listas |
| Sumar | `graph.Sum()` | Suma enteros |
| Efímero | `channels.Ephemeral(false)` | Dura solo un paso |
| Personalizado | `channels.BinOp` | Cualquier función `(actual, nuevo) -> resultado` |

Cada nodo recibe una **copia** del estado y devuelve una **actualización parcial**; el runtime aplica las actualizaciones mediante los reducers. Más en el capítulo 7.

## 2.6 Ejecución en super-pasos

El runtime es bulk-síncrono (estilo Pregel):

1. En cada super-paso, todos los nodos agendados corren **en paralelo** sobre el mismo snapshot del estado.
2. Las escrituras se aplican mediante los reducers, en el orden de las tasks.
3. El runtime enruta a los próximos nodos (aristas estáticas, uniones, routers condicionales, `Command`, `Send`).
4. Con checkpointer, se guarda un checkpoint tras la entrada y tras cada paso.

Consecuencia práctica: los nodos del mismo paso no ven las escrituras de los demás. Para sumar resultados de ramas paralelas, declara un reducer (`Append`, `Sum` o personalizado) en el canal que recibe las escrituras.

## 2.7 Tipos de retorno de un nodo

Un nodo (`func(ctx, graph.State) (any, error)`) puede devolver:

- `nil`: ninguna actualización.
- `graph.State{...}`: actualización parcial.
- `graph.Command{Update: ..., Goto: []string{...}}`: actualización y desvío de flujo.
- `[]graph.Send{...}`: fan-out dinámico (map-reduce).

## 2.8 Tools

Una `tools.Tool` expone nombre, descripción, JSON Schema de los argumentos y una función de ejecución. `tools.New[Args]` genera el schema por reflexión de la struct `Args`. El modelo "llama" a la tool pidiendo `ToolCalls`; el agent la ejecuta y devuelve un mensaje `RoleTool`. Capítulo 5.

## 2.9 Callbacks

Cualquier ejecución emite eventos (inicio/fin/error de chain, modelo, tool, nodo). `callbacks.Handler` consume esos eventos para logs, tracing y métricas. Los handlers van en el contexto: `callbacks.WithHandlers(ctx, h1, h2)`. Capítulo 12.

## 2.10 Errores que vas a ver

| Error | Cuándo |
|---|---|
| `*graph.GraphRecursionError` | El grafo superó el `RecursionLimit` (predeterminado 25 pasos) |
| `*tools.Error` (vía `tools.Errorf`) | Error "del modelo": se convierte en mensaje de tool y el loop continúa |
| `*middleware.LimitError` | Límite de llamadas a modelo/tool alcanzado (con `ExitBehavior: "error"`) |
| `*middleware.PIIError` | PII detectada con la estrategia `block` |
| `*mcp.RPCError` | Error JSON-RPC devuelto por un servidor MCP |

---

Anterior: [1. Introducción e instalación](01-introducao-e-instalacao.md) · Siguiente: [3. Modelos y proveedores](03-modelos-e-provedores.md) · [Índice del manual](README.md)
