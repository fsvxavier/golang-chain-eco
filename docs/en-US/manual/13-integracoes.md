# 13. Integrations

**English** | [Português](../../pt-BR/manual/13-integracoes.md) | [Español](../../es-ES/manual/13-integracoes.md)

This chapter shows how to use the integrations in `integrations/` and `providers/`. The **coverage matrix and verification level** of each one is in [INTEGRATIONS.md](../INTEGRATIONS.md); the list of LangChain's 504 providers is in [PROVIDERS.md](../PROVIDERS.md).

## 13.1 Verification level (read before depending on it)

| Level | What it means | Examples |
|---|---|---|
| **Real service** | Tested against the service in Docker | PostgreSQL+pgvector, Redis Stack, MongoDB 7, DynamoDB Local, Neo4j 5, ClickHouse, Oracle Free 23ai, SpiceDB, Jenkins, k3s |
| **Contract (`httptest`)** | Written from the public documentation; **may diverge from the real service** | Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, Typesense, Upstash, Astra, watsonx, Replicate, Azure AI Search, Vectara, Permit, Salesforce, Kendra/Knowledge Bases, SMS, LLM providers |

For a "contract"-level service, run a smoke test against your own environment before going to production.

## 13.2 Extension points

A service with no integration comes in through one of these paths, each with a small contract:

| I want to add... | Implement |
|---|---|
| An action for the agent | `tools.New(...)` (chapter 5) |
| Thousands of existing tools | An MCP server + `mcp.LoadTools` |
| A vector database | `vectorstore.Backend` (`Upsert`, `Query`, `Delete`) + `vectorstore.New(emb, backend)` |
| Thread persistence | `checkpoint.Saver` (4 methods) |
| Long-term memory | `store.Store` |
| A document source | `documents.Loader` / `retrievers.Retriever` |
| A sandbox for Deep Agents | `backends.Remote{Run: ...}` |
| A tracing destination | `callbacks.Handler` |
| An OpenAI-compatible LLM | `openai.Chat{BaseURL, APIKey}` |

Validate new checkpoint, store and vector backends with the `internal/conformance` suite.

## 13.3 Web search (`integrations/search`)

Each service implements `search.Searcher` (`Search(ctx, query, max) ([]Result, error)`):

`Tavily`, `Exa`, `Brave`, `Serper`, `SerpAPI`, `SearXNG`, `DuckDuckGo` (HTML, fragile), `Wikipedia`, `ArXiv`, `HackerNews`, `ScraperAPI`, `ScrapingBee`, `Brightdata`, `Diffbot`, `YouCom`, `Linkup`, `PerplexitySearch`, `SearchAPI`, `JinaSearch`, `StackOverflow`, `Reddit`, `OpenAlex`, `Apify`, plus Zotero, YouTube, Perigon and Oxylabs.

```go
searcher := search.NewTavily("") // "" reads TAVILY_API_KEY
web := search.Tool("web_search", "Searches the web.", searcher, 5) // becomes a tool
retr := search.Retriever(searcher, 5)                             // or a retriever
```

Also: `Firecrawl`, `JinaReader` and `search.Fetcher`, which downloads pages with **SSRF protection** (refuses loopback and private networks, even after resolving DNS):

```go
f := &search.Fetcher{MaxBytes: 500_000} // AllowPrivate: true disables the protection (tests only)
html, err := f.Get(ctx, "https://example.com")
```

Web loaders use the `Fetcher`: `loaders.Web(f, urls...)`, `loaders.Sitemap(...)`, `loaders.JSON(...)`, `loaders.Unstructured{...}`.

## 13.4 Reranking and BM25

```go
rr, _ := chatmodels.NewReranker("cohere:rerank-v3.5")
better := retrieval.WithRerank(retriever, rr)   // reorders the results
kw := retrieval.NewBM25(docs)                   // keyword search in pure Go

hybrid := retrievers.Ensemble([]retrievers.Retriever{retriever, kw}, []float64{0.6, 0.4})
```

## 13.5 Permissions in RAG: return only what the user can see

The filters **fail closed**: without an ID, or with the check failing, the document is not returned.

```go
// SpiceDB
c := &spicedb.Client{URL: "http://localhost:8443", Token: os.Getenv("SPICEDB_KEY")}
safe := spicedb.FilterRetriever(retr, c, spicedb.Subject{Type: "user", ID: "alice"},
	"document", "view", func(d documents.Document) string { return d.Metadata["id"].(string) })

// Permit.io
p := &permit.Client{} // reads PERMIT_API_KEY
safe2 := permit.FilterRetriever(retr, p, "alice", "read", "document", idOf)
```

