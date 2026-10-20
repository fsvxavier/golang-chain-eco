# rag_chain

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Una chain de recuperación clásica construida con runnables: dividir, generar embeddings, recuperar, armar el prompt y responder.

## Qué hace

- Divide un corpus pequeño con `textsplitters.NewRecursive`, genera embeddings de los fragmentos en un vector store en memoria y arma un retriever con `K: 2`.
- `runnables.Assign` añade un campo `context` (los fragmentos recuperados) al mapa `{"question": ...}`; luego prompt, modelo y parser cierran la chain.
- Pregunta "What are goroutines?" e imprime una respuesta fundamentada en el texto recuperado.

## Cómo aplicarlo

Úsalo cuando el conocimiento cabe en un retriever simple y quieres todo el flujo como un único `Runnable` componible.

```go
store, _ := vectorstores.FromDocuments(ctx, embedder, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})
withCtx := runnables.Assign(map[string]runnables.Runnable[map[string]any, any]{"context": retrieve})
```

Para indexación incremental, búsqueda híbrida o un router entre RAG, CAG y KAG, usa los paquetes `rag` y `knowledge` (capítulo 20).

## Cómo ejecutarlo

```bash
go run ./examples/langchain/rag_chain
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Cambia `embeddings.Fake` por un embedder real (por ejemplo `providers/ollama.Embeddings` o uno compatible con OpenAI) y `FromDocuments` por un vector store duradero de `integrations/vectorstore`.
2. Indexa tus documentos reales una vez, fuera de la ruta de la petición, y mantén el tamaño de chunk y el solapamiento bajo control de versiones.
3. Mide la recuperación antes de la generación: haz 20 preguntas con respuestas conocidas y comprueba que el chunk correcto esté entre los `K` primeros.
4. Mantén la instrucción "responde solo con este contexto" y prueba una pregunta que el corpus no puede responder: el modelo debe decir que no sabe.
5. Registra los chunks recuperados con cada respuesta para rastrear una respuesta errónea hasta la recuperación o la generación.

## Ver también

- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores y retrievers](../../../docs/es-ES/manual/06-rag.md)
- [Manual, capítulo 20: RAG, CAG y KAG](../../../docs/es-ES/manual/20-rag-cag-kag.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
