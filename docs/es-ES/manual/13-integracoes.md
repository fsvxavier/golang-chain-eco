# 13. Integraciones

[English](../../en-US/manual/13-integracoes.md) | [Português](../../pt-BR/manual/13-integracoes.md) | **Español**

Este capítulo muestra cómo usar las integraciones de `integrations/` y `providers/`. La **matriz de cobertura y el nivel de verificación** de cada una están en [INTEGRATIONS.md](../INTEGRATIONS.md); la lista de los 504 proveedores de LangChain, en [PROVIDERS.md](../PROVIDERS.md).

## 13.1 Nivel de verificación (léelo antes de depender de una)

| Nivel | Qué significa | Ejemplos |
|---|---|---|
| **Servicio real** | Probado contra el servicio en Docker | PostgreSQL+pgvector, Redis Stack, MongoDB 7, DynamoDB Local, Neo4j 5, ClickHouse, Oracle Free 23ai, SpiceDB, Jenkins, k3s |
| **Contrato (`httptest`)** | Escrito a partir de la documentación pública; **puede divergir del servicio real** | Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, Typesense, Upstash, Astra, watsonx, Replicate, Azure AI Search, Vectara, Permit, Salesforce, Kendra/Knowledge Bases, SMS, proveedores de LLM |

Para un servicio de nivel "contrato", haz una prueba de humo contra tu entorno antes de ir a producción.

## 13.2 Puntos de extensión

Un servicio sin integración entra por uno de estos caminos, cada uno con un contrato pequeño:

| Quiero agregar... | Implementa |
|---|---|
| Una acción para el agent | `tools.New(...)` (capítulo 5) |
| Miles de herramientas existentes | Un servidor MCP + `mcp.LoadTools` |
| Una base de datos vectorial | `vectorstore.Backend` (`Upsert`, `Query`, `Delete`) + `vectorstore.New(emb, backend)` |
| Persistencia de threads | `checkpoint.Saver` (4 métodos) |
| Memoria a largo plazo | `store.Store` |
| Una fuente de documentos | `documents.Loader` / `retrievers.Retriever` |
| Un sandbox para Deep Agents | `backends.Remote{Run: ...}` |
| Un destino de tracing | `callbacks.Handler` |
| Un LLM compatible con OpenAI | `openai.Chat{BaseURL, APIKey}` |

Valida los backends nuevos de checkpoint, store y vector con la suite `internal/conformance`.

## 13.3 Búsqueda en la web (`integrations/search`)

Cada servicio implementa `search.Searcher` (`Search(ctx, query, max) ([]Result, error)`):

`Tavily`, `Exa`, `Brave`, `Serper`, `SerpAPI`, `SearXNG`, `DuckDuckGo` (HTML, frágil), `Wikipedia`, `ArXiv`, `HackerNews`, `ScraperAPI`, `ScrapingBee`, `Brightdata`, `Diffbot`, `YouCom`, `Linkup`, `PerplexitySearch`, `SearchAPI`, `JinaSearch`, `StackOverflow`, `Reddit`, `OpenAlex`, `Apify`, además de Zotero, YouTube, Perigon y Oxylabs.

```go
searcher := search.NewTavily("") // "" lee TAVILY_API_KEY
web := search.Tool("web_search", "Busca en la web.", searcher, 5) // se convierte en tool
retr := search.Retriever(searcher, 5)                             // o retriever
```

También: `Firecrawl`, `JinaReader` y `search.Fetcher`, que descarga páginas con **protección contra SSRF** (rechaza loopback y redes privadas, incluso tras resolver el DNS):

```go
f := &search.Fetcher{MaxBytes: 500_000} // AllowPrivate: true desactiva la protección (solo en pruebas)
html, err := f.Get(ctx, "https://ejemplo.com")
```

Los loaders de web usan el `Fetcher`: `loaders.Web(f, urls...)`, `loaders.Sitemap(...)`, `loaders.JSON(...)`, `loaders.Unstructured{...}`.

## 13.4 Reranking y BM25

```go
rr, _ := chatmodels.NewReranker("cohere:rerank-v3.5")
better := retrieval.WithRerank(retriever, rr)   // reordena los resultados
kw := retrieval.NewBM25(docs)                   // búsqueda por palabra clave en Go puro

hybrid := retrievers.Ensemble([]retrievers.Retriever{retriever, kw}, []float64{0.6, 0.4})
```

## 13.5 Permisos en RAG: devolver solo lo que el usuario puede ver

Los filtros **fallan en modo cerrado**: sin ID, o con la comprobación fallida, el documento no se devuelve.

```go
// SpiceDB
c := &spicedb.Client{URL: "http://localhost:8443", Token: os.Getenv("SPICEDB_KEY")}
safe := spicedb.FilterRetriever(retr, c, spicedb.Subject{Type: "user", ID: "alice"},
	"document", "view", func(d documents.Document) string { return d.Metadata["id"].(string) })

// Permit.io
p := &permit.Client{} // lee PERMIT_API_KEY
safe2 := permit.FilterRetriever(retr, p, "alice", "read", "document", idOf)
```

Ejemplo sin conexión: `examples/integrations/permission_rag`.

## 13.6 Graph RAG

