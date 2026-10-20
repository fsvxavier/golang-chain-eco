# 8. Persistencia, human-in-the-loop y streaming

[English](../../en-US/manual/08-persistencia-hitl-streaming.md) | [Português](../../pt-BR/manual/08-persistencia-hitl-streaming.md) | **Español**

## 8.1 Checkpointers y threads

Con un checkpointer, cada super-paso se guarda por `thread_id`. Eso permite continuar conversaciones, pausar para aprobación humana, inspeccionar el historial y volver atrás en el tiempo.

```go
saver := checkpoint.NewMemory()
app, _ := g.Compile(graph.WithCheckpointer(saver))

ctx := runnables.WithConfig(context.Background(), graph.Thread("usuario-42"))
out, _ := app.Invoke(ctx, graph.State{"messages": "hola"})
out, _ = app.Invoke(ctx, graph.State{"messages": "¿y ahora?"}) // continúa la misma conversación
```

- Sin `thread_id` en la configuración, el grafo no persiste entre llamadas.
- Cada thread tiene su propia línea de tiempo de checkpoints.
- La misma regla vale para `agents.Create` (vía `Options.Compile`) y `deepagents.Create` (vía `Options.Checkpointer`).

### Durabilidad

Por defecto (`graph.DurabilitySync`) el checkpoint se escribe antes de que siga cada super-paso. Dos alternativas cambian garantía por latencia:

| Modo | Cuándo escribe | Uso |
|------|--------------|-----|
| `DurabilitySync` | En cada super-paso, antes de continuar (predeterminado) | Máxima garantía: una caída pierde como mucho el paso en curso |
| `DurabilityAsync` | En cada super-paso, en una goroutine de fondo, en orden | Reduce la latencia; `Invoke`/`Stream` solo retornan después del último guardado. Un error de guardado aborta la ejecución en el siguiente super-paso |
| `DurabilityExit` | Solo al pausar (interrupt), terminar o fallar entre super-pasos | Menos escrituras; una caída en medio de la ejecución pierde los pasos desde el último checkpoint |

```go
app, _ := g.Compile(graph.WithCheckpointer(saver), graph.WithDurability(graph.DurabilityAsync)) // predeterminado del grafo

// por llamada, sobrescribe el predeterminado del grafo
ctx := graph.WithRunDurability(runnables.WithConfig(context.Background(), graph.Thread("usuario-42")), graph.DurabilityExit)
out, err := app.Invoke(ctx, graph.State{"messages": "hola"})
```

- Las pausas de subgrafos con checkpointer propio se escriben siempre, en cualquier modo, porque el padre necesita reanudar desde ellas.
- En `exit`, un fallo de nodo entre super-pasos escribe el último estado bueno, así que el thread puede reanudarse. Un fallo de reducer en medio de un super-paso no escribe nada.

### Backends de checkpoint

| Backend | Constructor | Observación |
|---|---|---|
| Memoria | `checkpoint.NewMemory()` | Pruebas y prototipos; pierde todo al terminar |
| SQLite / PostgreSQL | `sqlsaver.New(ctx, db, sqlsaver.SQLite \| sqlsaver.Postgres)` | `database/sql`; tú traes el driver |
| Redis | `redis.NewSaver(redis.New("host:6379"))` | Cliente RESP propio |
| MongoDB / Atlas | `mongo.NewSaver(client)` | OP_MSG propio; sin `mongodb+srv://` |
| DynamoDB | `dynamodb.NewSaver(ctx, client, "tabla")` | Crea la tabla bajo demanda; ítem ≤ 400 KB |
| S3 y compatibles | `s3.NewSaver(client, "bucket", "prefijo")` | AWS, MinIO, Tigris, R2... |

Ejemplo con SQLite:

```go
import (
	"database/sql"
	_ "modernc.org/sqlite" // o _ "github.com/jackc/pgx/v5/stdlib"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
)

db, _ := sql.Open("sqlite", "checkpoints.db")
saver, err := sqlsaver.New(ctx, db, sqlsaver.SQLite) // o sqlsaver.Postgres
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

### Serialización: tipos propios

Los valores se guardan en JSON. Si el estado contiene tipos tuyos, regístralos para que sobrevivan al round trip:

```go
checkpoint.Register("miapp.Pedido", Pedido{})
checkpoint.Register("[]miapp.Pedido", []Pedido{})
```

Ya vienen registrados: mensajes, `graph.Send`, archivos, todos, tasks asíncronas y evaluaciones de rúbrica, además de los escalares de Go `int8`…`int64`, `uint`…`uint64`, `float32`, `[]byte`, `time.Time` y `time.Duration`, que vuelven con su tipo original. Los números sin tipo explícito siguen volviendo como `int` (enteros) o `float64`. Con `checkpoint.Strict(true)`, guardar una struct no registrada pasa a ser un error en lugar de convertirse en `map[string]any`.

### Backend propio

Implementa `checkpoint.Saver` (4 métodos):

```go
type Saver interface {
	Put(ctx context.Context, cp Checkpoint) (string, error)
	Get(ctx context.Context, cfg Config) (Checkpoint, bool, error)
	List(ctx context.Context, threadID string, limit int) ([]Checkpoint, error)
	DeleteThread(ctx context.Context, threadID string) error
}
```

Valídalo con la suite `internal/conformance` (la misma que usan los backends del proyecto).

## 8.2 Human-in-the-loop con `Interrupt`

`graph.Interrupt(ctx, valor)` pausa la ejecución y devuelve el valor al que llama. Al reanudar, `Interrupt` retorna la respuesta enviada.

```go
approve := func(ctx context.Context, s graph.State) (any, error) {
	// La primera ejecución se pausa aquí. Al reanudar, Interrupt devuelve el valor del resume.
	answer := graph.Interrupt(ctx, map[string]any{"pregunta": "¿Aprobar el envío?"})
	if answer != "sí" {
		return graph.State{"status": "cancelado"}, nil
	}
	return graph.State{"status": "enviado"}, nil
}

out, _ := app.Invoke(ctx, graph.State{"status": "nuevo"})
for _, i := range graph.Interrupts(out) {
	fmt.Println("pausado en", i.Node, i.Value) // InterruptInfo{ID, Node, Value}
}
out, _ = app.Invoke(ctx, graph.Command{Resume: "sí"}) // reanuda
```

Requisitos y reglas:

- **Exige checkpointer y `thread_id`**: el estado pausado queda guardado.
- **El nodo se reejecuta desde el inicio al reanudar.** Mantén idempotente (o mueve después del `Interrupt`) todo lo que tenga efectos colaterales y venga antes de él.
- Varios interrupts simultáneos (ramas paralelas): responde por ID con `graph.Command{ResumeMap: map[string]any{id: valor}}`.
- Las tasks hermanas ya completadas en el mismo paso no se vuelven a ejecutar.

### Pausas estáticas

```go
app, _ := g.Compile(graph.WithCheckpointer(saver), graph.InterruptBefore("tools"))
// o graph.InterruptAfter("planner")

out, _ := app.Invoke(ctx, input) // corre hasta antes de "tools" y pausa
// inspecciona/edita el estado...
out, _ = app.Invoke(ctx, nil)    // nil continúa desde donde se detuvo
```

## 8.3 Inspección y viaje en el tiempo

```go
snap, _ := app.GetState(ctx)            // Values, Next, Interrupts, CheckpointID, ParentID, Step
hist, _ := app.GetStateHistory(ctx, 10) // del más reciente al más antiguo

// edita el estado como si lo hubiera escrito el nodo "research":
app.UpdateState(ctx, graph.State{"topic": "otro"}, "research")

