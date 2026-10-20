# hot_path

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent que cura su propia memoria de largo plazo con las tools `manage_memory` y `search_memory`, aislada por usuario.

## Qué hace

- `langmem.NewIndexedStore` añade búsqueda semántica a un store clave-valor; el namespace de la memoria es `["memories", "{langgraph_user_id}"]`, así que cada usuario tiene un espacio privado.
- El modelo guionizado guarda lo que Alice pide recordar y luego lo recupera. Hacer la misma pregunta como Bob no encuentra nada: las memorias no se filtran entre usuarios.

## Cómo aplicarlo

```go
st := langmem.NewIndexedStore(store.NewMemory(), embedder)
cfg := langmem.ToolConfig{Namespace: langmem.Namespace{"memories", "{langgraph_user_id}"}}
agent, _ := agents.Create(agents.Options{
    Model: model, Tools: []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
    Compile: []graph.CompileOption{graph.WithStore(st)},
})
ctx = langmem.WithConfigurable(ctx, "langgraph_user_id", userID)
```

"Hot path" significa que el agent decide recordar durante la conversación, al coste de una llamada a tool en la latencia del usuario.

## Cómo ejecutarlo

```bash
go run ./examples/langmem/hot_path
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo y un embedder reales y un store duradero (los stores SQL, Redis, MongoDB, DynamoDB y S3 implementan `store.Store`).
2. Define `langgraph_user_id` a partir del usuario autenticado, nunca del cuerpo de la petición; prueba que el usuario B no lee las memorias del usuario A.
3. Decide qué puede guardar el agent (sin secretos, sin categorías sensibles sin consentimiento) y ofrece una forma de listar y borrar las memorias de un usuario.
4. Sigue la cantidad de memorias por usuario y poda o consolida; mira `background` para formar memorias fuera del hot path.

## Ver también

- [Manual, capítulo 16: Memoria a largo plazo (LangMem)](../../../docs/es-ES/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/es-ES/LANGMEM.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