Sigue enlaces de metadatos a partir de los mejores resultados de similitud:

```go
gr := &graphretriever.Retriever{
	Store:  vs,   // cualquier vectorstores.VectorStore
	K:      6,    // total de documentos devueltos
	StartK: 2,    // aciertos iniciales por similitud
	Depth:  2,    // saltos
	Edges:  []graphretriever.Edge{{From: "links", To: "id"}},
}
docs, err := gr.Invoke(ctx, "pregunta")
```

Ejemplo: `examples/integrations/graph_rag` (compara similitud pura con un recorrido de 2 saltos).

## 13.7 Bases de datos y almacenamiento

Todos siguen el mismo contrato de los capítulos 6 y 8.

```go
// Redis (saver + store; también Vector con Redis Stack)
rc := redis.New("localhost:6379")
app, _ := g.Compile(graph.WithCheckpointer(redis.NewSaver(rc)), graph.WithStore(redis.NewStore(rc)))

// DynamoDB (tablas creadas bajo demanda), S3 y MongoDB siguen el mismo patrón:
// dynamodb.NewSaver(ctx, client, "tabla")   s3.NewSaver(client, "bucket", "prefijo")   mongo.NewSaver(client)
```

Detalles por backend:

- **Redis**: cliente RESP2 propio (AUTH, SELECT).
- **MongoDB**: protocolo propio (OP_MSG + BSON), TLS y SCRAM-SHA-256; **sin `mongodb+srv://`** (pasa `host:puerto`).
- **DynamoDB**: JSON API + SigV4; ítem ≤ 400 KB; el `Search` del store hace un Scan.
- **S3**: SigV4; sirve para AWS, MinIO, Tigris, R2 y otros compatibles.
- **SQL (SQLite/PostgreSQL)**: `sqlsaver` y `sqlstore`; tú traes el driver.

### Bases de datos de grafos (KAG)

Para el KAG ([capítulo 20](20-rag-cag-kag.md)), `integrations/graphstore` guarda el grafo de conocimiento en **Neo4j** (Query API HTTP) o en **Memgraph** (Bolt). Ambos implementan
`kag.GraphStore` y pasan la misma suite de conformidad del grafo en memoria; ambos validan la etiqueta y el tipo de relación antes de armar la consulta Cypher.
Se probaron contra Neo4j 5.26 y Memgraph reales en Docker (`NEO4J_URL`, `MEMGRAPH_ADDR`). Detalles en [KNOWLEDGE.md](../KNOWLEDGE.md).

## 13.8 Acciones en sistemas externos (tools)

> **Atención:** estas tools actúan sobre sistemas reales. Da el alcance más pequeño posible, restringe los destinos y exige aprobación humana (`HumanInTheLoop`).

```go
// Jenkins: jenkins_trigger_build, jenkins_last_build y jenkins_console
j := &jenkins.Client{URL: "https://ci.ejemplo.com", User: "bot", Token: os.Getenv("JENKINS_TOKEN")}
jtools := j.Tools()

// Salesforce: SOQL de solo lectura
sf := &salesforce.Client{InstanceURL: "https://org.my.salesforce.com", AccessToken: token}
soql := sf.Tool(50) // máximo de registros por consulta

// SMS (Plivo/Telnyx) con lista de destinos permitidos
sms := messaging.Tool("send_sms", &messaging.Telnyx{ /* credenciales */ Allow: func(to string) bool { return to == "+5511999990000" } })
```

Completa los campos de credencial de `messaging.Plivo`/`messaging.Telnyx` según el tipo en el código. Combínalo con aprobación:

```go
middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_sms": {}}}
```

AWS (`integrations/aws`): Knowledge Bases de Bedrock, Kendra y una tool Lambda, todos firmados con SigV4 sin SDK. Vectara: `integrations/vectara` (retriever).

## 13.9 Sandbox en Kubernetes para Deep Agents

```go
kc := &kubernetes.Client{Server: "https://host:6443", Token: saToken, CA: caPEM}
sb, err := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
if err != nil {
	log.Fatal(err)
}
defer sb.Close(ctx) // elimina el pod

agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})
```

`sb.Run` ejecuta comandos en el pod; `sb.Backend()` devuelve un `backends.Remote` (archivos + shell en el pod). `kubernetes.Client` también habla con la API (pods, logs, exec por WebSocket). Prefiere una service account con permisos mínimos, solo en el namespace del sandbox, y una imagen sin secretos.

## 13.10 Frontends: AG-UI

Mira el [capítulo 11, sección 11.5](11-mcp-remote-servidor.md#115-protocolo-ag-ui).

## 13.11 Cola larga (lo que no está incluido)

Guardrails y gobernanza de terceros, carteras y pagos, memorias de startups, APIs de nicho y sandboxes gestionados (E2B, Daytona, Modal, Runloop). El motivo de cada uno está en [PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md). Para cualquiera de ellos, usa los puntos de extensión de la sección 13.2.

---

Anterior: [12. Observabilidad](12-observabilidade.md) · Siguiente: [14. Producción, seguridad y troubleshooting](14-producao-seguranca-troubleshooting.md) · [Índice del manual](README.es-ES.md)
