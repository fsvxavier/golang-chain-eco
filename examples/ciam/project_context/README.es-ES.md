# project_context

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Contexto barato y preciso de proyecto para un asistente de IA: índice incremental, búsqueda híbrida, compresión, decisiones (ADR/PRD/Plan), memoria y un servidor MCP.

## Qué hace

- Indexa un pequeño proyecto estilo Django más un ADR, un PRD y un plan, de forma incremental (la segunda ejecución no genera embeddings de nada; editar un archivo genera embeddings de uno).
- Búsqueda híbrida (vector + BM25, ranqueada con RRF) filtrada por tipo de chunk, compresión que conserva firmas y descarta documentación, `DecisionContext` (código más los ADR/PRD detrás de él), `Remember`/`Recall`, sustitución de ADR y las mismas capacidades servidas como servidor MCP.
- Imprime la estimación de tokens ahorrados frente a leer los archivos.

## Cómo aplicarlo

```go
m := ciam.New(embedder, nil)
m.Index(ctx, root, ciam.IndexOptions{})
res, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "send receipt", Types: []string{"task"}, Limit: 3})
dc, _ := m.DecisionContext(ctx, "shop", "invoice total cents stored", 3)
srv := ciam.Toolkit{M: m, Project: root}.MCPServer(nil) // for VS Code / Cursor / Claude
```

O usa la CLI: `go run ./cmd/ciam index|search|decision|mcp`. Escribe el PORQUÉ como ADR para que el próximo asistente (o persona) lo encuentre.

## Cómo ejecutarlo

```bash
go run ./examples/ciam/project_context
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Cambia `embeddings.Fake{}` por un embedder real (el comentario apunta a `providers/ollama.Embeddings`) y reindexa; comprueba la calidad de la búsqueda con 20 preguntas reales sobre tu código.
2. Indexa tu repositorio real y mantén el índice fuera del repo; vuelve a ejecutar `Index` en CI o en un hook de git (es incremental).
3. Registra el servidor MCP en tu editor (`go run ./cmd/ciam mcp`) y confirma que el asistente llama a tools `ciam_*` en lugar de leer archivos enteros.
4. Revisa los documentos ADR/PRD como código; los ADR se sustituyen, nunca se editan.
5. Sigue `Status` (búsquedas, cache hits, tokens ahorrados) para confirmar que compensa.

## Ver también

- [Manual, capítulo 18: ciam: contexto de proyecto barato y preciso para asistentes](../../../docs/es-ES/manual/18-ciam-contexto.md)
- [CIAM.md](../../../docs/es-ES/CIAM.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
