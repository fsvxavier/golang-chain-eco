# 6. RAG: loaders, splitters, embeddings, vector stores e retrievers

Pipeline típico:

```
Loader ─► Splitter ─► Embedder ─► VectorStore ─► Retriever ─► (chain ou tool de agent)
```

## 6.1 Carregar documentos (`core/documents`)

Um `documents.Document` tem `PageContent` (texto) e `Metadata` (`map[string]any`).

| Loader | Fonte |
|---|---|
| `documents.Text(path)` | Arquivo de texto |
| `documents.Reader(r, source)` | Um `io.Reader` |
| `documents.CSV(path)` | Uma linha CSV por documento |
| `documents.JSONLines(path, contentKey)` | Um objeto JSON por linha |
| `documents.HTML(path)` | HTML convertido em texto |
| `documents.Directory(root, glob, load)` | Todos os arquivos que casam com o glob |
| `loaders.Web`, `Sitemap`, `JSON`, `Unstructured` | `integrations/loaders` (web, sitemap, JSON, PDF/Office/e-mail via Unstructured) |
| `documents.LoaderFunc(fn)` | Loader customizado |

```go
docs, err := documents.Directory("./docs", "*.md", documents.Text).Load(ctx)
```

> **PDF:** não há parser de PDF nativo. Use o loader `Unstructured` ou converta antes.

## 6.2 Dividir em pedaços (`core/textsplitters`)

```go
chunks := textsplitters.NewRecursive(800, 100).SplitText(text) // tamanho, sobreposição

// Já com documentos (preserva metadados):
split := textsplitters.SplitDocuments(textsplitters.NewRecursive(800, 100), docs)

// Código-fonte, respeitando funções e tipos:
goSplit := textsplitters.ForLanguage("go", 1000, 100)

// Markdown por cabeçalhos:
parts := textsplitters.MarkdownHeaders(md, map[string]string{"#": "h1", "##": "h2"})
```

`NewRecursive` tenta quebrar em parágrafos, depois linhas, depois palavras. `Character` quebra por um separador fixo. O tamanho padrão é medido em caracteres; `LengthFunc` permite medir em tokens.

## 6.3 Vector store em memória (`core/vectorstores`)

```go
emb, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small") // ou embeddings.Fake{Dim: 64} em testes
store, err := vectorstores.FromDocuments(ctx, emb, docs)

hits, err := vectorstores.SimilaritySearch(ctx, store, "cancelar pedido", 4, nil)
scored, err := store.SimilaritySearchWithScore(ctx, "cancelar pedido", 4, nil)
mmr, err := store.MaxMarginalRelevance(ctx, "cancelar pedido", 4, 20, 0.5, nil) // k, fetchK, lambda

ids, err := store.AddDocuments(ctx, moreDocs)
store.Delete(ctx, ids)
store.Save("index.json")                       // persiste em arquivo
store, err = vectorstores.Load("index.json", emb)
```

Filtros por metadados: `vectorstores.MetadataEquals(map[string]any{"source": "manual.txt"})` ou qualquer `func(documents.Document) bool`.

`vectorstores.Memory` é adequado para protótipos e conjuntos pequenos (busca linear). Para volume, use um backend de `integrations/vectorstore` (seção 6.6).

## 6.4 Retrievers (`core/retrievers`)

Um `Retriever` recebe uma pergunta e devolve documentos.

```go
retriever := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{
	Type:   retrievers.MMR, // Similarity (padrão), MMR ou SimilarityThreshold
	K:      4,              // padrão 4
	FetchK: 20,             // pool do MMR
	Lambda: 0.5,            // diversidade do MMR
	// ScoreThreshold: 0.7, // com SimilarityThreshold
	// Filter: vectorstores.MetadataEquals(...),
})
hits, err := retriever.Invoke(ctx, "como cancelar o pedido?")
```

Composição:

| Função | Efeito |
|---|---|
| `retrievers.Ensemble(rs, pesos)` | Combina vários retrievers (ex.: vetorial + BM25) |
| `retrievers.MultiQuery(r, model, n)` | O modelo reescreve a pergunta em `n` variações e junta os resultados |
| `retrievers.Func(fn)` | Retriever a partir de uma função |
| `retrieval.NewBM25(docs)` | Busca por palavra-chave em Go puro (`integrations/retrieval`) |
| `retrieval.WithRerank(r, reranker)` | Reordena com Cohere/Jina/Voyage |
| `search.Retriever(...)` | Busca na web como retriever |
| `graphretriever.Retriever` | Graph RAG: segue links de metadados |
| `spicedb.FilterRetriever`, `permit.FilterRetriever` | Filtra por permissão do usuário |

