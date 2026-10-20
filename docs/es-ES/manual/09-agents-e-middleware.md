# 9. Agents y middleware

[English](../../en-US/manual/09-agents-e-middleware.md) | [Português](../../pt-BR/manual/09-agents-e-middleware.md) | **Español**

`agents.Create` monta el loop clásico de agent: el modelo decide, las tools se ejecutan, el resultado vuelve al modelo, hasta que hay una respuesta final. Es el equivalente de `create_agent` de LangChain.

## 9.1 Crear un agent

```go
agent, err := agents.Create(agents.Options{
	Model:        m,
	Tools:        []tools.Tool{search, sendEmail},
	SystemPrompt: "Eres un asistente de atención al cliente.",
	Middleware:   []agents.Middleware{ /* ver 9.4 */ },
	Compile:      []graph.CompileOption{graph.WithCheckpointer(saver)},
})
```

| Campo | Función |
|---|---|
| `Model` | **Obligatorio.** Cualquier `models.ChatModel` |
| `Tools` | Tools disponibles (se suman las de los middlewares) |
| `SystemPrompt` | Prompt de sistema |
| `Middleware` | Lista ordenada de middlewares |
| `ResponseFormat` | Pide salida JSON; resultado en `state["structured_response"]` |
| `StateSchema` | Canales de estado extra (`graph.Schema`) |
| `Compile` | Opciones de compilación del grafo (checkpointer, store, interrupts, caché) |

El retorno es un `*graph.Compiled`: todo lo de los capítulos 7 y 8 aplica (`Invoke`, `Stream`, `StreamEvents`, `GetState`, threads...).

## 9.2 Ejecutar

```go
out, err := agent.Invoke(ctx, graph.State{"messages": "¿Qué tiempo hace en Lisboa?"})
msgs := out["messages"].([]messages.Message)
fmt.Println(msgs[len(msgs)-1].Text())
```

La entrada `messages` acepta `string`, `messages.Message` o `[]messages.Message`. Con checkpointer y `graph.Thread(id)` en el contexto, las llamadas siguientes continúan la conversación.

### Streaming

```go
for ev, err := range agent.StreamEvents(ctx, graph.State{"messages": "hola"}, graph.ModeMessages) {
	if err != nil {
		log.Fatal(err)
	}
	if msg, ok := ev.Data.(messages.Message); ok {
		fmt.Print(msg.Text())
	}
}
```

## 9.3 Flujo interno y puntos de extensión

```
before_agent → [ before_model → model → after_model ] ⇄ tools → after_agent
```

Un middleware implementa `Name() string` y cualquier combinación de estos hooks:

| Hook | Cuándo se ejecuta |
|---|---|
| `BeforeAgent(ctx, st)` | Una vez, antes de todo |
| `BeforeModel(ctx, st)` | Antes de cada llamada al modelo |
| `WrapModelCall(ctx, req, next)` | Alrededor de la llamada al modelo (puede cambiar `req` o reemplazar el resultado) |
| `AfterModel(ctx, st)` | Después de cada respuesta del modelo |
| `WrapToolCall(ctx, call, next)` | Alrededor de cada ejecución de tool |
| `AfterAgent(ctx, st)` | Una vez, al final |
| `Tools() []tools.Tool` | (opcional) agrega tools |
| `StateSchema() graph.Schema` | (opcional) agrega canales de estado |

Los hooks de estado devuelven `nil`, una actualización `graph.State` o un `graph.Command`. Para desviar el flujo, incluye `jump_to`:

```go
return graph.State{"jump_to": agents.JumpEnd}, nil // o agents.JumpModel, agents.JumpTools
```

## 9.4 Middlewares incluidos (`agents/middleware`)

```go
Middleware: []agents.Middleware{
	middleware.ModelCallLimit{RunLimit: 10},
	middleware.ToolRetry{Options: runnables.RetryOptions{MaxAttempts: 3}},
	middleware.Summarization{Model: m, MaxTokens: 8000, KeepMessages: 12},
	middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail, middleware.PIICreditCard}, Strategy: middleware.PIIRedact},
	middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_email": {}}},
},
```

| Middleware | Para qué | Campos principales |
|---|---|---|
| `ModelCallLimit` | Limita llamadas al modelo (costo, loops) | `ThreadLimit`, `RunLimit`, `ExitBehavior` (`"end"` predeterminado o `"error"`) |
| `ToolCallLimit` | Limita llamadas de tool (de una o de todas) | `ToolName`, `ThreadLimit`, `RunLimit`, `ExitBehavior` (`"continue"`, `"error"`, `"end"`) |
| `ModelFallback` | Prueba otros modelos si el principal falla | `Fallbacks []models.ChatModel` |
| `ModelRetry` | Repite la llamada al modelo con backoff | `Options runnables.RetryOptions` |
| `ToolRetry` | Repite la ejecución de tools | `Options`, `Tools` (restringe a algunos nombres) |
| `Summarization` | Resume el historial cuando pasa de un límite | `Model`, `MaxTokens` (4000), `KeepMessages` (20), `Counter`, `Prompt` |
| `ContextEditing` | Limpia resultados antiguos de tools | `TriggerTokens` (100000), `Keep` (3), `Placeholder` |
| `TodoList` | Da al agent la tool `write_todos` y la clave `todos` | `SystemPrompt` |
| `PII` | Elimina datos personales de la entrada | `Kinds`, `Strategy` |
| `HumanInTheLoop` | Pausa para aprobar/editar/rechazar tools | `Tools map[string]ToolPolicy` |

