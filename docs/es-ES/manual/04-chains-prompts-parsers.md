# 4. Prompts, chains y parsers

[English](../../en-US/manual/04-chains-prompts-parsers.md) | [Português](../../pt-BR/manual/04-chains-prompts-parsers.md) | **Español**

## 4.1 Plantillas de prompt (`core/prompts`)

### Plantilla simple

```go
p := prompts.New("Explica {topic} a un {audience}.")
txt, err := p.Format(prompts.Values{"topic": "goroutines", "audience": "principiante"})

p.InputVariables()                 // ["topic", "audience"]
p2 := p.Partial(prompts.Values{"audience": "principiante"}) // fija variables
```

`prompts.Values` es `map[string]any`. Las variables usan llaves `{nombre}`.

### Plantilla de chat

`prompts.Chat` acepta pares `[2]string{rol, plantilla}`, `MessageTemplate`, mensajes estáticos y placeholders:

```go
prompt := prompts.Chat(
	[2]string{"system", "Responde en una frase."},
	prompts.Placeholder{Name: "history", Optional: true}, // inyecta []messages.Message
	[2]string{"human", "Explica {topic}."},
)
msgs, err := prompt.FormatMessages(prompts.Values{"topic": "channels"})
```

Roles aceptados: `system`, `human`/`user`, `ai`/`assistant`, `placeholder`. Ayudantes: `prompts.SystemTemplate`, `HumanTemplate`, `AITemplate`.

### Few-shot

`prompts.FewShot` (texto) y `prompts.FewShotChat` (mensajes) arman ejemplos a partir de una lista de `prompts.Example`. `prompts.LengthSelector` selecciona cuántos ejemplos caben en un límite de tamaño (`ExampleSelector`).

## 4.2 Componer con `Pipe`

`Pipe3` es el equivalente tipado de `prompt | modelo | parser`:

```go
chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
	prompt,                  // Values -> []Message
	models.AsRunnable(m),    // []Message -> Message
	outputparsers.Str{},     // Message -> string
)
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})
```

Los parámetros de tipo son `[Entrada, T1, T2, Salida]`. `Pipe` une 2 etapas, `Pipe3` 3, `Pipe4` 4; para más, encadena pipes.

### Streaming de una chain

```go
for tok, err := range runnables.Stream[prompts.Values, string](ctx, chain, prompts.Values{"topic": "channels"}) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(tok)
}
```

### Lote

```go
outs, err := runnables.Batch(ctx, chain, []prompts.Values{{"topic": "a"}, {"topic": "b"}})
```

`Config.MaxConcurrency` limita el paralelismo.

## 4.3 Output parsers (`core/outputparsers`)

| Parser | Entrada → salida |
|---|---|
| `outputparsers.Str{}` | `Message` → `string` |
| `outputparsers.JSON{}` | `Message` → `any` (JSON decodificado; normalmente `map[string]any`) |
| `outputparsers.Struct[T]{}` | `Message` → `T` (decodifica JSON en una struct) |
| `outputparsers.CommaList{}` | `Message` → `[]string` |
| `outputparsers.ToolCalls{}` | `Message` → llamadas a tools |
| `outputparsers.ToolArgs[T]{Name: "..."}` | `Message` → argumentos `T` de una tool específica |

## 4.4 Combinadores (`core/runnables`)

| Función | Para qué |
|---|---|
| `Lambda(nombre, fn)` | Convertir una función Go en runnable |
| `Parallel(map)` | Ejecutar ramas en paralelo sobre la misma entrada y unirlas en un mapa |
| `Assign(map)` | Agregar claves calculadas a un mapa de entrada |
| `Pick(claves...)` | Seleccionar claves de un mapa |
| `Passthrough[T]()` | Pasar la entrada tal cual |
| `Branch(default, cases...)` | Elegir una rama por condición (`runnables.Case{When, Then}`) |
| `Map(r)` | Aplicar un runnable a cada elemento de una lista |
| `WithRetry(r, opts)` | Repetir con backoff exponencial |
| `WithFallbacks(r, fb...)` | Probar alternativas cuando falla |
| `WithConfigured(r, cfg)` | Fijar una `Config` |
| `Erase(r)` / `Sequence(...)` | Trabajar con `Runnable[any, any]` |

