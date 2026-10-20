# 7. LangGraph: StateGraph

[English](../../en-US/manual/07-langgraph.md) | [Português](../../pt-BR/manual/07-langgraph.md) | **Español**

Usa `langgraph/graph` cuando el flujo tiene estado, ramificaciones, loops o pausas. Un grafo está formado por **nodos** (funciones Go) conectados por **aristas**, que trabajan sobre un **estado** compartido.

## 7.1 Anatomía de un grafo

```go
import (
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

schema := graph.Schema{
	"messages": graph.Messages(),      // acumula con el reducer add_messages
	"steps":    graph.Sum(),           // suma enteros
	"log":      graph.Append(),        // concatena listas
	"topic":    channels.LastValue(),  // último valor (predeterminado para claves no declaradas)
}

g := graph.NewStateGraph(schema).
	AddNode("research", func(ctx context.Context, s graph.State) (any, error) {
		return graph.State{"log": []any{"investigó " + s["topic"].(string)}, "steps": 1}, nil
	}).
	AddNode("write", writeNode).
	AddNode("review", reviewNode).
	AddEdge(graph.Start, "research").
	AddEdge("research", "write").
	AddConditionalEdges("write", func(ctx context.Context, s graph.State) (any, error) {
		if s["steps"].(int) > 3 {
			return graph.End, nil
		}
		return "review", nil
	}).
	AddEdge("review", "write")

app, err := g.Compile()
out, err := app.Invoke(ctx, graph.State{"topic": "Go generics"})
fmt.Println(app.Mermaid()) // diagrama Mermaid del grafo
```

Pasos:

1. **Schema**: declara los canales y sus reducers.
2. **Nodos**: `AddNode(nombre, func(ctx, State) (any, error))`.
3. **Aristas**: `AddEdge(desde, hasta)` (usa `graph.Start` y `graph.End`), `AddConditionalEdges(desde, router)`.
4. **Compile**: valida y devuelve `*graph.Compiled`, que ofrece `Invoke`, `Stream`, `StreamEvents`, `GetState`, `GetStateHistory`, `UpdateState`, `Mermaid` y `Nodes`.

Atajos: `SetEntryPoint(n)` equivale a `AddEdge(Start, n)`; `SetFinishPoint(n)` a `AddEdge(n, End)`; `AddSequence(nombres, funcs)` encadena nodos en orden.

## 7.2 Nodos

Firma: `func(ctx context.Context, s graph.State) (any, error)`.

- El nodo recibe una **copia** del estado; alterarla no afecta al grafo.
- Devuelve solo lo que cambió, como `graph.State{...}`. Las actualizaciones pasan por los reducers del schema.
- Valores de retorno aceptados: `nil`, `graph.State`, `graph.Command`, `[]graph.Send`.
- Un error devuelto aborta la ejecución (usa `graph.WithRetry` para reintentar).

### Opciones de nodo

```go
g.AddNode("fetch", fetchFn,
	graph.WithRetry(runnables.RetryOptions{MaxAttempts: 3}),
	graph.WithCachePolicy(graph.CachePolicy{TTL: time.Minute}),
	graph.Deferred(),
)
app, _ := g.Compile(graph.WithCache(graph.NewMemoryCache()))
```

| Opción | Efecto |
|---|---|
| `graph.WithRetry(opts)` | Repite el nodo con backoff cuando falla |
| `graph.WithCachePolicy(p)` | Memoriza el resultado por TTL (requiere `Compile(graph.WithCache(...))`) |
| `graph.Deferred()` | El nodo solo se ejecuta cuando no hay otras tasks pendientes (útil para consolidar) |

## 7.3 Reducers y canales

Si dos escrituras en la misma clave ocurren en el mismo paso, el reducer decide el resultado. Sin reducer, vale el último valor y las escrituras simultáneas pueden chocar. Para acumular resultados de ramas paralelas, usa `Append()`, `Sum()`, `Messages()` o un reducer propio:

```go
maxCh := channels.BinOp(func(cur, upd any) (any, error) {
	if upd.(int) > cur.(int) {
		return upd, nil
	}
	return cur, nil
}, func() any { return 0 })

schema := graph.Schema{"best": maxCh}
```

`channels.Overwrite{Value: v}` fuerza la sustitución ignorando el reducer. Otros canales: `AnyValue`, `Topic(accumulate)`, `NamedBarrier(nombres...)`, `Ephemeral(guard)`.

### Estado tipado con struct (`graph.NewTyped`)

Si prefieres una struct a `map[string]any`, `graph.NewTyped[T]()` arma el `Schema` a partir de las tags `graph:"clave[,reducer=nombre]"` de `T`. La API de bajo nivel sigue igual; la tipada es solo una capa por encima (`langgraph/graph/typed.go`):

