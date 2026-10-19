# 13. Integrações

Este capítulo mostra como usar as integrações de `integrations/` e `providers/`. A **matriz de cobertura e o nível de verificação** de cada uma está em [INTEGRATIONS.md](../INTEGRATIONS.md); a lista dos 504 provedores da LangChain, em [PROVIDERS.md](../PROVIDERS.md).

## 13.1 Nível de verificação (leia antes de depender)

| Nível | O que significa | Exemplos |
|---|---|---|
| **Serviço real** | Testado contra o serviço em Docker | PostgreSQL+pgvector, Redis Stack, MongoDB 7, DynamoDB Local, Neo4j 5, ClickHouse, Oracle Free 23ai, SpiceDB, Jenkins, k3s |
| **Contrato (`httptest`)** | Escrito a partir da documentação pública; **pode divergir do serviço real** | Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, Typesense, Upstash, Astra, watsonx, Replicate, Azure AI Search, Vectara, Permit, Salesforce, Kendra/Knowledge Bases, SMS, provedores de LLM |

Para um serviço de nível "contrato", faça um teste de fumaça contra o seu ambiente antes de ir para produção.

## 13.2 Pontos de extensão

Um serviço sem integração entra por um destes caminhos, cada um com um contrato pequeno:

| Quero adicionar... | Implemente |
|---|---|
| Uma ação para o agent | `tools.New(...)` (capítulo 5) |
| Milhares de ferramentas existentes | Um servidor MCP + `mcp.LoadTools` |
| Um banco vetorial | `vectorstore.Backend` (`Upsert`, `Query`, `Delete`) + `vectorstore.New(emb, backend)` |
| Persistência de threads | `checkpoint.Saver` (4 métodos) |
| Memória de longo prazo | `store.Store` |
| Uma fonte de documentos | `documents.Loader` / `retrievers.Retriever` |
| Um sandbox para Deep Agents | `backends.Remote{Run: ...}` |
| Um destino de tracing | `callbacks.Handler` |
| Um LLM compatível com OpenAI | `openai.Chat{BaseURL, APIKey}` |

Valide backends novos de checkpoint, store e vector com a suíte `internal/conformance`.

## 13.3 Busca na web (`integrations/search`)

Cada serviço implementa `search.Searcher` (`Search(ctx, query, max) ([]Result, error)`):

`Tavily`, `Exa`, `Brave`, `Serper`, `SerpAPI`, `SearXNG`, `DuckDuckGo` (HTML, frágil), `Wikipedia`, `ArXiv`, `HackerNews`, `ScraperAPI`, `ScrapingBee`, `Brightdata`, `Diffbot`, `YouCom`, `Linkup`, `PerplexitySearch`, `SearchAPI`, `JinaSearch`, `StackOverflow`, `Reddit`, `OpenAlex`, `Apify`, além de Zotero, YouTube, Perigon e Oxylabs.

```go
searcher := search.NewTavily("") // "" lê TAVILY_API_KEY
web := search.Tool("web_search", "Busca na web.", searcher, 5) // vira tool
retr := search.Retriever(searcher, 5)                         // ou retriever
```

Também: `Firecrawl`, `JinaReader` e `search.Fetcher`, que baixa páginas com **proteção contra SSRF** (recusa loopback e redes privadas, mesmo após resolver o DNS):

```go
f := &search.Fetcher{MaxBytes: 500_000} // AllowPrivate: true desativa a proteção (só em testes)
html, err := f.Get(ctx, "https://exemplo.com")
```

Loaders de web usam o `Fetcher`: `loaders.Web(f, urls...)`, `loaders.Sitemap(...)`, `loaders.JSON(...)`, `loaders.Unstructured{...}`.

## 13.4 Reranking e BM25

```go
rr, _ := chatmodels.NewReranker("cohere:rerank-v3.5")
better := retrieval.WithRerank(retriever, rr)   // reordena os resultados
kw := retrieval.NewBM25(docs)                   // busca por palavra-chave em Go puro

hybrid := retrievers.Ensemble([]retrievers.Retriever{retriever, kw}, []float64{0.6, 0.4})
```

## 13.5 Permissões no RAG: só devolver o que o usuário pode ver

Os filtros **falham fechado**: sem ID ou com a checagem falhando, o documento não é devolvido.

```go
// SpiceDB
c := &spicedb.Client{URL: "http://localhost:8443", Token: os.Getenv("SPICEDB_KEY")}
safe := spicedb.FilterRetriever(retr, c, spicedb.Subject{Type: "user", ID: "alice"},
	"document", "view", func(d documents.Document) string { return d.Metadata["id"].(string) })

// Permit.io
p := &permit.Client{} // lê PERMIT_API_KEY
safe2 := permit.FilterRetriever(retr, p, "alice", "read", "document", idOf)
```

Exemplo offline: `examples/integrations/permission_rag`.

## 13.6 Graph RAG

Segue links de metadados a partir dos melhores resultados de similaridade:

```go
gr := &graphretriever.Retriever{
	Store:  vs,   // qualquer vectorstores.VectorStore
	K:      6,    // total de documentos devolvidos
	StartK: 2,    // acertos iniciais por similaridade
	Depth:  2,    // saltos
	Edges:  []graphretriever.Edge{{From: "links", To: "id"}},
}
docs, err := gr.Invoke(ctx, "pergunta")
```

Exemplo: `examples/integrations/graph_rag` (compara similaridade pura com travessia em 2 saltos).

## 13.7 Bancos e armazenamento

Todos seguem o mesmo contrato dos capítulos 6 e 8.

```go
// Redis (saver + store; também Vector com Redis Stack)
rc := redis.New("localhost:6379")
app, _ := g.Compile(graph.WithCheckpointer(redis.NewSaver(rc)), graph.WithStore(redis.NewStore(rc)))

// DynamoDB (tabelas criadas on-demand), S3 e MongoDB seguem o mesmo padrão:
// dynamodb.NewSaver(ctx, client, "tabela")   s3.NewSaver(client, "bucket", "prefixo")   mongo.NewSaver(client)
```

Detalhes por backend:

- **Redis**: cliente RESP2 próprio (AUTH, SELECT).
- **MongoDB**: protocolo próprio (OP_MSG + BSON), TLS e SCRAM-SHA-256; **sem `mongodb+srv://`** (passe `host:porta`).
- **DynamoDB**: JSON API + SigV4; item ≤ 400 KB; `Search` do store faz Scan.
- **S3**: SigV4; serve AWS, MinIO, Tigris, R2 e outros compatíveis.
- **SQL (SQLite/PostgreSQL)**: `sqlsaver` e `sqlstore`; você traz o driver.

### Bancos de grafo (KAG)

Para o KAG ([capítulo 20](20-rag-cag-kag.md)), `integrations/graphstore` guarda o grafo de conhecimento em **Neo4j** (Query API HTTP) ou **Memgraph** (Bolt). Os dois implementam
`kag.GraphStore` e passam a mesma suíte de conformidade do grafo em memória; ambos validam rótulo e tipo de relação antes de montar a consulta Cypher.
Foram testados contra Neo4j 5.26 e Memgraph reais em Docker (`NEO4J_URL`, `MEMGRAPH_ADDR`). Detalhes em [KNOWLEDGE.md](../KNOWLEDGE.md).

## 13.8 Ações em sistemas externos (tools)

> **Atenção:** estas tools agem em sistemas reais. Dê o menor escopo possível, restrinja destinos e exija aprovação humana (`HumanInTheLoop`).

```go
// Jenkins: jenkins_trigger_build, jenkins_last_build e jenkins_console
j := &jenkins.Client{URL: "https://ci.exemplo.com", User: "bot", Token: os.Getenv("JENKINS_TOKEN")}
jtools := j.Tools()

// Salesforce: SOQL somente leitura
sf := &salesforce.Client{InstanceURL: "https://org.my.salesforce.com", AccessToken: token}
soql := sf.Tool(50) // máximo de registros por consulta

// SMS (Plivo/Telnyx) com lista de destinos permitidos
sms := messaging.Tool("send_sms", &messaging.Telnyx{ /* credenciais */ Allow: func(to string) bool { return to == "+5511999990000" } })
```

Preencha os campos de credencial de `messaging.Plivo`/`messaging.Telnyx` conforme o tipo no código. Combine com aprovação:

```go
middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_sms": {}}}
```

AWS (`integrations/aws`): Knowledge Bases do Bedrock, Kendra e uma tool Lambda, todos assinados com SigV4 sem SDK. Vectara: `integrations/vectara` (retriever).

## 13.9 Sandbox em Kubernetes para Deep Agents

```go
kc := &kubernetes.Client{Server: "https://host:6443", Token: saToken, CA: caPEM}
sb, err := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
if err != nil {
	log.Fatal(err)
}
defer sb.Close(ctx) // remove o pod

agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})
```

`sb.Run` executa comandos no pod; `sb.Backend()` devolve um `backends.Remote` (arquivos + shell no pod). `kubernetes.Client` também fala com a API (pods, logs, exec por WebSocket). Prefira uma service account com permissões mínimas, só no namespace do sandbox, e uma imagem sem segredos.

## 13.10 Frontends: AG-UI

Veja o [capítulo 11, seção 11.5](11-mcp-remote-servidor.md#115-protocolo-ag-ui).

## 13.11 Cauda longa (o que não está incluído)

Guardrails e governança de terceiros, carteiras e pagamentos, memórias de startups, APIs de nicho e sandboxes gerenciados (E2B, Daytona, Modal, Runloop). O motivo de cada um está em [PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md). Para qualquer um deles, use os pontos de extensão da seção 13.2.

---

Anterior: [12. Observabilidade](12-observabilidade.md) · Próximo: [14. Produção, segurança e troubleshooting](14-producao-seguranca-troubleshooting.md) · [Índice do manual](README.md)