### Ejemplo: chain de RAG con `Lambda`

```go
ragChain := runnables.Pipe4[string, prompts.Values, []messages.Message, messages.Message, string](
	runnables.Lambda("contexto", func(ctx context.Context, q string) (prompts.Values, error) {
		docs, err := retriever.Invoke(ctx, q)
		return prompts.Values{"question": q, "context": retrievers.FormatDocuments(docs)}, err
	}),
	prompts.Chat(
		[2]string{"system", "Responde solo con base en el contexto:\n{context}"},
		[2]string{"human", "{question}"},
	),
	models.AsRunnable(m),
	outputparsers.Str{},
)
answer, err := ragChain.Invoke(ctx, "¿Quién creó Go?")
```

El ejemplo ejecutable completo, con `Parallel`/`Assign`, está en `examples/langchain/rag_chain`.

## 4.5 Retry y fallbacks

```go
robust := runnables.WithRetry(chain, runnables.RetryOptions{
	MaxAttempts:     4,                      // intentos totales (predeterminado 3)
	InitialInterval: 500 * time.Millisecond, // predeterminado 500ms
	BackoffFactor:   2,                      // predeterminado 2
	MaxInterval:     30 * time.Second,       // predeterminado 30s
	Jitter:          true,
	RetryIf: func(err error) bool { return !errors.Is(err, context.Canceled) },
})
safe := runnables.WithFallbacks(robust, backupChain)
```

Un `RetryIf` nulo significa "repetir cualquier error". `runnables.Backoff(ctx, opts, fn)` aplica la misma política a cualquier función.

## 4.6 Salida estructurada

Hace que el modelo devuelva una struct de Go:

```go
type Person struct {
	Name string `json:"name" description:"Nombre completo"`
	Age  int    `json:"age"`
}

extract := models.Structured[Person](m, models.MethodTool) // o models.MethodJSONSchema
p, err := extract.Invoke(ctx, []messages.Message{messages.Human("Ana Souza tiene 31 años.")})
// p == Person{Name: "Ana Souza", Age: 31}
```

- `MethodTool` (predeterminado): fuerza una llamada a tool cuyos argumentos son la salida. Funciona con cualquier modelo que tenga tools.
- `MethodJSONSchema`: usa la función nativa de JSON Schema del proveedor.
- Las tags `description` y `jsonschema` de la struct enriquecen el schema (véase el [capítulo 5](05-tools.md#52-esquema-de-los-argumentos)).

Para agents, usa `agents.Options.ResponseFormat` (capítulo 9).

## 4.7 Historial de chat (`core/chathistory`)

Memoria de conversación para chains simples (sin LangGraph):

```go
store := &chathistory.Store{} // un chathistory.Memory por sesión; define New para otro tipo
withMem := chathistory.WithHistory(models.AsRunnable(m), store.Get)

ctx := runnables.WithConfig(ctx, runnables.Config{
	Configurable: map[string]any{chathistory.SessionKey: "sesion-1"},
})
resp, err := withMem.Invoke(ctx, []messages.Message{messages.Human("Me llamo Ana.")})
resp, err = withMem.Invoke(ctx, []messages.Message{messages.Human("¿Cómo me llamo?")})
```

Implementaciones de `chathistory.History`: `chathistory.Memory` (en memoria), `chathistory.File` (archivo JSON) y `chathistory.Store` (un historial por ID de sesión). Para agents y grafos, prefiere los checkpointers ([capítulo 8](08-persistencia-hitl-streaming.md)).

## 4.8 Utilidades de mensajes para controlar el contexto

```go
trimmed := messages.Trim(msgs, messages.TrimOptions{
	MaxTokens:     4000,
	Strategy:      "last",        // mantiene las más recientes
	IncludeSystem: true,          // preserva el system inicial
	StartOn:       messages.RoleHuman,
})
only := messages.Filter(msgs, []messages.Role{messages.RoleHuman, messages.RoleAI}, nil)
merged := messages.MergeRuns(msgs)
```

El conteo predeterminado es aproximado (`ApproxTokens`); pasa un `Counter` con un contador exacto si lo necesitas.

---

Anterior: [3. Modelos y proveedores](03-modelos-e-provedores.md) · Siguiente: [5. Tools](05-tools.md) · [Índice del manual](README.es-ES.md)