```go
type OrderState struct {
	Messages []messages.Message `graph:"messages,reducer=messages"`
	Total    int                `graph:"total,reducer=sum"`
	Log      []string           `graph:"log,reducer=append"`
	Approved bool               `graph:"approved"`
	Items    int                `graph:"items"`
}

g := graph.NewTyped[OrderState]().
	AddNode("price", func(ctx context.Context, s OrderState) (graph.Update, error) {
		return graph.Update{"total": s.Items * 10, "log": []string{"precificado"}}, nil
	}).
	SetEntryPoint("price").SetFinishPoint("price")

c, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
out, interrupts, err := c.Invoke(ctx, OrderState{Items: 3}) // out es OrderState
out, _, err = c.Invoke(ctx, graph.Command{Resume: true})    // reanuda el mismo thread
```

Reglas:

- Solo entran en el estado los campos exportados con tag. Los campos sin tag se ignoran; `graph:"-"` los ignora explícitamente.
- Reducers: `last` (predeterminado), `messages` (add_messages), `append` (concatena los elementos de cada update; campo slice), `sum` (campo numérico). Un reducer desconocido, una clave duplicada, un campo no exportado o un tipo incompatible con el reducer son errores de `Compile`.
- Un nodo tipado recibe `T` y devuelve un `graph.Update` (mapa parcial). Una struct no puede decir "este campo no se actualizó", porque el cero también es un valor válido; por eso el nodo nombra las claves que escribe. Una clave que no es tag de `T` hace fallar el nodo.
- `Invoke(ctx, in)` acepta `T` (escribe todos los campos con tag, incluidos los ceros) o `graph.Command` (reanudación o actualización parcial). Devuelve `(T, []graph.InterruptInfo, error)`.
- `Stream(ctx, in)` devuelve `iter.Seq2[T, error]`. Para eventos completos (`StreamEvents`), `GetState` y el `Saver`, usa `c.Unwrap()`, que devuelve el `*graph.Compiled`.
- Si `T` (o `*T`) tiene `Validate() error`, se ejecuta en la entrada de cada nodo; un error devuelto hace fallar la ejecución.
- Los tipos de campo struct se registran en `checkpoint.Register` con el nombre `paquete.Tipo` cuando corre `NewTyped`, así sobreviven a `checkpoint.Strict(true)`. Las structs anónimas no se pueden registrar.
- Las aristas, las aristas condicionales (el router recibe `T`) y `AddJoin` existen en el builder tipado; lo que falte está en `g.Graph()`.

Ejemplo offline completo: `go run ./examples/langgraph/typed_state`.

## 7.4 Enrutamiento condicional

El router devuelve el nombre del siguiente nodo, `graph.End`, o una lista de `graph.Send`:

```go
AddConditionalEdges("classify", func(ctx context.Context, s graph.State) (any, error) {
	switch s["kind"] {
	case "billing":
		return "billing_agent", nil
	case "tech":
		return "tech_agent", nil
	}
	return graph.End, nil
})
```

Con un mapa opcional, el router devuelve etiquetas y el mapa las traduce a nodos: `AddConditionalEdges("classify", router, map[string]string{"b": "billing_agent", "t": "tech_agent"})`.

## 7.5 Paralelismo y unión

```go
AddEdge("plan", "search_web").
AddEdge("plan", "search_db").          // search_web y search_db corren en el mismo super-paso
AddJoin([]string{"search_web", "search_db"}, "merge") // merge solo se ejecuta cuando los dos terminan
```

Sin `AddJoin`, el nodo de destino se ejecutaría en cuanto *cualquier* rama terminara. Usa un reducer (`Append`) en el canal donde escriben las dos ramas.

## 7.6 Fan-out dinámico con `Send` (map-reduce)

```go
AddConditionalEdges("plan", func(ctx context.Context, s graph.State) (any, error) {
	var sends []graph.Send
	for _, topic := range s["topics"].([]string) {
		sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"topic": topic}})
	}
	return sends, nil
})
```

Cada `Send` crea una task de `worker` con el `Arg` como estado de entrada, en paralelo. Los resultados vuelven al estado principal mediante los reducers (por ejemplo `graph.Append()` en `"results"`). Ejemplos: `examples/langgraph/map_reduce`, `llm_compiler`.

## 7.7 `Command`: actualizar y desviar en el mismo retorno

```go
return graph.Command{
	Update: graph.State{"status": "aprobado"},
	Goto:   []string{"publicar"},
}, nil
```

También se usa para reanudar ejecuciones pausadas: `app.Invoke(ctx, graph.Command{Resume: valor})` (capítulo 8).

