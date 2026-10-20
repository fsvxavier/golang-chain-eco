# 6. RAG: loaders, splitters, embeddings, vector stores y retrievers

[English](../../en-US/manual/06-rag.md) | [Português](../../pt-BR/manual/06-rag.md) | **Español**

Pipeline típico:

```
Loader ─► Splitter ─► Embedder ─► VectorStore ─► Retriever ─► (chain o tool de agent)
```

## 6.1 Cargar documentos (`core/documents`)

Un `documents.Document` tiene `PageContent` (texto) y `Metadata` (`map[string]any`).

| Loader | Fuente |
|---|---|
| `documents.Text(path)` | Archivo de texto |
| `documents.Reader(r, source)` | Un `io.Reader` |
| `documents.CSV(path)` | Una fila CSV por documento |
| `documents.JSONLines(path, contentKey)` | Un objeto JSON por línea |
| `documents.HTML(path)` | HTML convertido en texto |
| `documents.Directory(root, glob, load)` | Todos los archivos que cumplen el glob |
| `loaders.Web`, `Sitemap`, `JSON`, `Unstructured` | `integrations/loaders` (web, sitemap, JSON, PDF/Office/correo vía Unstructured) |
| `documents.LoaderFunc(fn)` | Loader personalizado |

```go
docs, err := documents.Directory("./docs", "*.md", documents.Text).Load(ctx)
```

> **PDF:** no hay parser de PDF nativo. Usa el loader `Unstructured` o convierte antes.

## 6.2 Dividir en fragmentos (`core/textsplitters`)

```go
chunks := textsplitters.NewRecursive(800, 100).SplitText(text) // tamaño, solapamiento

// Ya con documentos (conserva los metadatos):
split := textsplitters.SplitDocuments(textsplitters.NewRecursive(800, 100), docs)

// Código fuente, respetando funciones y tipos:
goSplit := textsplitters.ForLanguage("go", 1000, 100)

// Markdown por encabezados:
parts := textsplitters.MarkdownHeaders(md, map[string]string{"#": "h1", "##": "h2"})
```

`NewRecursive` intenta cortar en párrafos, luego en líneas, luego en palabras. `Character` corta por un separador fijo. El tamaño predeterminado se mide en caracteres; `LengthFunc` permite medir en tokens.

## 6.3 Vector store en memoria (`core/vectorstores`)

```go
emb, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small") // o embeddings.Fake{Dim: 64} en pruebas
store, err := vectorstores.FromDocuments(ctx, emb, docs)

hits, err := vectorstores.SimilaritySearch(ctx, store, "cancelar pedido", 4, nil)
scored, err := store.SimilaritySearchWithScore(ctx, "cancelar pedido", 4, nil)
mmr, err := store.MaxMarginalRelevance(ctx, "cancelar pedido", 4, 20, 0.5, nil) // k, fetchK, lambda

ids, err := store.AddDocuments(ctx, moreDocs)
store.Delete(ctx, ids)
store.Save("index.json")                       // persiste en archivo
store, err = vectorstores.Load("index.json", emb)
```

Filtros por metadatos: `vectorstores.MetadataEquals(map[string]any{"source": "manual.txt"})` o cualquier `func(documents.Document) bool`.

`vectorstores.Memory` es adecuado para prototipos y conjuntos pequeños (búsqueda lineal). Para volumen, usa un backend de `integrations/vectorstore` (sección 6.6).

## 6.4 Retrievers (`core/retrievers`)

Un `Retriever` recibe una pregunta y devuelve documentos.

```go
retriever := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{
	Type:   retrievers.MMR, // Similarity (predeterminado), MMR o SimilarityThreshold
	K:      4,              // predeterminado 4
	FetchK: 20,             // pool del MMR
	Lambda: 0.5,            // diversidad del MMR
	// ScoreThreshold: 0.7, // con SimilarityThreshold
	// Filter: vectorstores.MetadataEquals(...),
})
hits, err := retriever.Invoke(ctx, "¿cómo cancelo mi pedido?")
```

Composición:

| Función | Efecto |
|---|---|
| `retrievers.Ensemble(rs, pesos)` | Combina varios retrievers (p. ej. vectorial + BM25) |
| `retrievers.MultiQuery(r, model, n)` | El modelo reescribe la pregunta en `n` variaciones y une los resultados |
| `retrievers.Func(fn)` | Retriever a partir de una función |
| `retrieval.NewBM25(docs)` | Búsqueda por palabra clave en Go puro (`integrations/retrieval`) |
| `retrieval.WithRerank(r, reranker)` | Reordena con Cohere/Jina/Voyage |
| `search.Retriever(...)` | Búsqueda en la web como retriever |
| `graphretriever.Retriever` | Graph RAG: sigue enlaces de metadatos |
| `spicedb.FilterRetriever`, `permit.FilterRetriever` | Filtra por el permiso del usuario |

`retrievers.FormatDocuments(docs)` une documentos en texto para ponerlo en el prompt.

## 6.5 RAG agéntico: el retriever como tool

```go
ragTool := retrievers.AsTool(retriever, "search_docs", "Busca en la documentación del producto.")

agent, _ := agents.Create(agents.Options{
	Model:        m,
	Tools:        []tools.Tool{ragTool},
	SystemPrompt: "Usa search_docs antes de responder. Cita la fuente.",
})
```