`retrievers.FormatDocuments(docs)` junta documentos em texto para colocar no prompt.

## 6.5 RAG agêntico: o retriever como tool

```go
ragTool := retrievers.AsTool(retriever, "search_docs", "Busca na documentação do produto.")

agent, _ := agents.Create(agents.Options{
	Model:        m,
	Tools:        []tools.Tool{ragTool},
	SystemPrompt: "Use search_docs antes de responder. Cite a fonte.",
})
```

O modelo decide quando e quantas vezes buscar. Variantes prontas nos exemplos: `corrective_rag`, `self_rag` e `adaptive_rag`.

## 6.6 Vector stores de produção (`integrations/vectorstore`)

Todos implementam `vectorstores.VectorStore` por trás do contrato `vectorstore.Backend` (`Upsert`, `Query`, `Delete`). Você monta com `vectorstore.New(embedder, backend)`:

```go
import "github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"

store := vectorstore.New(emb, &vectorstore.Qdrant{URL: "http://localhost:6333", Collection: "docs"})
ids, err := store.AddDocuments(ctx, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})
```

Backends disponíveis (detalhes e nível de verificação em [INTEGRATIONS.md](../INTEGRATIONS.md)):

| Categoria | Backends |
|---|---|
| Serviços vetoriais | Qdrant, Chroma, Pinecone, Weaviate, Milvus/Zilliz, Typesense, Upstash, Astra DB |
| Busca | Elasticsearch, Solr/Opensolr, Azure AI Search |
| SQL | PostgreSQL + pgvector, MariaDB 11.7+, CockroachDB, Oracle 23ai, ClickHouse, CrateDB |
| Grafos e documentos | Neo4j, SurrealDB, ArcadeDB, Infinispan, Couchbase, Memgraph (`memgraph.Vector`), FalkorDB (`redis.FalkorDB`) |
| Redis e Mongo | `redis.Vector` (Redis Stack), `mongo.Vector` (Atlas `$vectorSearch` ou varredura) |
| Índices locais | `vectorstore.Flat` (exato, equivalente ao FAISS) e `vectorstore.Annoy` (aproximado); leitura de arquivos nativos com `LoadAnnoyFile` e `LoadFAISSFlat` (só leitura) |

Exemplo com PostgreSQL (você fornece o driver):

```go
import _ "github.com/jackc/pgx/v5/stdlib"

db, _ := sql.Open("pgx", dsn)
store := vectorstore.New(emb, &vectorstore.PGVector{DB: db, Table: "docs"}) // Table padrão: langchain_pg_embedding
```

O nome da tabela é validado contra injeção de SQL. Os campos de cada backend diferem (`URL`, `APIKey`, `Collection`...): consulte o tipo em `integrations/vectorstore/` ao configurar um novo.

### Índices locais `Flat` e `Annoy`

```go
flat := vectorstore.NewFlat()                          // exato; flat.Save(path) e vectorstore.LoadFlat(path)
ann := &vectorstore.Annoy{Trees: 20, SearchK: 200, Seed: 7} // aproximado
store := vectorstore.New(emb, flat)
```

Para ler um índice gerado pelo FAISS ou pelo Annoy (sem binding, só leitura):

```go
faissIdx, _ := vectorstore.LoadFAISSFlat("flat.faiss")   // IndexFlatL2, IndexFlatIP ou IndexIDMap sobre flat
annIdx, _ := vectorstore.LoadAnnoyFile("idx.ann", 384, "angular") // dimensão e métrica não ficam no arquivo
store := vectorstore.New(emb, annIdx)
```

A compatibilidade com arquivos das bibliotecas oficiais não é coberta pelos testes: veja [INTEGRATIONS.md](../INTEGRATIONS.md) para verificar com um arquivo gerado em Python.

Veja `examples/integrations/vector_indexes` para recall versus `SearchK` e persistência.

## 6.7 Receita completa

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
	prompts.Chat([2]string{"system", "Responda com base em:\n{ctx}"}, [2]string{"human", "{q}"}),
	models.AsRunnable(chatmodels.MustInit("openai:gpt-4o-mini")),
	outputparsers.Str{},
)
answer, _ := chain.Invoke(ctx, "Qual é a política de reembolso?")
```

## 6.8 Dicas de qualidade

- Ajuste tamanho e sobreposição dos pedaços ao tipo de texto (800/100 é um bom começo para prosa).
- Guarde `source` e identificadores nos metadados para citar fontes e filtrar.
- Use MMR quando os resultados vierem repetitivos; use `Ensemble` com BM25 quando termos exatos importam.
- Meça: teste perguntas reais e veja se o pedaço certo aparece nos `K` primeiros.

Próximo: [7. LangGraph: StateGraph](07-langgraph.md).