## 7.8 Subgrafos

Un grafo compilado puede llamarse dentro de un nodo:

```go
inner, _ := innerGraph.Compile()
outer := graph.NewStateGraph(schema).
	AddNode("team", func(ctx context.Context, s graph.State) (any, error) {
		return inner.Invoke(ctx, graph.State{"messages": s["messages"]})
	})
```

Un `Interrupt` interno se propaga al grafo padre y se reanuda correctamente, con o sin checkpointer propio en el subgrafo (su checkpoint vive en el namespace `nodo:índice`). Si varios nodos del subgrafo se detienen en el mismo super-paso, la primera pausa (en el orden de las tasks) sube al padre, y cada `Command{Resume}` responde a la siguiente, siempre en el mismo orden. Para recibir los eventos del subgrafo en el stream del padre, usa `graph.Subgraphs()`; cada evento llega con `Event.Namespace`. Ejemplos: `subgraph`, `hierarchical_teams`.

## 7.9 Límite de pasos

El predeterminado es 25 super-pasos. Al excederlo, `Invoke` devuelve `*graph.GraphRecursionError`. Ajústalo por ejecución:

```go
ctx = runnables.WithConfig(ctx, runnables.Config{RecursionLimit: 50})
```

Los loops legítimos (agents que usan muchas tools) pueden necesitar un límite mayor; los loops infinitos se cortarán.

## 7.10 Memoria entre threads (store)

```go
app, _ := g.Compile(graph.WithStore(store.NewMemory()))

// dentro de un nodo:
st := graph.StoreFrom(ctx)
st.Put(ctx, []string{"users", "ana"}, "prefs", map[string]any{"idioma": "pt-BR"})
item, _ := st.Get(ctx, []string{"users", "ana"}, "prefs")
```

El `store.Store` guarda memoria de **largo plazo** compartida entre threads, con namespaces (`[]string`). Capítulo 8.

## 7.11 Prebuilt: ToolNode y agent ReAct mínimo

```go
agent, err := prebuilt.CreateReactAgent(prebuilt.ReactOptions{
	Model: m, Tools: []tools.Tool{search}, Prompt: "Sé objetivo.",
})
```

`prebuilt.NewToolNode(tools...)` ejecuta en paralelo las `ToolCalls` del último mensaje de IA, y `prebuilt.ToolsCondition` es el router predeterminado "¿tiene tool call? ve a tools; si no, fin". Para un agent completo (middleware, límites, HITL), usa `agents.Create` (capítulo 9). El ejemplo `react_from_scratch` arma el loop a mano.

## 7.12 API funcional: `Entrypoint` y `Task`

Escribe el flujo como código Go normal. Las tasks se memorizan en el checkpoint, así que una reanudación no repite trabajo ya hecho.

```go
wf := functional.New("informe", saver, func(ctx context.Context, tema string) (string, error) {
	datos, err := functional.Task(ctx, "recolectar", func(ctx context.Context) (string, error) {
		return recolectar(tema) // los efectos secundarios van dentro de tasks
	}).Result()
	if err != nil {
		return "", err
	}
	ok := graph.Interrupt(ctx, "¿Publicar?").(string) // human-in-the-loop
	if ok != "sí" {
		return "descartado", nil
	}
	return publicar(datos)
})

ctx := runnables.WithConfig(ctx, graph.Thread("wf-1"))
r, _ := wf.Invoke(ctx, "baterías") // r.Interrupt != nil: esperando
r, _ = wf.Resume(ctx, "sí")        // "recolectar" NO se ejecuta de nuevo
```

Cuándo usarlo: flujos lineales con pocas ramificaciones y mucha lógica de negocio, en los que dibujar nodos y aristas sería ceremonia. Usa `StateGraph` cuando quieras visualizar el flujo (Mermaid), paralelizar ramas o tener varios puntos de entrada.

## 7.13 Patrones listos en los ejemplos

| Patrón | Ejemplo |
|---|---|
| ReAct (loop modelo/tools) | `react_from_scratch`, `react_agent_middleware` |
| Plan-and-execute, ReWOO, LLM Compiler | `plan_and_execute`, `rewoo`, `llm_compiler` |
| Reflexión (generador + crítico) | `reflection`, `code_assistant` |
| Supervisor de agents | `multi_agent_supervisor`, `hierarchical_teams` |
| RAG correctivo/adaptativo | `corrective_rag`, `self_rag`, `adaptive_rag` |
| Map-reduce | `map_reduce` |

---

Anterior: [6. RAG](06-rag.md) · Siguiente: [8. Persistencia, HITL y streaming](08-persistencia-hitl-streaming.md) · [Índice del manual](README.md)
