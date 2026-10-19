# Integrações

A lista oficial da LangChain tem 504 provedores únicos ([all providers](https://docs.langchain.com/oss/python/integrations/providers/all_providers)).
A maioria é cauda longa de terceiros (guardrails, memórias, wallets, APIs de nicho). Este projeto implementa os provedores centrais por
**protocolo** e cobre a cauda longa pelos pontos de extensão. A situação de cada um está em [PROVIDERS.md](PROVIDERS.md) (matriz) e
[PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md) (o que falta e por quê).

**Como foi verificado.** Bancos e serviços de infraestrutura: testados contra o serviço real em Docker (veja
[Testes contra serviços reais](#testes-contra-serviços-reais)). Serviços de API paga ou SaaS (watsonx, Replicate, Azure AI Search, Vectara,
Permit, Salesforce, AWS Kendra/Knowledge Bases/Lambda, SMS Plivo/Telnyx, buscas pagas): testes de contrato com `httptest`, escritos a partir da
documentação pública; podem divergir do serviço real. Nenhum teste da suíte padrão chama a rede: os de serviço real se pulam sozinhos.

Veja também o uso no [README](../README.md).

## Chat models e embeddings (`chatmodels`, `providers/*`)

| Provedor | Pacote / prefixo em `chatmodels.Init` | Chat | Tools | Stream | Embeddings |
|---|---|---|---|---|---|
| OpenAI | `openai` | sim | sim | SSE | sim |
| Azure OpenAI | `azure_openai` (`openai.Azure`) | sim | sim | SSE | sim |
| Anthropic | `anthropic` | sim | sim | SSE | - |
| Google Gemini | `google_genai` / `google` | sim | sim | SSE | sim |
| Google Vertex AI | `google_vertexai` (token OAuth em `GOOGLE_OAUTH_ACCESS_TOKEN`) | sim | sim | SSE | - |
| Cohere | `cohere` | sim | sim | SSE | sim + rerank |
| AWS Bedrock (Converse) | `bedrock` | sim | sim | resposta única* | - |
| Ollama | `ollama` | sim | sim | NDJSON | sim |
| IBM watsonx.ai | `ibm` / `watsonx` (token IAM com cache) | sim | sim | SSE | sim |
| Replicate | `replicate` | sim (texto) | não | resposta única | - |

\* O stream binário do Bedrock (`vnd.amazon.eventstream`) não é decodificado; `Stream` devolve a resposta completa de uma vez.
A assinatura AWS SigV4 é implementada sem SDK e validada com o vetor de teste oficial da AWS.

**Compatíveis com OpenAI** (mesmo cliente, `<PROVEDOR>_API_KEY` e `<PROVEDOR>_BASE_URL`): `groq`, `together`, `openrouter`,
`deepseek`, `xai`, `mistralai`, `fireworks`, `perplexity`, `cerebras`, `nvidia`, `sambanova`, `nebius`, `novita`, `deepinfra`,
`hyperbolic`, `huggingface`, `moonshot`, `zhipuai`, `dashscope` (Alibaba), `upstage`, `writer`, `ai21`, `baseten`, `github`,
`sarvam`, `featherless`, `modelscope`, `aimlapi`, `netmind`, `digitalocean`, `predictionguard`, `baidu`, `naver`, `helicone`, `portkey`, `lambda`,
`friendli`, `telnyx` e servidores locais `vllm`, `lmstudio`, `llamacpp`, `localai`, `litellm`. Os de endpoint por tenant (`databricks`, `cloudflare`,
`snowflake`, `runpod`, `truefoundry`, `kserve`, `rayserve`) e 23 gateways de terceiros (Doubleword, Hubris, FuturMix...) não têm URL padrão: defina
`$<PROVEDOR>_BASE_URL`, senão `Init` devolve erro claro. Para embeddings: `voyageai`, `jina`, `nomic` e os
anteriores que expõem `/embeddings`. Qualquer outro serviço compatível entra com `openai.Chat{BaseURL: ..., APIKey: ...}`
(cobre OCI, Azure AI e proxies similares).

Rerank: `chatmodels.NewReranker("cohere:rerank-v3.5" | "jina:jina-reranker-v2-base-multilingual" | "voyageai:rerank-2")`.

## Vector stores (`integrations/vectorstore`)

Todos implementam `vectorstores.VectorStore` (busca por similaridade com score, MMR, filtro, delete) via a interface `Backend`:

| Banco | Tipo | Observação |
|---|---|---|
| Qdrant | `vectorstore.Qdrant` | REST; IDs viram UUID determinístico |
| Chroma | `vectorstore.Chroma` | API v2, coleção cosseno |
| Pinecone | `vectorstore.Pinecone` | índice criado previamente; namespace |
| Elasticsearch | `vectorstore.Elasticsearch` | `dense_vector` + kNN; API key ou basic auth |
| Weaviate | `vectorstore.Weaviate` | REST + GraphQL, vetores próprios |
| Milvus / Zilliz | `vectorstore.Milvus` | REST v2 |
| PostgreSQL + pgvector | `vectorstore.PGVector` | `database/sql` (traga o driver); nome de tabela validado; testado contra Postgres 16 real |
| Redis Stack (RediSearch) | `redis.Vector` | índice FLAT cosseno via `FT.SEARCH` KNN; cliente RESP próprio |
| MongoDB / Atlas | `mongo.Vector` | com `Index`: `$vectorSearch` (Atlas, índice criado no Atlas); sem `Index`: varredura + cosseno no cliente |
| Cassandra / Astra DB | `vectorstore.Astra` | Data API HTTP (Astra, HCD); CQL nativo não é usado |
| Typesense | `vectorstore.Typesense` | REST, KNN via `vector_query`; só `httptest` (o binário do Typesense falha neste ambiente) |
| Upstash Vector | `vectorstore.Upstash` | REST; índice criado no console; só `httptest` |
| Neo4j | `vectorstore.Neo4j` | Query API HTTP (5.19+), índice vetorial nativo; testado contra Neo4j 5 real |
| SurrealDB | `vectorstore.SurrealDB` | HTTP `/sql`, busca exata por cosseno; testado contra SurrealDB real |
| MariaDB 11.7+ | `vectorstore.MariaDB` | `database/sql` + `VECTOR INDEX`; ids ASCII ≤ 64 bytes; testado contra MariaDB 11.8 real |
| FalkorDB | `redis.FalkorDB` | grafo sobre Redis, índice vetorial nativo; testado contra FalkorDB real |
| ClickHouse / chDB | `vectorstore.ClickHouse` | HTTP, `cosineDistance` exato; testado contra ClickHouse 24.8 |
| CrateDB | `vectorstore.CrateDB` | HTTP `/_sql`, `knn_match` (euclidiano); testado contra CrateDB 5.9 |
| CockroachDB | `vectorstore.CockroachDB` | `database/sql`, tipo `VECTOR` nativo, `<=>` exato; testado contra CockroachDB |
| Oracle AI Database 23ai | `vectorstore.Oracle` | `database/sql` (go-ora), `VECTOR_DISTANCE`; testado contra Oracle Free 23ai; hits sem embedding |
| Apache Solr / Opensolr | `vectorstore.Solr` | `DenseVectorField` + `{!knn}`; testado contra Solr 9.7 |
| ArcadeDB | `vectorstore.ArcadeDB` | HTTP, `vectorCosineSimilarity` exato; testado contra ArcadeDB |
| Infinispan | `vectorstore.Infinispan` | REST + Digest + Protobuf `@Vector`, `<->`; testado contra Infinispan 15; hits sem embedding |
| Couchbase | `vectorstore.Couchbase` | SQL++ com aritmética de arrays (cosseno exato, sem índice vetorial); testado contra Couchbase Community |
| Memgraph | `memgraph.Vector` | cliente Bolt próprio + `vector_search`; testado contra Memgraph 3 |
| Azure AI Search | `vectorstore.AzureAISearch` | REST, HNSW cosseno, índice criado no primeiro `Upsert`; só teste de contrato (`httptest`) |
| FAISS (equivalente) | `vectorstore.Flat` | índice exato em Go puro, `Save`/`LoadFlat`; não é binding do FAISS |
| FAISS (leitura) | `vectorstore.LoadFAISSFlat` | lê `IndexFlatL2` (`IxF2`), `IndexFlatIP` (`IxFI`) e `IndexIDMap` sobre um deles (`IxMp`); só leitura |
| Annoy (equivalente) | `vectorstore.Annoy` | floresta de árvores de projeção aleatória, Go puro; recall medido em teste |
| Annoy (leitura) | `vectorstore.LoadAnnoyFile` | lê o `.ann` nativo (`angular` e `euclidean`); busca exata por força bruta; só leitura |
| HNSW (índice de grafo) | `vectorstore.NewHNSW()` | grafo hierárquico em Go puro, cosseno; `Save`/`LoadHNSW`; deletes são tombstones; recall medido em teste |

Matriz completa dos 504 provedores da LangChain (o que existe e o que não): [PROVIDERS.md](PROVIDERS.md).

Para um banco novo, implemente `Upsert`, `Query` e `Delete` (3 métodos) e use `vectorstore.New(embedder, backend)`.
FAISS e Annoy reais são bibliotecas C++ (exigiriam cgo); `Flat`, `Annoy` e `HNSW` cobrem o mesmo uso em Go puro. Para índices já prontos,
`LoadAnnoyFile` e `LoadFAISSFlat` leem o formato nativo dos arquivos (só os tipos listados acima).

**Compatibilidade verificada com arquivos reais.** `integrations/vectorstore/realfiles_test.go` lê arquivos gerados pelas bibliotecas
oficiais (annoy 1.17.3 e faiss-cpu 1.15.1, em `testdata/real_*`, recriados por `testdata/gen_real.py`) e compara o top-k e os scores com a
busca feita pelas próprias bibliotecas. Esse teste encontrou e corrigiu um erro: o FAISS grava o tamanho dos códigos em número de floats, não em bytes.
Alternativas sem servidor: `core/vectorstores.Memory` (com `Save`/`Load`) e `integrations/retrieval.BM25`.

## Objetos, permissões, CI, sandboxes e protocolos

| Pacote | O que faz | Teste real |
|---|---|---|
| `integrations/s3` | cliente S3 (SigV4) + `checkpoint.Saver`, `store.Store` e loader; serve AWS S3, Tigris, XNS, R2, MinIO... | Zenko CloudServer (valida a assinatura) |
| `integrations/spicedb` | cliente HTTP do SpiceDB + `FilterRetriever` que só devolve documentos que o usuário pode ver | SpiceDB |
| `integrations/jenkins` | disparar build, status, console; 3 tools de agente | Jenkins LTS |
| `integrations/kubernetes` | cliente da API (pods, logs, exec por WebSocket) e `Sandbox`: um pod descartável vira backend do deepagents | k3s |
| `deepagents/backends.Remote` | transforma qualquer "executar comando" num backend completo (arquivos + shell); base para Docker, SSH e sandboxes em nuvem | shell local + k3s |
| `integrations/graphretriever` | retriever de travessia em grafo de metadados (Graph RAG) sobre qualquer vector store | offline |
| `integrations/agui` | serve um grafo pelo protocolo AG-UI (SSE) para CopilotKit, assistant-ui e outros | `httptest` |

### Grupo B: contrato de API pública, sem serviço real aqui

Cobertos só por testes de contrato (`httptest`) escritos a partir da documentação pública de cada API; podem divergir do serviço real:
`providers/watsonx` (IBM watsonx.ai: chat, streaming, embeddings, token IAM com cache), `providers/replicate`, `integrations/vectara` (retriever),
`integrations/permit` (retriever filtrado por permissão), `integrations/messaging` (SMS Plivo/Telnyx, com `Allow` por destino),
`integrations/salesforce` (SOQL somente leitura), `integrations/aws` (Knowledge Bases do Bedrock, Kendra e tool Lambda, SigV4),
`vectorstore.AzureAISearch` e as buscas Zotero, YouTube Data API, Perigon e Oxylabs. Gateways OpenAI-compatíveis de terceiros (Doubleword,
Hubris, FuturMix...) entram na tabela do `chatmodels` e exigem `$<PROVEDOR>_BASE_URL`.

## Busca e web (`integrations/search`)

`Tavily`, `Exa`, `Brave`, `Serper`, `SerpAPI`, `SearXNG`, `DuckDuckGo` (HTML, frágil), `Wikipedia`, `ArXiv`, `HackerNews`,
`ScraperAPI`, `ScrapingBee`, `Brightdata`, `Diffbot` (páginas via `ScrapeTool`), `YouCom`, `Linkup`, `PerplexitySearch`, `SearchAPI`, `JinaSearch`, `StackOverflow`, `Reddit`, `OpenAlex`, `Apify` (qualquer Actor).
Cada um implementa `Searcher` e vira tool (`search.Tool`) ou retriever (`search.Retriever`). Também: `Firecrawl`, `JinaReader` e
`Fetcher` (GET de páginas com **bloqueio de SSRF**: recusa loopback/rede privada, mesmo após DNS).

## Loaders e retrievers

- `core/documents`: texto, CSV, JSONL, HTML, diretório. `integrations/loaders`: `Web`, `Sitemap`, `JSON`, `Unstructured` (PDF, Office, e-mail, imagens, via API ou instância própria).
- `integrations/retrieval`: `BM25` (puro Go) e `WithRerank` (qualquer `Reranker`, ex.: Cohere/Jina/Voyage).
- Não há parser de PDF nativo (a stdlib não tem): use `Unstructured` ou converta antes.

## Persistência

`langgraph/checkpoint/sqlsaver` e `langgraph/store/sqlstore` para SQLite/PostgreSQL, `checkpoint.Memory`, `store.Memory`.

| Backend | Saver | Store | Notas |
|---|---|---|---|
| Redis | `redis.NewSaver` | `redis.NewStore` | cliente RESP2 próprio (AUTH, SELECT) |
| MongoDB / Atlas | `mongo.NewSaver` | `mongo.NewStore` | OP_MSG + BSON próprios; TLS e SCRAM-SHA-256; sem `mongodb+srv://` (passe host:porta) |
| DynamoDB | `dynamodb.NewSaver` | `dynamodb.NewStore` | JSON API + SigV4; cria tabelas on-demand; item ≤ 400 KB; `Search` faz Scan |

Para outro banco, implemente `checkpoint.Saver` (4 métodos) ou `store.Store`.

## Testes contra serviços reais

`internal/conformance` traz as suítes comuns (Saver, Store, Vector). Os testes de integração pulam sozinhos quando a variável
não está definida:

```bash
docker run -d -p 6379:6379 redis/redis-stack-server        # REDIS_ADDR=localhost:6379
docker run -d -p 27017:27017 mongo:7                       # MONGO_ADDR=localhost:27017 (MONGO_USER/MONGO_PASS para SCRAM)
docker run -d -p 8000:8000 amazon/dynamodb-local           # DYNAMODB_ENDPOINT=http://localhost:8000
docker run -d -p 8001:8000 zenko/cloudserver               # S3_ENDPOINT=http://localhost:8001 S3_KEY=accessKey1 S3_SECRET=verySecretKey1
docker run -d -e POSTGRES_PASSWORD=pw -p 5432:5432 pgvector/pgvector:pg16
PG_DSN=postgres://postgres:pw@localhost:5432/postgres go test ./internal/pgtest/...   # driver pgx (só de teste)
```

Verificado contra serviços reais: ClickHouse, CrateDB, CockroachDB, Oracle Free 23ai, Solr, ArcadeDB, Infinispan, Couchbase, Memgraph, Zenko CloudServer (S3), SpiceDB, Jenkins, k3s, Redis Stack, MongoDB 7 (sem e com autenticação), DynamoDB Local, PostgreSQL 16 + pgvector
(inclui `sqlsaver`/`sqlstore`), Neo4j 5, SurrealDB e MariaDB 11.8. **Não verificado contra o serviço real**: Atlas `$vectorSearch`, Astra DB (só `httptest`), Qdrant, Chroma,
Pinecone, Elasticsearch, Weaviate, Milvus, Typesense, Upstash, Astra, provedores de LLM e tracers (só `httptest`).

## Observabilidade

| Destino | Pacote |
|---|---|
| LangSmith | `tracing/langsmith` |
| Langfuse | `tracing/langfuse` |
| OpenTelemetry (OTLP/HTTP): Jaeger, Tempo, Honeycomb, Datadog, New Relic, Grafana | `tracing/otlp` |
| Log/console | `core/callbacks` (`Logger`, `StdOut`) |

## Servidor e cliente de agentes

`remote/server` serve grafos compilados pela API de threads/runs; `remote.Client` e `remote.Graph` os consomem (e funcionam com o
Agent Server oficial). `deepagents/middleware.AsyncSubagents` usa os dois.

## Fora do escopo (cauda longa)

Guardrails e governança de agentes de terceiros, carteiras/pagamentos (x402, cripto), memórias de startups, APIs de dados de nicho e os
sandboxes gerenciados (E2B, Daytona, Modal, Runloop) não estão incluídos; a lista completa e o motivo de cada um estão em
[PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md). Os caminhos de extensão são: uma `tools.Tool` (`tools.New`), um servidor MCP
(`mcp.LoadTools`, que dá acesso a milhares de integrações existentes), um `vectorstore.Backend`, um `checkpoint.Saver`/`store.Store`, um
runner de comandos plugado em `backends.Remote` (qualquer sandbox vira backend de arquivos e shell) ou um `callbacks.Handler`.
