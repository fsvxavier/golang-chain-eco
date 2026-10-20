# rag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Respuestas con recuperación y citas, búsqueda híbrida (vector + BM25) e indexación incremental.

## Qué hace

- Indexa cinco documentos cortos (en portugués, sobre un router ficticio) con `rag.NewIndexer`.
- Compara la búsqueda vectorial simple con la híbrida (`pipe.Hybrid = true`): el código de error exacto `KRX-9921` lo encuentra BM25 incluso cuando los embeddings lo difuminan.
- Responde con citas `[n]` e imprime los pasos de `Trace` y las fuentes.
- Reindexa: un corpus sin cambios se omite por completo (`Added=0 Skipped=5`), y editar un documento elimina y vuelve a añadir solo sus chunks.

## Cómo aplicarlo

```go
indexer := rag.NewIndexer(store, nil)
stats, _ := indexer.Index(ctx, loader)          // incremental: Added / Skipped / Removed
pipe := &rag.Pipeline{Store: store, Model: model, K: 3, Hybrid: true, Docs: indexer.Documents()}
ans, _ := pipe.Answer(ctx, knowledge.Question{Text: q}) // ans.Text, ans.Sources, ans.Trace
```

Usa RAG cuando el corpus es grande o cambia a menudo. Activa `Hybrid` cuando los usuarios buscan por códigos, nombres o identificadores. La CLI hace lo mismo sobre archivos: `go run ./cmd/knowledge <index|ask|mcp|serve>`.

## Cómo ejecutarlo

```bash
go run ./examples/knowledge/rag
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un embedder real y un vector store duradero (`integrations/vectorstore`), y un modelo real; mantén metadatos de fuente (`source`) en cada documento para que las citas tengan sentido.
2. Indexa con la CLI o con tu propio loader en una programación; la segunda ejecución debe reportar sobre todo `Skipped` (eso es lo que abarata la reindexación).
3. Arma 20-30 preguntas con respuestas conocidas, incluidos códigos/identificadores y preguntas sin respuesta; verifica la recuperación (el chunk correcto entre los K primeros) por separado de la generación.
4. Mantén el comportamiento "no está en el contexto": el modelo debe decir que no sabe.
5. Registra `Trace` y las fuentes por respuesta para depuración y auditoría.

## Ver también

- [Manual, capítulo 20: RAG, CAG y KAG](../../../docs/es-ES/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/es-ES/KNOWLEDGE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