// fork desde un checkpoint antiguo:
old := runnables.WithConfig(ctx, runnables.Config{
	Configurable: map[string]any{"checkpoint_id": hist[3].CheckpointID},
})
app.Invoke(old, nil)
```

Usos: depurar una ejecución paso a paso, corregir a mano un estado malo y continuar, o explorar alternativas desde un punto anterior.

## 8.4 Memoria a largo plazo (`langgraph/store`)

El checkpoint guarda el estado **de un thread**. El `store.Store` guarda conocimiento **entre threads** (preferencias, hechos sobre el usuario).

```go
type Store interface {
	Put(ctx, ns []string, key string, value map[string]any) error
	Get(ctx, ns []string, key string) (*Item, error)
	Delete(ctx, ns []string, key string) error
	Search(ctx, prefix []string, filter map[string]any, limit int) ([]Item, error)
	ListNamespaces(ctx, prefix []string) ([][]string, error)
}
```

| Backend | Constructor |
|---|---|
| Memoria | `store.NewMemory()` |
| SQLite / PostgreSQL | `sqlstore` (`langgraph/store/sqlstore`) |
| Redis | `redis.NewStore(client)` |
| MongoDB | `mongo.NewStore(client)` |
| DynamoDB | `dynamodb.NewStore(ctx, client, "tabla")` (`Search` hace un Scan) |
| S3 | `s3.NewStore(client, "bucket", "prefijo")` |

Enchúfalo al grafo con `graph.WithStore(s)`; dentro del nodo, `graph.StoreFrom(ctx)`. En los Deep Agents, `backends.Store` expone un `Store` como sistema de archivos compartido.

## 8.5 Streaming

`app.Stream(ctx, input)` devuelve el estado en cada paso. `StreamEvents` permite elegir los modos:

```go
for ev, err := range app.StreamEvents(ctx, input, graph.ModeUpdates, graph.ModeMessages, graph.ModeCustom) {
	if err != nil {
		log.Fatal(err)
	}
	switch ev.Mode {
	case graph.ModeUpdates:  // ev.Node + actualización, cada vez que un nodo termina
	case graph.ModeValues:   // estado completo en cada paso
	case graph.ModeMessages: // tokens del LLM a medida que llegan (ev.Data es messages.Message)
	case graph.ModeCustom:   // lo que el nodo envió con graph.StreamWriter(ctx)(valor)
	}
}
```

Evento personalizado, dentro de un nodo:

```go
write := graph.StreamWriter(ctx)
write(map[string]any{"progreso": 40})
```

Cuándo usar cada modo:

| Modo | Uso típico |
|---|---|
| `ModeValues` | Depuración; UI que vuelve a dibujar el estado completo |
| `ModeUpdates` | Mostrar "qué hizo cada etapa" |
| `ModeMessages` | Chat con escritura en tiempo real |
| `ModeCustom` | Barras de progreso y telemetría de negocio |

La iteración es un `iter.Seq2`. Si sales del `for` antes (`break`), la ejecución se cancela. Para servir estos eventos a un navegador, mira el protocolo AG-UI en el [capítulo 11](11-mcp-remote-servidor.md#115-protocolo-ag-ui).

## 8.6 Receta: chatbot que recuerda y pide aprobación

```go
saver := checkpoint.NewMemory() // cámbialo por sqlsaver/redis en producción
agent, _ := agents.Create(agents.Options{
	Model: m,
	Tools: []tools.Tool{search, sendEmail},
	Middleware: []agents.Middleware{
		middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_email": {}}},
	},
	Compile: []graph.CompileOption{graph.WithCheckpointer(saver)},
})

ctx := runnables.WithConfig(ctx, graph.Thread("conv-1"))
out, _ := agent.Invoke(ctx, graph.State{"messages": "Envía el resumen a Bob"})
if reqs := graph.Interrupts(out); len(reqs) > 0 {
	// muestra reqs[0].Value al usuario; después:
	out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
		Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	}})
}
```

---

Anterior: [7. LangGraph: StateGraph](07-langgraph.md) · Siguiente: [9. Agents y middleware](09-agents-e-middleware.md) · [Índice del manual](README.es-ES.md)