El modelo decide cuándo y cuántas veces buscar. Variantes listas en los ejemplos: `corrective_rag`, `self_rag` y `adaptive_rag`.

## 6.6 Vector stores de producción (`integrations/vectorstore`)

Todos implementan `vectorstores.VectorStore` detrás del contrato `vectorstore.Backend` (`Upsert`, `Query`, `Delete`). Se arman con `vectorstore.New(embedder, backend)`:

```go
import "github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"

store := vectorstore.New(emb, &vectorstore.Qdrant{URL: "http://localhost:6333", Collection: "docs"})
ids, err := store.AddDocuments(ctx, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})
```

Backends disponibles (detalles y nivel de verificación en [INTEGRATIONS.md](../INTEGRATIONS.md)):

| Categoría | Backends |
|---|---|
| Servicios vectoriales | Qdrant, Chroma, Pinecone, Weaviate, Milvus/Zilliz, Typesense, Upstash, Astra DB |
| Búsqueda | Elasticsearch, Solr/Opensolr, Azure AI Search |
| SQL | PostgreSQL + pgvector, MariaDB 11.7+, CockroachDB, Oracle 23ai, ClickHouse, CrateDB |
| Grafos y documentos | Neo4j, SurrealDB, ArcadeDB, Infinispan, Couchbase, Memgraph (`memgraph.Vector`), FalkorDB (`redis.FalkorDB`) |
| Redis y Mongo | `redis.Vector` (Redis Stack), `mongo.Vector` (Atlas `$vectorSearch` o escaneo) |
| Índices locales | `vectorstore.Flat` (exacto, equivalente a FAISS) y `vectorstore.Annoy` (aproximado); lectura de archivos nativos con `LoadAnnoyFile` y `LoadFAISSFlat` (solo lectura) |

Ejemplo con PostgreSQL (tú aportas el driver):

```go
import _ "github.com/jackc/pgx/v5/stdlib"

db, _ := sql.Open("pgx", dsn)
store := vectorstore.New(emb, &vectorstore.PGVector{DB: db, Table: "docs"}) // Table predeterminada: langchain_pg_embedding
```

El nombre de la tabla se valida contra inyección SQL. Los campos de cada backend difieren (`URL`, `APIKey`, `Collection`...): consulta el tipo en `integrations/vectorstore/` al configurar uno nuevo.

### Índices locales `Flat` y `Annoy`

```go
flat := vectorstore.NewFlat()                          // exacto; flat.Save(path) y vectorstore.LoadFlat(path)
ann := &vectorstore.Annoy{Trees: 20, SearchK: 200, Seed: 7} // aproximado
store := vectorstore.New(emb, flat)
```

Para leer un índice generado por FAISS o Annoy (sin binding, solo lectura):

```go
faissIdx, _ := vectorstore.LoadFAISSFlat("flat.faiss")   // IndexFlatL2, IndexFlatIP o IndexIDMap sobre flat
annIdx, _ := vectorstore.LoadAnnoyFile("idx.ann", 384, "angular") // la dimensión y la métrica no quedan en el archivo
store := vectorstore.New(emb, annIdx)
```

La compatibilidad con los archivos de las bibliotecas oficiales no está cubierta por las pruebas: véase [INTEGRATIONS.md](../INTEGRATIONS.md) para verificar con un archivo generado en Python.

Mira `examples/integrations/vector_indexes` para ver el recall frente a `SearchK` y la persistencia.

## 6.7 Receta completa

```go
ctx := context.Background()
emb, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small")

raw, _ := documents.Directory("./kb", "*.md", documents.Text).Load(ctx)
docs := textsplitters.SplitDocuments(textsplitters.NewRecursive(800, 100), raw)

store, _ := vectorstores.FromDocuments(ctx, emb, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})

chain := runnables.Pipe4[string, prompts.Values, []messages.Message, messages.Message, string](
	runnables.Lambda("ctx", func(ctx context.Context, q string) (prompts.Values, error) {
		d, err := retr.Invoke(ctx, q)
		return prompts.Values{"q": q, "ctx": retrievers.FormatDocuments(d)}, err
	}),
	prompts.Chat([2]string{"system", "Responde con base en:\n{ctx}"}, [2]string{"human", "{q}"}),
	models.AsRunnable(chatmodels.MustInit("openai:gpt-4o-mini")),
	outputparsers.Str{},
)
answer, _ := chain.Invoke(ctx, "¿Cuál es la política de reembolso?")
```

## 6.8 Consejos de calidad

- Ajusta el tamaño y el solapamiento de los fragmentos al tipo de texto (800/100 es un buen punto de partida para prosa).
- Guarda `source` e identificadores en los metadatos para citar fuentes y filtrar.
- Usa MMR cuando los resultados salgan repetitivos; usa `Ensemble` con BM25 cuando importan los términos exactos.
- Mide: prueba preguntas reales y revisa si el fragmento correcto aparece en los `K` primeros.

¿Quieres indexación incremental, búsqueda híbrida lista, caché de prompt (CAG) o grafo de conocimiento (KAG)? Mira el [capítulo 20](20-rag-cag-kag.md).

---

Anterior: [5. Tools](05-tools.md) · Siguiente: [7. LangGraph: StateGraph](07-langgraph.md) · [Índice del manual](README.es-ES.md)