### `PII`

Detectores listos: `PIIEmail`, `PIICreditCard` (y otros en `context.go`). Estrategias:

| Constante | Resultado |
|---|---|
| `middleware.PIIRedact` (predeterminado) | `[REDACTED_EMAIL]` |
| `middleware.PIIMask` | `****1234` |
| `middleware.PIIBlock` | Error `*middleware.PIIError` |

Detectores propios: `middleware.PIIKind{Name: "cpf", Pattern: regexp.MustCompile(...)}`. El historial guardado ya contiene el texto saneado.

> La detección es por expresión regular: ayuda, pero no sustituye una revisión de privacidad. Valida con tus datos reales.

### `Summarization` y `ContextEditing`

Controlan el tamaño del contexto en conversaciones largas. `Summarization` reemplaza mensajes antiguos por un resumen generado por el modelo; `ContextEditing` solo limpia resultados antiguos de tools (más barato, sin llamada al modelo). Se pueden usar juntos.

## 9.5 Aprobación humana de tools (`HumanInTheLoop`)

Requiere checkpointer y `thread_id`.

```go
policy := map[string]middleware.ToolPolicy{
	"send_email":   {},                                                   // aprobar, editar o rechazar
	"delete_file":  {Allowed: []string{middleware.Approve, middleware.Reject}}, // sin edición
}

out, _ := agent.Invoke(ctx, graph.State{"messages": "Envía el informe a Bob"})
// se pausa antes de send_email; la solicitud viene en graph.Interrupts(out)

out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
	Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	// o {Type: middleware.Edit, EditedAction: &middleware.ActionRequest{Name: "send_email", Args: map[string]any{...}}}
	// o {Type: middleware.Reject, Message: "no lo envíes"}
}})
```

- `Interrupt.Value` es un `middleware.HITLRequest` con las `ActionRequests` (nombre, argumentos, descripción) y, por tool, las decisiones permitidas.
- Responde con **una decisión por acción**, en el mismo orden.
- `Reject` devuelve al modelo el mensaje informado; él puede proponer otro enfoque.
- Las tools que no están en `Tools` se ejecutan sin revisión.

Ejemplos: `hitl_agent`, `customer_support`.

## 9.6 Middleware propio

Implementa `Name()` y los hooks que quieras:

```go
type Timing struct{}

func (Timing) Name() string { return "timing" }

func (Timing) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	t := time.Now()
	out, err := next(ctx, req) // req.System, req.Messages, req.Tools y req.Model pueden cambiarse antes
	log.Println("el modelo tardó", time.Since(t))
	return out, err
}
```

Middleware que agrega una tool y estado:

```go
type Audit struct{ Log *slog.Logger }

func (Audit) Name() string { return "audit" }
func (Audit) StateSchema() graph.Schema { return graph.Schema{"audit": graph.Append()} }
func (a Audit) WrapToolCall(ctx context.Context, c messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	a.Log.Info("tool", "name", c.Name)
	return next(ctx, c)
}
```

`ModelRequest` expone `Model`, `System`, `Messages`, `Tools`, `Options` y `State`: se puede cambiar el modelo por consulta (enrutamiento por costo), filtrar tools según el contexto o inyectar instrucciones.

### Orden

En los hooks `Wrap*`, el **primer** middleware de la lista es el más externo (llama a los siguientes mediante `next`). Los hooks `Before*`/`After*` se ejecutan en secuencia; no dependas del orden entre ellos. Combínalos según el efecto que buscas (por ejemplo, `ModelRetry` por fuera de `ModelFallback` repite la cadena entera) y prueba la combinación (véase `examples/langchain/middleware_stack`).

## 9.7 Salida estructurada en agents

```go
type Report struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}

agent, _ := agents.Create(agents.Options{
	Model: m,
	ResponseFormat: &models.ResponseFormat{
		Name: "report",
		Schema: map[string]any{ // JSON Schema del tipo Report
			"type": "object",
			"properties": map[string]any{
				"title":   map[string]any{"type": "string"},
				"bullets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"title", "bullets"},
		},
	},
})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Resume el artículo"})
raw := out["structured_response"] // parseado del JSON de la respuesta final
```

Ejemplo completo: `examples/langgraph/structured_output`.

## 9.8 Probar agents sin red

```go
fake := models.NewFake(
	messages.AI("", messages.ToolCall{ID: "1", Name: "get_weather", Args: map[string]any{"city": "Lisboa"}}),
	messages.AI("Soleado en Lisboa."),
)
agent, _ := agents.Create(agents.Options{Model: fake, Tools: []tools.Tool{weather}})
out, _ := agent.Invoke(ctx, graph.State{"messages": "¿tiempo en Lisboa?"})
// verifica out["messages"] y fake.Calls
```

## 9.9 Cuándo usar cada cosa

| Situación | Usa |
|---|---|
| Loop mínimo modelo/tools | `prebuilt.CreateReactAgent` |
| Agent de producción con límites, retry, PII, aprobación | `agents.Create` + middleware |
| Flujo con etapas fijas, ramificaciones y paralelismo | `graph.StateGraph` |
| Tareas largas con planificación, archivos y subagents | `deepagents.Create` |

---

Anterior: [8. Persistencia, HITL y streaming](08-persistencia-hitl-streaming.md) · Siguiente: [10. Deep Agents](10-deep-agents.md) · [Índice del manual](README.md)
