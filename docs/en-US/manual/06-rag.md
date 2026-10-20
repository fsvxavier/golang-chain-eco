# 6. RAG: loaders, splitters, embeddings, vector stores and retrievers

**English** | [Português](../../pt-BR/manual/06-rag.md) | [Español](../../es-ES/manual/06-rag.md)

Typical pipeline:

```
Loader ─► Splitter ─► Embedder ─► VectorStore ─► Retriever ─► (chain or agent tool)
```

## 6.1 Loading documents (`core/documents`)

A `documents.Document` has `PageContent` (text) and `Metadata` (`map[string]any`).

| Loader | Source |
|---|---|
| `documents.Text(path)` | Text file |
| `documents.Reader(r, source)` | An `io.Reader` |
| `documents.CSV(path)` | One CSV row per document |
| `documents.JSONLines(path, contentKey)` | One JSON object per line |
| `documents.HTML(path)` | HTML converted to text |
| `documents.Directory(root, glob, load)` | All files matching the glob |
| `loaders.Web`, `Sitemap`, `JSON`, `Unstructured` | `integrations/loaders` (web, sitemap, JSON, PDF/Office/email via Unstructured) |
| `documents.LoaderFunc(fn)` | Custom loader |

```go
docs, err := documents.Directory("./docs", "*.md", documents.Text).Load(ctx)
```

> **PDF:** there is no native PDF parser. Use the `Unstructured` loader or convert first.

## 6.2 Splitting into pieces (`core/textsplitters`)

```go
chunks := textsplitters.NewRecursive(800, 100).SplitText(text) // size, overlap

// Already with documents (keeps metadata):
split := textsplitters.SplitDocuments(textsplitters.NewRecursive(800, 100), docs)

// Source code, respecting functions and types:
goSplit := textsplitters.ForLanguage("go", 1000, 100)

// Markdown by headings:
parts := textsplitters.MarkdownHeaders(md, map[string]string{"#": "h1", "##": "h2"})
```

`NewRecursive` tries to break on paragraphs, then lines, then words. `Character` breaks on a fixed separator. The default size is measured in characters; `LengthFunc` lets you measure in tokens.

## 6.3 In-memory vector store (`core/vectorstores`)

```go
emb, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small") // or embeddings.Fake{Dim: 64} in tests
store, err := vectorstores.FromDocuments(ctx, emb, docs)

hits, err := vectorstores.SimilaritySearch(ctx, store, "cancel order", 4, nil)
scored, err := store.SimilaritySearchWithScore(ctx, "cancel order", 4, nil)
mmr, err := store.MaxMarginalRelevance(ctx, "cancel order", 4, 20, 0.5, nil) // k, fetchK, lambda

ids, err := store.AddDocuments(ctx, moreDocs)
store.Delete(ctx, ids)
store.Save("index.json")                       // persists to a file
store, err = vectorstores.Load("index.json", emb)
```

Metadata filters: `vectorstores.MetadataEquals(map[string]any{"source": "manual.txt"})` or any `func(documents.Document) bool`.

`vectorstores.Memory` suits prototypes and small sets (linear search). For volume, use a backend from `integrations/vectorstore` (section 6.6).

## 6.4 Retrievers (`core/retrievers`)

A `Retriever` receives a question and returns documents.

```go
retriever := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{
	Type:   retrievers.MMR, // Similarity (default), MMR or SimilarityThreshold
	K:      4,              // default 4
	FetchK: 20,             // MMR pool
	Lambda: 0.5,            // MMR diversity
	// ScoreThreshold: 0.7, // with SimilarityThreshold
	// Filter: vectorstores.MetadataEquals(...),
})
hits, err := retriever.Invoke(ctx, "how do I cancel my order?")
```

Composition:

| Function | Effect |
|---|---|
| `retrievers.Ensemble(rs, weights)` | Combines several retrievers (e.g. vector + BM25) |
| `retrievers.MultiQuery(r, model, n)` | The model rewrites the question into `n` variations and joins the results |
| `retrievers.Func(fn)` | Retriever from a function |
| `retrieval.NewBM25(docs)` | Keyword search in pure Go (`integrations/retrieval`) |
| `retrieval.WithRerank(r, reranker)` | Reorders with Cohere/Jina/Voyage |
| `search.Retriever(...)` | Web search as a retriever |
| `graphretriever.Retriever` | Graph RAG: follows metadata links |
| `spicedb.FilterRetriever`, `permit.FilterRetriever` | Filters by the user's permission |

`retrievers.FormatDocuments(docs)` joins documents into text to put in the prompt.