Offline example: `examples/integrations/permission_rag`.

## 13.6 Graph RAG

Follows metadata links from the best similarity results:

```go
gr := &graphretriever.Retriever{
	Store:  vs,   // any vectorstores.VectorStore
	K:      6,    // total documents returned
	StartK: 2,    // initial hits by similarity
	Depth:  2,    // hops
	Edges:  []graphretriever.Edge{{From: "links", To: "id"}},
}
docs, err := gr.Invoke(ctx, "question")
```

Example: `examples/integrations/graph_rag` (compares pure similarity with a 2-hop traversal).

## 13.7 Databases and storage

All of them follow the same contract as chapters 6 and 8.

```go
// Redis (saver + store; also Vector with Redis Stack)
rc := redis.New("localhost:6379")
app, _ := g.Compile(graph.WithCheckpointer(redis.NewSaver(rc)), graph.WithStore(redis.NewStore(rc)))

// DynamoDB (tables created on demand), S3 and MongoDB follow the same pattern:
// dynamodb.NewSaver(ctx, client, "table")   s3.NewSaver(client, "bucket", "prefix")   mongo.NewSaver(client)
```

Details per backend:

- **Redis**: own RESP2 client (AUTH, SELECT).
- **MongoDB**: own protocol (OP_MSG + BSON), TLS and SCRAM-SHA-256; **no `mongodb+srv://`** (pass `host:port`).
- **DynamoDB**: JSON API + SigV4; item ≤ 400 KB; the store's `Search` does a Scan.
- **S3**: SigV4; works with AWS, MinIO, Tigris, R2 and other compatible services.
- **SQL (SQLite/PostgreSQL)**: `sqlsaver` and `sqlstore`; you bring the driver.

### Graph databases (KAG)

For KAG ([chapter 20](20-rag-cag-kag.md)), `integrations/graphstore` stores the knowledge graph in **Neo4j** (HTTP Query API) or **Memgraph** (Bolt). Both implement
`kag.GraphStore` and pass the same conformance suite as the in-memory graph; both validate the label and relation type before building the Cypher query.
They were tested against real Neo4j 5.26 and Memgraph in Docker (`NEO4J_URL`, `MEMGRAPH_ADDR`). Details in [KNOWLEDGE.md](../KNOWLEDGE.md).

## 13.8 Actions in external systems (tools)

> **Warning:** these tools act on real systems. Give the narrowest possible scope, restrict destinations and require human approval (`HumanInTheLoop`).

```go
// Jenkins: jenkins_trigger_build, jenkins_last_build and jenkins_console
j := &jenkins.Client{URL: "https://ci.example.com", User: "bot", Token: os.Getenv("JENKINS_TOKEN")}
jtools := j.Tools()

// Salesforce: read-only SOQL
sf := &salesforce.Client{InstanceURL: "https://org.my.salesforce.com", AccessToken: token}
soql := sf.Tool(50) // maximum records per query

// SMS (Plivo/Telnyx) with an allowed-destinations list
sms := messaging.Tool("send_sms", &messaging.Telnyx{ /* credentials */ Allow: func(to string) bool { return to == "+5511999990000" } })
```

Fill in the credential fields of `messaging.Plivo`/`messaging.Telnyx` according to the type in the code. Combine with approval:

```go
middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_sms": {}}}
```

AWS (`integrations/aws`): Bedrock Knowledge Bases, Kendra and a Lambda tool, all signed with SigV4 without an SDK. Vectara: `integrations/vectara` (retriever).

## 13.9 Kubernetes sandbox for Deep Agents

```go
kc := &kubernetes.Client{Server: "https://host:6443", Token: saToken, CA: caPEM}
sb, err := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
if err != nil {
	log.Fatal(err)
}
defer sb.Close(ctx) // removes the pod

agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})
```

`sb.Run` executes commands in the pod; `sb.Backend()` returns a `backends.Remote` (files + shell in the pod). `kubernetes.Client` also talks to the API (pods, logs, exec over WebSocket). Prefer a service account with minimal permissions, only in the sandbox's namespace, and an image without secrets.

## 13.10 Frontends: AG-UI

See [chapter 11, section 11.5](11-mcp-remote-servidor.md#115-ag-ui-protocol).

## 13.11 Long tail (what is not included)

Third-party guardrails and governance, wallets and payments, startup memories, niche APIs and managed sandboxes (E2B, Daytona, Modal, Runloop). The reason for each one is in [PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md). For any of them, use the extension points in section 13.2.

---

Previous: [12. Observability](12-observabilidade.md) · Next: [14. Production, security and troubleshooting](14-producao-seguranca-troubleshooting.md) · [Manual index](README.md)
