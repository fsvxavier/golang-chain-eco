# Integrações

O LangChain lista ~490 provedores ([all providers](https://docs.langchain.com/oss/python/integrations/providers/all_providers)).
A maioria é cauda longa de terceiros (guardrails, memórias, wallets, APIs de nicho). Este projeto implementa
os provedores centrais por **protocolo**, e cobre a cauda longa pelos pontos de extensão. Tudo abaixo tem
testes (`httptest`, driver `database/sql` falso ou SQLite real); nenhum teste chama serviços externos.

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

\* O stream binário do Bedrock (`vnd.amazon.eventstream`) não é decodificado; `Stream` devolve a resposta completa de uma vez.
A assinatura AWS SigV4 é implementada sem SDK e validada com o vetor de teste oficial da AWS.

**Compatíveis com OpenAI** (mesmo cliente, `<PROVEDOR>_API_KEY` e `<PROVEDOR>_BASE_URL`): `groq`, `together`, `openrouter`,
`deepseek`, `xai`, `mistralai`, `fireworks`, `perplexity`, `cerebras`, `nvidia`, `sambanova`, `nebius`, `novita`, `deepinfra`,
`hyperbolic`, `huggingface`, `moonshot`, `zhipuai`, `dashscope` (Alibaba), `upstage`, `writer`, `ai21`, `baseten`, `github`,
e servidores locais `vllm`, `lmstudio`, `llamacpp`, `localai`, `litellm`. Para embeddings: `voyageai`, `jina`, `nomic` e os
anteriores que expõem `/embeddings`. Qualquer outro serviço compatível entra com `openai.Chat{BaseURL: ..., APIKey: ...}`
(cobre Databricks serving endpoints, IBM watsonx via gateway, OCI, Azure AI, TrueFoundry, Portkey, Helicone e proxies similares).

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
| PostgreSQL + pgvector | `vectorstore.PGVector` | `database/sql` (traga o driver); nome de tabela validado |

Para um banco novo, implemente `Upsert`, `Query` e `Delete` (3 métodos) e use `vectorstore.New(embedder, backend)`.
Não implementados: Redis (exige RESP), MongoDB Atlas e Cassandra/Astra (exigem drivers próprios), FAISS/Annoy (bibliotecas C).
Alternativas sem servidor: `core/vectorstores.Memory` (com `Save`/`Load`) e `integrations/retrieval.BM25`.

## Busca e web (`integrations/search`)

`Tavily`, `Exa`, `Brave`, `Serper`, `SerpAPI`, `SearXNG`, `DuckDuckGo` (HTML, frágil), `Wikipedia`, `ArXiv`, `HackerNews`.
Cada um implementa `Searcher` e vira tool (`search.Tool`) ou retriever (`search.Retriever`). Também: `Firecrawl`, `JinaReader` e
`Fetcher` (GET de páginas com **bloqueio de SSRF**: recusa loopback/rede privada, mesmo após DNS).

## Loaders e retrievers

- `core/documents`: texto, CSV, JSONL, HTML, diretório. `integrations/loaders`: `Web`, `Sitemap`, `JSON`, `Unstructured` (PDF, Office, e-mail, imagens, via API ou instância própria).
- `integrations/retrieval`: `BM25` (puro Go) e `WithRerank` (qualquer `Reranker`, ex.: Cohere/Jina/Voyage).
- Não há parser de PDF nativo (a stdlib não tem): use `Unstructured` ou converta antes.

## Persistência

`langgraph/checkpoint/sqlsaver` e `langgraph/store/sqlstore` para SQLite/PostgreSQL, `checkpoint.Memory`, `store.Memory`.
Redis/Mongo/DynamoDB: implemente `checkpoint.Saver` (4 métodos) ou `store.Store`.

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

Sandboxes gerenciados (E2B, Daytona, Modal, Runloop), guardrails de terceiros, carteiras/pagamentos, memórias de agentes de
fornecedores, toolkits SaaS (Slack, Jira, Salesforce, ...) e conectores de dados (Snowflake, Databricks SQL, ...) não estão
incluídos. Os caminhos de extensão são: uma `tools.Tool` (`tools.New`), um servidor MCP (`mcp.LoadTools`, que dá acesso a milhares
de integrações existentes), um `Backend` de arquivos/execução (`deepagents/backends`) ou um `callbacks.Handler`.
