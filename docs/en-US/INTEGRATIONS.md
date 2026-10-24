# Integrations

**English** | [Português](../pt-BR/INTEGRATIONS.md) | [Español](../es-ES/INTEGRATIONS.md)

The official LangChain list has 504 unique providers ([all providers](https://docs.langchain.com/oss/python/integrations/providers/all_providers)).
Most of them are a long tail of third parties (guardrails, memories, wallets, niche APIs). This project implements the core providers by
**protocol** and covers the long tail through extension points. The status of each one is in [PROVIDERS.md](PROVIDERS.md) (matrix) and
[PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md) (what is missing and why).

**How it was verified.** Databases and infrastructure services: tested against the real service in Docker (see
[Testing against real services](#testing-against-real-services)). Paid API or SaaS services (watsonx, Replicate, Azure AI Search, Vectara,
Permit, Salesforce, AWS Kendra/Knowledge Bases/Lambda, SMS Plivo/Telnyx, paid search): contract tests with `httptest`, written from the
public documentation; they may diverge from the real service. No test in the standard suite calls the network: the real-service tests skip themselves.

See also the usage in the [README](../../README.md).
Manuals for the router with several gateways and for MCP clients (VS Code, Claude Code, Kilo Code, Copilot CLI, Opencode, Cursor): [docs/integrations](../integrations/en-US/README.md).

## Chat models and embeddings (`chatmodels`, `providers/*`)

| Provider | Package / prefix in `chatmodels.Init` | Chat | Tools | Stream | Embeddings |
|---|---|---|---|---|---|
| OpenAI | `openai` | yes | yes | SSE | yes |
| Azure OpenAI | `azure_openai` (`openai.Azure`) | yes | yes | SSE | yes |
| Anthropic | `anthropic` | yes | yes | SSE | - |
| Google Gemini | `google_genai` / `google` | yes | yes | SSE | yes |
| Google Vertex AI | `google_vertexai` (OAuth token in `GOOGLE_OAUTH_ACCESS_TOKEN`) | yes | yes | SSE | - |
| Cohere | `cohere` | yes | yes | SSE | yes + rerank |
| AWS Bedrock (Converse) | `bedrock` | yes | yes | single response* | - |
| Ollama | `ollama` | yes | yes | NDJSON | yes |
| IBM watsonx.ai | `ibm` / `watsonx` (IAM token with cache) | yes | yes | SSE | yes |
| Replicate | `replicate` | yes (text) | no | single response | - |

\* The Bedrock binary stream (`vnd.amazon.eventstream`) is not decoded; `Stream` returns the full response at once.
AWS SigV4 signing is implemented without an SDK and validated against AWS's official test vector.

**OpenAI-compatible** (same client, `<PROVIDER>_API_KEY` and `<PROVIDER>_BASE_URL`): `groq`, `together`, `openrouter`,
`deepseek`, `xai`, `mistralai`, `fireworks`, `perplexity`, `cerebras`, `nvidia`, `sambanova`, `nebius`, `novita`, `deepinfra`,
`hyperbolic`, `huggingface`, `moonshot`, `zhipuai`, `dashscope` (Alibaba), `upstage`, `writer`, `ai21`, `baseten`, `github`,
`sarvam`, `featherless`, `modelscope`, `aimlapi`, `netmind`, `digitalocean`, `predictionguard`, `baidu`, `naver`, `helicone`, `portkey`, `lambda`,
`friendli`, `telnyx`, and the local servers `vllm`, `lmstudio`, `llamacpp`, `localai`, `litellm`. The per-tenant endpoints (`databricks`, `cloudflare`,
`snowflake`, `runpod`, `truefoundry`, `kserve`, `rayserve`) and 23 third-party gateways (Doubleword, Hubris, FuturMix...) have no default URL: set
`$<PROVIDER>_BASE_URL`, otherwise `Init` returns a clear error. For embeddings: `voyageai`, `jina`, `nomic` and the
above that expose `/embeddings`. Any other compatible service is wired with `openai.Chat{BaseURL: ..., APIKey: ...}`
(this covers OCI, Azure AI and similar proxies).

Rerank: `chatmodels.NewReranker("cohere:rerank-v3.5" | "jina:jina-reranker-v2-base-multilingual" | "voyageai:rerank-2")`.

## Vector stores (`integrations/vectorstore`)

All of them implement `vectorstores.VectorStore` (similarity search with score, MMR, filter, delete) through the `Backend` interface:

| Database | Type | Notes |
|---|---|---|
| Qdrant | `vectorstore.Qdrant` | REST; IDs become deterministic UUIDs |
| Chroma | `vectorstore.Chroma` | v2 API, cosine collection |
| Pinecone | `vectorstore.Pinecone` | index created beforehand; namespace |
| Elasticsearch | `vectorstore.Elasticsearch` | `dense_vector` + kNN; API key or basic auth |
| Weaviate | `vectorstore.Weaviate` | REST + GraphQL, own vectors |
| Milvus / Zilliz | `vectorstore.Milvus` | REST v2 |
| PostgreSQL + pgvector | `vectorstore.PGVector` | `database/sql` (bring the driver); table name validated; tested against real Postgres 16 |
| Redis Stack (RediSearch) | `redis.Vector` | FLAT cosine index via `FT.SEARCH` KNN; own RESP client |
| MongoDB / Atlas | `mongo.Vector` | with `Index`: `$vectorSearch` (Atlas, index created in Atlas); without `Index`: scan + cosine on the client |
| Cassandra / Astra DB | `vectorstore.Astra` | Data API over HTTP (Astra, HCD); native CQL is not used |
| Typesense | `vectorstore.Typesense` | REST, KNN via `vector_query`; `httptest` only (the Typesense binary fails in this environment) |
| Upstash Vector | `vectorstore.Upstash` | REST; index created in the console; `httptest` only |
| Neo4j | `vectorstore.Neo4j` | HTTP Query API (5.19+), native vector index; tested against real Neo4j 5 |
| SurrealDB | `vectorstore.SurrealDB` | HTTP `/sql`, exact cosine search; tested against real SurrealDB |
| MariaDB 11.7+ | `vectorstore.MariaDB` | `database/sql` + `VECTOR INDEX`; ASCII ids ≤ 64 bytes; tested against real MariaDB 11.8 |
| FalkorDB | `redis.FalkorDB` | graph over Redis, native vector index; tested against real FalkorDB |
| ClickHouse / chDB | `vectorstore.ClickHouse` | HTTP, exact `cosineDistance`; tested against ClickHouse 24.8 |
| CrateDB | `vectorstore.CrateDB` | HTTP `/_sql`, `knn_match` (Euclidean); tested against CrateDB 5.9 |
| CockroachDB | `vectorstore.CockroachDB` | `database/sql`, native `VECTOR` type, exact `<=>`; tested against CockroachDB |
| Oracle AI Database 23ai | `vectorstore.Oracle` | `database/sql` (go-ora), `VECTOR_DISTANCE`; tested against Oracle Free 23ai; hits without embedding |
| Apache Solr / Opensolr | `vectorstore.Solr` | `DenseVectorField` + `{!knn}`; tested against Solr 9.7 |
| ArcadeDB | `vectorstore.ArcadeDB` | HTTP, exact `vectorCosineSimilarity`; tested against ArcadeDB |
| Infinispan | `vectorstore.Infinispan` | REST + Digest + Protobuf `@Vector`, `<->`; tested against Infinispan 15; hits without embedding |
| Couchbase | `vectorstore.Couchbase` | SQL++ with array arithmetic (exact cosine, no vector index); tested against Couchbase Community |
| Memgraph | `memgraph.Vector` | own Bolt client + `vector_search`; tested against Memgraph 3 |
| Azure AI Search | `vectorstore.AzureAISearch` | REST, HNSW cosine, index created on the first `Upsert`; contract test only (`httptest`) |
| FAISS (equivalent) | `vectorstore.Flat` | exact index in pure Go, `Save`/`LoadFlat`; not a FAISS binding |
| FAISS (read) | `vectorstore.LoadFAISSFlat` | reads `IndexFlatL2` (`IxF2`), `IndexFlatIP` (`IxFI`) and `IndexIDMap` over one of them (`IxMp`); read-only |
| Annoy (equivalent) | `vectorstore.Annoy` | forest of random projection trees, pure Go; recall measured in tests |
| Annoy (read) | `vectorstore.LoadAnnoyFile` | reads the native `.ann` (`angular` and `euclidean`); exact brute-force search; read-only |
| HNSW (graph index) | `vectorstore.NewHNSW()` | hierarchical graph in pure Go, cosine; `Save`/`LoadHNSW`; deletes are tombstones; recall measured in tests |

Full matrix of LangChain's 504 providers (what exists and what does not): [PROVIDERS.md](PROVIDERS.md).

For a new database, implement `Upsert`, `Query` and `Delete` (3 methods) and use `vectorstore.New(embedder, backend)`.
Real FAISS and Annoy are C++ libraries (they would require cgo); `Flat`, `Annoy` and `HNSW` cover the same use in pure Go. For ready-made indexes,
`LoadAnnoyFile` and `LoadFAISSFlat` read the native file format (only the types listed above).

**Compatibility verified with real files.** `integrations/vectorstore/realfiles_test.go` reads files generated by the official libraries
(annoy 1.17.3 and faiss-cpu 1.15.1, in `testdata/real_*`, regenerated by `testdata/gen_real.py`) and compares the top-k and the scores with the
search done by the libraries themselves. This test found and fixed a bug: FAISS writes the code size in number of floats, not bytes.
Server-less alternatives: `core/vectorstores.Memory` (with `Save`/`Load`) and `integrations/retrieval.BM25`.

## Objects, permissions, CI, sandboxes and protocols

| Package | What it does | Real test |
|---|---|---|
| `integrations/s3` | S3 client (SigV4) + `checkpoint.Saver`, `store.Store` and loader; serves AWS S3, Tigris, XNS, R2, MinIO... | Zenko CloudServer (validates the signature) |
| `integrations/spicedb` | SpiceDB HTTP client + `FilterRetriever` that returns only documents the user can see | SpiceDB |
| `integrations/jenkins` | trigger a build, status, console; 3 agent tools | Jenkins LTS |
| `integrations/kubernetes` | API client (pods, logs, exec over WebSocket) and `Sandbox`: a throwaway pod becomes a deepagents backend | k3s |
| `deepagents/backends.Remote` | turns any "run command" into a full backend (files + shell); base for Docker, SSH and cloud sandboxes | local shell + k3s |
| `integrations/graphretriever` | graph-traversal retriever over a metadata graph (Graph RAG) on top of any vector store | offline |
| `integrations/agui` | serves a graph over the AG-UI protocol (SSE) for CopilotKit, assistant-ui and others | `httptest` |

### Group B: public API contract, no real service here

Covered only by contract tests (`httptest`) written from each API's public documentation; they may diverge from the real service:
`providers/watsonx` (IBM watsonx.ai: chat, streaming, embeddings, IAM token with cache), `providers/replicate`, `integrations/vectara` (retriever),
`integrations/permit` (permission-filtered retriever), `integrations/messaging` (SMS Plivo/Telnyx, with per-destination `Allow`),
`integrations/salesforce` (read-only SOQL), `integrations/aws` (Bedrock Knowledge Bases, Kendra and Lambda tool, SigV4),
`vectorstore.AzureAISearch`, and the Zotero, YouTube Data API, Perigon and Oxylabs searches. Third-party OpenAI-compatible gateways (Doubleword,
Hubris, FuturMix...) are in the `chatmodels` table and require `$<PROVIDER>_BASE_URL`.

## Search and web (`integrations/search`)

`Tavily`, `Exa`, `Brave`, `Serper`, `SerpAPI`, `SearXNG`, `DuckDuckGo` (HTML, fragile), `Wikipedia`, `ArXiv`, `HackerNews`,
`ScraperAPI`, `ScrapingBee`, `Brightdata`, `Diffbot` (pages via `ScrapeTool`), `YouCom`, `Linkup`, `PerplexitySearch`, `SearchAPI`, `JinaSearch`, `StackOverflow`, `Reddit`, `OpenAlex`, `Apify` (any Actor).
Each one implements `Searcher` and becomes a tool (`search.Tool`) or a retriever (`search.Retriever`). Also: `Firecrawl`, `JinaReader` and
`Fetcher` (GET of pages with **SSRF blocking**: refuses loopback/private networks, even after DNS resolution).

## Knowledge graphs (`integrations/graphstore`)

| Database | Type | Transport | Verification |
|---|---|---|---|
| Neo4j 5.19+ / Aura | `graphstore.Neo4j` | HTTP Query API | tested against Neo4j 5.26 in Docker (`NEO4J_URL`) |
| Memgraph | `graphstore.Memgraph` | Bolt 4.x (`integrations/memgraph`) | tested against Memgraph in Docker (`MEMGRAPH_ADDR`) |

They implement `kag.GraphStore` and pass the `conformance.Graph` suite, the same one used by the in-memory graph. Details, Cypher identifier safety and the differences between the two in
[KNOWLEDGE.md](KNOWLEDGE.md#integrationsgraphstore-graph-on-neo4j-and-memgraph).

## Loaders and retrievers

- `core/documents`: text, CSV, JSONL, HTML, directory. `integrations/loaders`: `Web`, `Sitemap`, `JSON`, `Unstructured` (PDF, Office, email, images, via API or your own instance).
- `integrations/retrieval`: `BM25` (pure Go) and `WithRerank` (any `Reranker`, e.g. Cohere/Jina/Voyage).
- There is no native PDF parser (the standard library has none): use `Unstructured` or convert first.

## Persistence

`langgraph/checkpoint/sqlsaver` and `langgraph/store/sqlstore` for SQLite/PostgreSQL, `checkpoint.Memory`, `store.Memory`.

| Backend | Saver | Store | Notes |
|---|---|---|---|
| Redis | `redis.NewSaver` | `redis.NewStore` | own RESP2 client (AUTH, SELECT) |
| MongoDB / Atlas | `mongo.NewSaver` | `mongo.NewStore` | own OP_MSG + BSON; TLS and SCRAM-SHA-256; no `mongodb+srv://` (pass host:port) |
| DynamoDB | `dynamodb.NewSaver` | `dynamodb.NewStore` | JSON API + SigV4; creates tables on demand; item ≤ 400 KB; `Search` does a Scan |

For another database, implement `checkpoint.Saver` (4 methods) or `store.Store`.

## Testing against real services

`internal/conformance` contains the shared suites (Saver, Store, Vector). Integration tests skip themselves when the variable
is not set:

```bash
docker run -d -p 6379:6379 redis/redis-stack-server        # REDIS_ADDR=localhost:6379
docker run -d -p 27017:27017 mongo:7                       # MONGO_ADDR=localhost:27017 (MONGO_USER/MONGO_PASS for SCRAM)
docker run -d -p 8000:8000 amazon/dynamodb-local           # DYNAMODB_ENDPOINT=http://localhost:8000
docker run -d -p 8001:8000 zenko/cloudserver               # S3_ENDPOINT=http://localhost:8001 S3_KEY=accessKey1 S3_SECRET=verySecretKey1
docker run -d -e POSTGRES_PASSWORD=pw -p 5432:5432 pgvector/pgvector:pg16
docker run -d -p 7474:7474 -p 7688:7687 -e NEO4J_AUTH=neo4j/lcgtestpass neo4j:5.26   # NEO4J_URL=http://localhost:7474 NEO4J_USER=neo4j NEO4J_PASS=lcgtestpass
docker run -d -p 7687:7687 memgraph/memgraph               # MEMGRAPH_ADDR=localhost:7687
PG_DSN=postgres://postgres:pw@localhost:5432/postgres go test ./internal/pgtest/...   # pgx driver (test only)
```

Verified against real services: ClickHouse, CrateDB, CockroachDB, Oracle Free 23ai, Solr, ArcadeDB, Infinispan, Couchbase, Memgraph, Zenko CloudServer (S3), SpiceDB, Jenkins, k3s, Redis Stack, MongoDB 7 (with and without authentication), DynamoDB Local, PostgreSQL 16 + pgvector
(includes `sqlsaver`/`sqlstore`), Neo4j 5, SurrealDB and MariaDB 11.8. **Not verified against the real service**: Atlas `$vectorSearch`, Astra DB (`httptest` only), Qdrant, Chroma,
Pinecone, Elasticsearch, Weaviate, Milvus, Typesense, Upstash, Astra, LLM providers and tracers (`httptest` only).

## Observability

| Destination | Package |
|---|---|
| LangSmith | `tracing/langsmith` |
| Langfuse | `tracing/langfuse` |
| OpenTelemetry (OTLP/HTTP): Jaeger, Tempo, Honeycomb, Datadog, New Relic, Grafana | `tracing/otlp` |
| Log/console | `core/callbacks` (`Logger`, `StdOut`) |

## Agent server and client

`remote/server` serves compiled graphs through the threads/runs API; `remote.Client` and `remote.Graph` consume them (and work with the
official Agent Server). `deepagents/middleware.AsyncSubagents` uses both.

## Out of scope (long tail)

Third-party agent guardrails and governance, wallets/payments (x402, crypto), startup memories, niche data APIs and the
managed sandboxes (E2B, Daytona, Modal, Runloop) are not included; the full list and the reason for each one are in
[PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md). The extension paths are: a `tools.Tool` (`tools.New`), an MCP server
(`mcp.LoadTools`, which gives access to thousands of existing integrations), a `vectorstore.Backend`, a `checkpoint.Saver`/`store.Store`, a
command runner plugged into `backends.Remote` (any sandbox becomes a file and shell backend) or a `callbacks.Handler`.

## Code graph (`integrations/codegraph`)

Searching a project by meaning costs far fewer tokens than grepping and reading files. When a directory has a CodeGraph index (`.codegraph` or `.codegraph-wsl`) or a graphify one (`graphify-out/graph.json`), the `codegraph` and `graphify` programs answer a question with the relevant symbols' source and their call paths in one call.

```go
if t := codegraph.ProjectTool("."); t != nil { // nil when the directory has no index
	opts.Tools = append(opts.Tools, t) // the agent gets code_graph
}
```

- `Detect(dir)` lists the engines whose index sits directly in `dir` (CodeGraph first; for graphify, the newest `graph.json` with at least one node anywhere under `graphify-out/`, because graphify keeps dated snapshots such as `graphify-out/2026-10-13/graph.json` and the top-level file can be empty after an update that indexed nothing); an index above `dir` is ignored on purpose, since it could cover directories the caller did not mean to expose.
- `Ask(ctx, run, dir, engine, query)` runs one question; `Tool(name, resolve, run, done)` builds a tool whose `resolve` decides which directories the agent may ask (the `full_team_front` `backend_graph` limits it to the demand's roots and the workspace); `ProjectTool(root)` is the one-directory version.
- The engines run without a shell, with a minimal environment, a 30 second timeout and a 24 KB cap on the answer; the query is one argument that is not empty, not longer than 500 bytes and does not start with `-`.
- `deepagents.Create` adds `code_graph` by itself when its local backend has an index; every agent of `full_team_front` has `backend_graph`.

Test it: `go test -race -count=1 ./integrations/codegraph`; with a real index, `GRAPH_TEST_ROOT=/path/to/project go test -run TestRealCodeGraphIfAvailable ./examples/langgraph/full_team_front/internal/backend`.