## 6.5 Agentic RAG: the retriever as a tool

```go
ragTool := retrievers.AsTool(retriever, "search_docs", "Searches the product documentation.")

agent, _ := agents.Create(agents.Options{
	Model:        m,
	Tools:        []tools.Tool{ragTool},
	SystemPrompt: "Use search_docs before answering. Cite the source.",
})
```

The model decides when and how many times to search. Ready-made variants in the examples: `corrective_rag`, `self_rag` and `adaptive_rag`.

## 6.6 Production vector stores (`integrations/vectorstore`)

All of them implement `vectorstores.VectorStore` behind the `vectorstore.Backend` contract (`Upsert`, `Query`, `Delete`). You assemble them with `vectorstore.New(embedder, backend)`:

```go
import "github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"

store := vectorstore.New(emb, &vectorstore.Qdrant{URL: "http://localhost:6333", Collection: "docs"})
ids, err := store.AddDocuments(ctx, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})
```

Available backends (details and verification level in [INTEGRATIONS.md](../INTEGRATIONS.md)):

| Category | Backends |
|---|---|
| Vector services | Qdrant, Chroma, Pinecone, Weaviate, Milvus/Zilliz, Typesense, Upstash, Astra DB |
| Search | Elasticsearch, Solr/Opensolr, Azure AI Search |
| SQL | PostgreSQL + pgvector, MariaDB 11.7+, CockroachDB, Oracle 23ai, ClickHouse, CrateDB |
| Graphs and documents | Neo4j, SurrealDB, ArcadeDB, Infinispan, Couchbase, Memgraph (`memgraph.Vector`), FalkorDB (`redis.FalkorDB`) |
| Redis and Mongo | `redis.Vector` (Redis Stack), `mongo.Vector` (Atlas `$vectorSearch` or scan) |
| Local indexes | `vectorstore.Flat` (exact, equivalent to FAISS) and `vectorstore.Annoy` (approximate); reading native files with `LoadAnnoyFile` and `LoadFAISSFlat` (read-only) |

Example with PostgreSQL (you supply the driver):

```go
import _ "github.com/jackc/pgx/v5/stdlib"

db, _ := sql.Open("pgx", dsn)
store := vectorstore.New(emb, &vectorstore.PGVector{DB: db, Table: "docs"}) // default Table: langchain_pg_embedding
```

The table name is validated against SQL injection. Each backend's fields differ (`URL`, `APIKey`, `Collection`...): check the type in `integrations/vectorstore/` when configuring a new one.

### Local `Flat` and `Annoy` indexes

```go
flat := vectorstore.NewFlat()                          // exact; flat.Save(path) and vectorstore.LoadFlat(path)
ann := &vectorstore.Annoy{Trees: 20, SearchK: 200, Seed: 7} // approximate
store := vectorstore.New(emb, flat)
```

To read an index generated by FAISS or Annoy (no binding, read-only):

```go
faissIdx, _ := vectorstore.LoadFAISSFlat("flat.faiss")   // IndexFlatL2, IndexFlatIP or IndexIDMap over flat
annIdx, _ := vectorstore.LoadAnnoyFile("idx.ann", 384, "angular") // dimension and metric are not stored in the file
store := vectorstore.New(emb, annIdx)
```

Compatibility with files from the official libraries is not covered by the tests: see [INTEGRATIONS.md](../INTEGRATIONS.md) to verify with a file generated in Python.

See `examples/integrations/vector_indexes` for recall versus `SearchK` and persistence.

## 6.7 Complete recipe

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
	prompts.Chat([2]string{"system", "Answer based on:\n{ctx}"}, [2]string{"human", "{q}"}),
	models.AsRunnable(chatmodels.MustInit("openai:gpt-4o-mini")),
	outputparsers.Str{},
)
answer, _ := chain.Invoke(ctx, "What is the refund policy?")
```

## 6.8 Quality tips

- Adjust chunk size and overlap to the type of text (800/100 is a good start for prose).
- Keep `source` and identifiers in the metadata to cite sources and filter.
- Use MMR when results come back repetitive; use `Ensemble` with BM25 when exact terms matter.
- Measure: test real questions and check whether the right chunk shows up in the first `K`.

Want incremental indexing, ready-made hybrid search, prompt cache (CAG) or a knowledge graph (KAG)? See [chapter 20](20-rag-cag-kag.md).

---

Previous: [5. Tools](05-tools.md) · Next: [7. LangGraph: StateGraph](07-langgraph.md) · [Manual index](README.md)
