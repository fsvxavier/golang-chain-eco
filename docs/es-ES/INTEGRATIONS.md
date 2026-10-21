# Integraciones

[English](../en-US/INTEGRATIONS.md) | [Português](../pt-BR/INTEGRATIONS.md) | **Español**

La lista oficial de LangChain tiene 504 proveedores únicos ([all providers](https://docs.langchain.com/oss/python/integrations/providers/all_providers)).
La mayoría es cola larga de terceros (guardrails, memorias, wallets, APIs de nicho). Este proyecto implementa los proveedores centrales por
**protocolo** y cubre la cola larga con los puntos de extensión. La situación de cada uno está en [PROVIDERS.md](PROVIDERS.md) (matriz) y
[PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md) (qué falta y por qué).

**Cómo se verificó.** Bases de datos y servicios de infraestructura: probados contra el servicio real en Docker (véase
[Pruebas contra servicios reales](#pruebas-contra-servicios-reales)). Servicios de API de pago o SaaS (watsonx, Replicate, Azure AI Search, Vectara,
Permit, Salesforce, AWS Kendra/Knowledge Bases/Lambda, SMS Plivo/Telnyx, búsquedas de pago): pruebas de contrato con `httptest`, escritas a partir de la
documentación pública; pueden divergir del servicio real. Ninguna prueba de la suite estándar llama a la red: las de servicio real se omiten solas.

Véase también el uso en el [README](../../README.es-ES.md).
Manuales del router con varios gateways y de clientes MCP (VS Code, Claude Code, Kilo Code, Copilot CLI, Opencode, Cursor): [docs/integrations](../integrations/es-ES/README.md).

## Chat models y embeddings (`chatmodels`, `providers/*`)

| Proveedor | Paquete / prefijo en `chatmodels.Init` | Chat | Tools | Stream | Embeddings |
|---|---|---|---|---|---|
| OpenAI | `openai` | sí | sí | SSE | sí |
| Azure OpenAI | `azure_openai` (`openai.Azure`) | sí | sí | SSE | sí |
| Anthropic | `anthropic` | sí | sí | SSE | - |
| Google Gemini | `google_genai` / `google` | sí | sí | SSE | sí |
| Google Vertex AI | `google_vertexai` (token OAuth en `GOOGLE_OAUTH_ACCESS_TOKEN`) | sí | sí | SSE | - |
| Cohere | `cohere` | sí | sí | SSE | sí + rerank |
| AWS Bedrock (Converse) | `bedrock` | sí | sí | respuesta única* | - |
| Ollama | `ollama` | sí | sí | NDJSON | sí |
| IBM watsonx.ai | `ibm` / `watsonx` (token IAM con caché) | sí | sí | SSE | sí |
| Replicate | `replicate` | sí (texto) | no | respuesta única | - |

\* El stream binario de Bedrock (`vnd.amazon.eventstream`) no se decodifica; `Stream` devuelve la respuesta completa de una vez.
La firma AWS SigV4 está implementada sin SDK y validada con el vector de prueba oficial de AWS.

**Compatibles con OpenAI** (mismo cliente, `<PROVEEDOR>_API_KEY` y `<PROVEEDOR>_BASE_URL`): `groq`, `together`, `openrouter`,
`deepseek`, `xai`, `mistralai`, `fireworks`, `perplexity`, `cerebras`, `nvidia`, `sambanova`, `nebius`, `novita`, `deepinfra`,
`hyperbolic`, `huggingface`, `moonshot`, `zhipuai`, `dashscope` (Alibaba), `upstage`, `writer`, `ai21`, `baseten`, `github`,
`sarvam`, `featherless`, `modelscope`, `aimlapi`, `netmind`, `digitalocean`, `predictionguard`, `baidu`, `naver`, `helicone`, `portkey`, `lambda`,
`friendli`, `telnyx` y los servidores locales `vllm`, `lmstudio`, `llamacpp`, `localai`, `litellm`. Los de endpoint por tenant (`databricks`, `cloudflare`,
`snowflake`, `runpod`, `truefoundry`, `kserve`, `rayserve`) y 23 gateways de terceros (Doubleword, Hubris, FuturMix...) no tienen URL predeterminada: define
`$<PROVEEDOR>_BASE_URL`, si no `Init` devuelve un error claro. Para embeddings: `voyageai`, `jina`, `nomic` y los
anteriores que exponen `/embeddings`. Cualquier otro servicio compatible se conecta con `openai.Chat{BaseURL: ..., APIKey: ...}`
(cubre OCI, Azure AI y proxies similares).

Rerank: `chatmodels.NewReranker("cohere:rerank-v3.5" | "jina:jina-reranker-v2-base-multilingual" | "voyageai:rerank-2")`.

## Vector stores (`integrations/vectorstore`)

Todos implementan `vectorstores.VectorStore` (búsqueda por similitud con score, MMR, filtro, delete) mediante la interfaz `Backend`:

| Base de datos | Tipo | Observación |
|---|---|---|
| Qdrant | `vectorstore.Qdrant` | REST; los IDs se vuelven UUID determinístico |
| Chroma | `vectorstore.Chroma` | API v2, colección coseno |
| Pinecone | `vectorstore.Pinecone` | índice creado previamente; namespace |
| Elasticsearch | `vectorstore.Elasticsearch` | `dense_vector` + kNN; API key o basic auth |
| Weaviate | `vectorstore.Weaviate` | REST + GraphQL, vectores propios |
| Milvus / Zilliz | `vectorstore.Milvus` | REST v2 |
| PostgreSQL + pgvector | `vectorstore.PGVector` | `database/sql` (aporta el driver); nombre de tabla validado; probado contra Postgres 16 real |
| Redis Stack (RediSearch) | `redis.Vector` | índice FLAT coseno vía `FT.SEARCH` KNN; cliente RESP propio |
| MongoDB / Atlas | `mongo.Vector` | con `Index`: `$vectorSearch` (Atlas, índice creado en Atlas); sin `Index`: escaneo + coseno en el cliente |
| Cassandra / Astra DB | `vectorstore.Astra` | Data API HTTP (Astra, HCD); no se usa CQL nativo |
| Typesense | `vectorstore.Typesense` | REST, KNN vía `vector_query`; solo `httptest` (el binario de Typesense falla en este entorno) |
| Upstash Vector | `vectorstore.Upstash` | REST; índice creado en la consola; solo `httptest` |
| Neo4j | `vectorstore.Neo4j` | Query API HTTP (5.19+), índice vectorial nativo; probado contra Neo4j 5 real |
| SurrealDB | `vectorstore.SurrealDB` | HTTP `/sql`, búsqueda exacta por coseno; probado contra SurrealDB real |
| MariaDB 11.7+ | `vectorstore.MariaDB` | `database/sql` + `VECTOR INDEX`; ids ASCII ≤ 64 bytes; probado contra MariaDB 11.8 real |
| FalkorDB | `redis.FalkorDB` | grafo sobre Redis, índice vectorial nativo; probado contra FalkorDB real |
| ClickHouse / chDB | `vectorstore.ClickHouse` | HTTP, `cosineDistance` exacto; probado contra ClickHouse 24.8 |
| CrateDB | `vectorstore.CrateDB` | HTTP `/_sql`, `knn_match` (euclidiano); probado contra CrateDB 5.9 |
| CockroachDB | `vectorstore.CockroachDB` | `database/sql`, tipo `VECTOR` nativo, `<=>` exacto; probado contra CockroachDB |
| Oracle AI Database 23ai | `vectorstore.Oracle` | `database/sql` (go-ora), `VECTOR_DISTANCE`; probado contra Oracle Free 23ai; hits sin embedding |
| Apache Solr / Opensolr | `vectorstore.Solr` | `DenseVectorField` + `{!knn}`; probado contra Solr 9.7 |
| ArcadeDB | `vectorstore.ArcadeDB` | HTTP, `vectorCosineSimilarity` exacto; probado contra ArcadeDB |
| Infinispan | `vectorstore.Infinispan` | REST + Digest + Protobuf `@Vector`, `<->`; probado contra Infinispan 15; hits sin embedding |
| Couchbase | `vectorstore.Couchbase` | SQL++ con aritmética de arrays (coseno exacto, sin índice vectorial); probado contra Couchbase Community |
| Memgraph | `memgraph.Vector` | cliente Bolt propio + `vector_search`; probado contra Memgraph 3 |
| Azure AI Search | `vectorstore.AzureAISearch` | REST, coseno HNSW, índice creado en el primer `Upsert`; solo prueba de contrato (`httptest`) |
| FAISS (equivalente) | `vectorstore.Flat` | índice exacto en Go puro, `Save`/`LoadFlat`; no es un binding de FAISS |
| FAISS (lectura) | `vectorstore.LoadFAISSFlat` | lee `IndexFlatL2` (`IxF2`), `IndexFlatIP` (`IxFI`) e `IndexIDMap` sobre uno de ellos (`IxMp`); solo lectura |
| Annoy (equivalente) | `vectorstore.Annoy` | bosque de árboles de proyección aleatoria, Go puro; recall medido en pruebas |
| Annoy (lectura) | `vectorstore.LoadAnnoyFile` | lee el `.ann` nativo (`angular` y `euclidean`); búsqueda exacta por fuerza bruta; solo lectura |
| HNSW (índice de grafo) | `vectorstore.NewHNSW()` | grafo jerárquico en Go puro, coseno; `Save`/`LoadHNSW`; los deletes son tombstones; recall medido en pruebas |

Matriz completa de los 504 proveedores de LangChain (qué existe y qué no): [PROVIDERS.md](PROVIDERS.md).

Para una base de datos nueva, implementa `Upsert`, `Query` y `Delete` (3 métodos) y usa `vectorstore.New(embedder, backend)`.
FAISS y Annoy reales son bibliotecas C++ (requerirían cgo); `Flat`, `Annoy` y `HNSW` cubren el mismo uso en Go puro. Para índices ya listos,
`LoadAnnoyFile` y `LoadFAISSFlat` leen el formato nativo de los archivos (solo los tipos listados arriba).

**Compatibilidad verificada con archivos reales.** `integrations/vectorstore/realfiles_test.go` lee archivos generados por las bibliotecas
oficiales (annoy 1.17.3 y faiss-cpu 1.15.1, en `testdata/real_*`, recreados por `testdata/gen_real.py`) y compara el top-k y los scores con la
búsqueda hecha por las propias bibliotecas. Esa prueba encontró y corrigió un error: FAISS guarda el tamaño de los códigos en número de floats, no en bytes.
Alternativas sin servidor: `core/vectorstores.Memory` (con `Save`/`Load`) e `integrations/retrieval.BM25`.

## Objetos, permisos, CI, sandboxes y protocolos

| Paquete | Qué hace | Prueba real |
|---|---|---|
| `integrations/s3` | cliente S3 (SigV4) + `checkpoint.Saver`, `store.Store` y loader; sirve AWS S3, Tigris, XNS, R2, MinIO... | Zenko CloudServer (valida la firma) |
| `integrations/spicedb` | cliente HTTP de SpiceDB + `FilterRetriever` que solo devuelve documentos que el usuario puede ver | SpiceDB |
| `integrations/jenkins` | disparar build, estado, consola; 3 tools de agent | Jenkins LTS |
| `integrations/kubernetes` | cliente de la API (pods, logs, exec por WebSocket) y `Sandbox`: un pod desechable se vuelve backend de deepagents | k3s |
| `deepagents/backends.Remote` | convierte cualquier "ejecutar comando" en un backend completo (archivos + shell); base para Docker, SSH y sandboxes en la nube | shell local + k3s |
| `integrations/graphretriever` | retriever de recorrido en un grafo de metadatos (Graph RAG) sobre cualquier vector store | offline |
| `integrations/agui` | sirve un grafo mediante el protocolo AG-UI (SSE) para CopilotKit, assistant-ui y otros | `httptest` |

### Grupo B: contrato de API pública, sin servicio real aquí

Cubiertos solo por pruebas de contrato (`httptest`) escritas a partir de la documentación pública de cada API; pueden divergir del servicio real:
`providers/watsonx` (IBM watsonx.ai: chat, streaming, embeddings, token IAM con caché), `providers/replicate`, `integrations/vectara` (retriever),
`integrations/permit` (retriever filtrado por permisos), `integrations/messaging` (SMS Plivo/Telnyx, con `Allow` por destino),
`integrations/salesforce` (SOQL de solo lectura), `integrations/aws` (Knowledge Bases de Bedrock, Kendra y tool Lambda, SigV4),
`vectorstore.AzureAISearch` y las búsquedas Zotero, YouTube Data API, Perigon y Oxylabs. Los gateways de terceros compatibles con OpenAI (Doubleword,
Hubris, FuturMix...) entran en la tabla de `chatmodels` y exigen `$<PROVEEDOR>_BASE_URL`.

## Búsqueda y web (`integrations/search`)

`Tavily`, `Exa`, `Brave`, `Serper`, `SerpAPI`, `SearXNG`, `DuckDuckGo` (HTML, frágil), `Wikipedia`, `ArXiv`, `HackerNews`,
`ScraperAPI`, `ScrapingBee`, `Brightdata`, `Diffbot` (páginas vía `ScrapeTool`), `YouCom`, `Linkup`, `PerplexitySearch`, `SearchAPI`, `JinaSearch`, `StackOverflow`, `Reddit`, `OpenAlex`, `Apify` (cualquier Actor).
Cada uno implementa `Searcher` y se convierte en tool (`search.Tool`) o retriever (`search.Retriever`). También: `Firecrawl`, `JinaReader` y
`Fetcher` (GET de páginas con **bloqueo de SSRF**: rechaza loopback/redes privadas, incluso tras resolver DNS).

## Grafos de conocimiento (`integrations/graphstore`)

| Base de datos | Tipo | Transporte | Verificación |
|---|---|---|---|
| Neo4j 5.19+ / Aura | `graphstore.Neo4j` | Query API HTTP | probado contra Neo4j 5.26 en Docker (`NEO4J_URL`) |
| Memgraph | `graphstore.Memgraph` | Bolt 4.x (`integrations/memgraph`) | probado contra Memgraph en Docker (`MEMGRAPH_ADDR`) |

Implementan `kag.GraphStore` y pasan la suite `conformance.Graph`, la misma del grafo en memoria. Detalles, seguridad de los identificadores Cypher y diferencias entre los dos en
[KNOWLEDGE.md](KNOWLEDGE.md#integrationsgraphstore-grafo-en-neo4j-y-memgraph).

## Loaders y retrievers

- `core/documents`: texto, CSV, JSONL, HTML, directorio. `integrations/loaders`: `Web`, `Sitemap`, `JSON`, `Unstructured` (PDF, Office, correo, imágenes, vía API o instancia propia).
- `integrations/retrieval`: `BM25` (Go puro) y `WithRerank` (cualquier `Reranker`, p. ej. Cohere/Jina/Voyage).
- No hay parser de PDF nativo (la stdlib no tiene): usa `Unstructured` o convierte antes.

## Persistencia

`langgraph/checkpoint/sqlsaver` y `langgraph/store/sqlstore` para SQLite/PostgreSQL, `checkpoint.Memory`, `store.Memory`.

| Backend | Saver | Store | Notas |
|---|---|---|---|
| Redis | `redis.NewSaver` | `redis.NewStore` | cliente RESP2 propio (AUTH, SELECT) |
| MongoDB / Atlas | `mongo.NewSaver` | `mongo.NewStore` | OP_MSG + BSON propios; TLS y SCRAM-SHA-256; sin `mongodb+srv://` (pasa host:puerto) |
| DynamoDB | `dynamodb.NewSaver` | `dynamodb.NewStore` | JSON API + SigV4; crea tablas bajo demanda; ítem ≤ 400 KB; `Search` hace un Scan |

Para otra base de datos, implementa `checkpoint.Saver` (4 métodos) o `store.Store`.

## Pruebas contra servicios reales

`internal/conformance` trae las suites comunes (Saver, Store, Vector). Las pruebas de integración se omiten solas cuando la variable
no está definida:

```bash
docker run -d -p 6379:6379 redis/redis-stack-server        # REDIS_ADDR=localhost:6379
docker run -d -p 27017:27017 mongo:7                       # MONGO_ADDR=localhost:27017 (MONGO_USER/MONGO_PASS para SCRAM)
docker run -d -p 8000:8000 amazon/dynamodb-local           # DYNAMODB_ENDPOINT=http://localhost:8000
docker run -d -p 8001:8000 zenko/cloudserver               # S3_ENDPOINT=http://localhost:8001 S3_KEY=accessKey1 S3_SECRET=verySecretKey1
docker run -d -e POSTGRES_PASSWORD=pw -p 5432:5432 pgvector/pgvector:pg16
docker run -d -p 7474:7474 -p 7688:7687 -e NEO4J_AUTH=neo4j/lcgtestpass neo4j:5.26   # NEO4J_URL=http://localhost:7474 NEO4J_USER=neo4j NEO4J_PASS=lcgtestpass
docker run -d -p 7687:7687 memgraph/memgraph               # MEMGRAPH_ADDR=localhost:7687
PG_DSN=postgres://postgres:pw@localhost:5432/postgres go test ./internal/pgtest/...   # driver pgx (solo de pruebas)
```

Verificado contra servicios reales: ClickHouse, CrateDB, CockroachDB, Oracle Free 23ai, Solr, ArcadeDB, Infinispan, Couchbase, Memgraph, Zenko CloudServer (S3), SpiceDB, Jenkins, k3s, Redis Stack, MongoDB 7 (con y sin autenticación), DynamoDB Local, PostgreSQL 16 + pgvector
(incluye `sqlsaver`/`sqlstore`), Neo4j 5, SurrealDB y MariaDB 11.8. **No verificado contra el servicio real**: Atlas `$vectorSearch`, Astra DB (solo `httptest`), Qdrant, Chroma,
Pinecone, Elasticsearch, Weaviate, Milvus, Typesense, Upstash, Astra, proveedores de LLM y tracers (solo `httptest`).

## Observabilidad

| Destino | Paquete |
|---|---|
| LangSmith | `tracing/langsmith` |
| Langfuse | `tracing/langfuse` |
| OpenTelemetry (OTLP/HTTP): Jaeger, Tempo, Honeycomb, Datadog, New Relic, Grafana | `tracing/otlp` |
| Log/consola | `core/callbacks` (`Logger`, `StdOut`) |

## Servidor y cliente de agents

`remote/server` sirve grafos compilados mediante la API de threads/runs; `remote.Client` y `remote.Graph` los consumen (y funcionan con el
Agent Server oficial). `deepagents/middleware.AsyncSubagents` usa ambos.

## Fuera del alcance (cola larga)

Guardrails y gobernanza de agents de terceros, carteras/pagos (x402, cripto), memorias de startups, APIs de datos de nicho y los
sandboxes gestionados (E2B, Daytona, Modal, Runloop) no están incluidos; la lista completa y el motivo de cada uno están en
[PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md). Los caminos de extensión son: una `tools.Tool` (`tools.New`), un servidor MCP
(`mcp.LoadTools`, que da acceso a miles de integraciones existentes), un `vectorstore.Backend`, un `checkpoint.Saver`/`store.Store`, un
runner de comandos enchufado en `backends.Remote` (cualquier sandbox se vuelve backend de archivos y shell) o un `callbacks.Handler`.
