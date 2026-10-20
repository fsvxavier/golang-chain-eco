# 5. Tools

[English](../../en-US/manual/05-tools.md) | [Português](../../pt-BR/manual/05-tools.md) | **Español**

Las tools son funciones Go que el modelo puede pedir que se ejecuten. El paquete `core/tools` se encarga del schema, la validación y la conversión de resultados.

## 5.1 Crear una tool tipada

```go
type SearchArgs struct {
	Query string `json:"query" description:"Término de búsqueda"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
}

search := tools.New("search", "Busca en la base de conocimiento.",
	func(ctx context.Context, a SearchArgs) (any, error) {
		return doSearch(a.Query, a.Limit)
	})
```

- El **nombre** y la **descripción** son lo que el modelo lee para decidir cuándo usar la tool. Sé específico.
- La struct de argumentos se convierte en JSON Schema por reflexión.
- El argumento llega ya validado y decodificado; el resultado `any` se convierte en texto (`tools.Stringify`) para el modelo.

## 5.2 Esquema de los argumentos

| Tag | Efecto |
|---|---|
| `json:"nombre"` | Nombre del campo; `omitempty` lo vuelve opcional |
| `description:"..."` | Descripción del campo (la lee el modelo) |
| `jsonschema:"enum=a\|b\|c"` | Valores permitidos |
| `jsonschema:"minimum=1,maximum=20"` | Rango numérico |
| `jsonschema:"required"` | Marca como obligatorio |
| `jsonschema:"default=x"` | Valor predeterminado |
| `jsonschema:"description=..."` | Descripción alternativa |

Los campos sin `omitempty` se tratan como obligatorios. El runtime valida los argumentos recibidos contra el schema antes de llamar a la función.

### Schema manual

Cuando la struct no basta, usa `NewRaw` y entrega el schema listo:

```go
t := tools.NewRaw("run_query", "Ejecuta SQL de solo lectura.",
	&jsonschema.Schema{ /* ... */ },
	func(ctx context.Context, args map[string]any) (any, error) {
		return runQuery(args["sql"].(string))
	})
```

Para decodificar un `map[string]any` en una struct: `tools.Decode(args, &miStruct)`.

## 5.3 Errores

Existen dos tipos de error, con efectos distintos:

```go
// Error "del modelo": vuelve como mensaje de tool y el loop continúa.
// El modelo puede corregir los argumentos e intentar de nuevo.
return nil, tools.Errorf("ciudad %q no encontrada", a.City)

// Error común: el agent lo trata como fallo de ejecución.
return nil, fmt.Errorf("base de datos no disponible: %w", err)
```

Usa `tools.Errorf` para entradas inválidas o casos esperados. Usa un error común para fallos de infraestructura. El tratamiento es configurable en `prebuilt.ToolNode` mediante `ErrorHandler`; el predeterminado es `prebuilt.DefaultErrorHandler`.

## 5.4 Tool terminal

Una tool terminal cierra el loop del agent usando su resultado como respuesta final:

```go
finish := tools.New("finish", "Termina y devuelve la respuesta final.", fn, tools.WithReturnDirect())
```

## 5.5 Resultado con artefacto

Cuando el resultado tiene una parte para el modelo y otra solo para tu código (un archivo, un objeto grande):

```go
return tools.ContentAndArtifact{
	Content:  "Gráfico generado con 120 puntos.", // va al modelo
	Artifact: chartBytes,                          // queda en Message.Artifact
}, nil
```

## 5.6 Cambiar el estado desde una tool

Una tool puede devolver `graph.Command` para actualizar el estado del grafo:

```go
return graph.Command{Update: graph.State{"user_name": "Ana"}}, nil
```

Dentro de la tool:

- `prebuilt.StateOf(ctx)` devuelve el estado actual del grafo.
- `tools.CallID(ctx)` devuelve el ID de la llamada (para armar un mensaje de tool propio).

## 5.7 Usar tools con un modelo directamente

```go
bound := models.BindTools(m, search, weather)
resp, _ := bound.Generate(ctx, msgs)
for _, call := range resp.ToolCalls {
	t, _ := tools.Find(allTools, call.Name)
	out, err := t.Call(ctx, call.Args) // ejecución manual
	...
}
```

En la práctica, deja que `agents.Create` o `prebuilt.ToolNode` hagan ese loop (capítulo 9). Para elegir el comportamiento del modelo: `models.WithToolChoice("any")` obliga a una llamada; `"none"` la prohíbe.

## 5.8 Tools listas del proyecto

| Origen | Tool |
|---|---|
| `retrievers.AsTool(retriever, nombre, descripción)` | Búsqueda en un retriever (RAG agéntico) |
| `search.Tool(nombre, descripción, searcher, n)` | Búsqueda en la web (Tavily, Exa, Brave...) |
| `mcp.LoadTools(...)` | Tools de servidores MCP |
| `jenkins`, `kubernetes`, `salesforce`, `messaging`, `aws` (Lambda) | Acciones en sistemas externos |
| `todo` y el filesystem de los Deep Agents | `write_todos`, `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `task` |

> **Atención:** las tools que envían SMS, ejecutan shell, leen datos de CRM o disparan builds actúan sobre sistemas reales. Restringe el alcance y exige aprobación humana (capítulo 9, `HumanInTheLoop`).

## 5.9 Buenas prácticas

1. Una tool, una responsabilidad. Nombres en `snake_case`, verbos claros (`get_weather`, `create_ticket`).
2. Describe cada campo con `description`; el modelo lo usa para completar los argumentos.
3. Devuelve resultados cortos y útiles. Los resultados enormes consumen contexto; usa `ContentAndArtifact` o resume.
4. Haz idempotentes las tools con efectos colaterales cuando sea posible, porque un retry puede repetir la llamada.
5. Valida las reglas de negocio dentro de la tool y responde con `tools.Errorf` para que el modelo se corrija.

---

Anterior: [4. Prompts, chains y parsers](04-chains-prompts-parsers.md) · Siguiente: [6. RAG](06-rag.md) · [Índice del manual](README.es-ES.md)
