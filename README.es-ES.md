# golang-chain-eco

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Port en Go del ecosistema LangChain: **langchain-core**, **LangGraph**, el agent con middleware (`create_agent`), **Deep Agents**, cliente y servidor **MCP**, **RemoteGraph**, tracing con **LangSmith**, la memoria a largo plazo de **LangMem**, el **Agent Protocol**, **ciam** (contexto para asistentes) y **Open SWE** (una fábrica de software con agents).

- El código de la biblioteca usa **solo la stdlib**: ningún paquete de producción importa una dependencia de terceros. El `go.mod` lista drivers SQL (`pgx`, `mysql`, `go-ora`, `modernc.org/sqlite`), pero solo los usan las pruebas en `internal/pgtest` y `internal/sqlitetest`; quien usa la biblioteca trae el driver que prefiera.
- Tipado con generics: `Runnable[I, O]`, `Structured[T]`, `tools.New[Args]`.
- Concurrencia nativa: los nodos de un grafo corren en goroutines, y el streaming usa `iter.Seq2`.
- Todo ejemplo corre sin conexión con un modelo falso; basta definir `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` para usar un proveedor real.

> **Manual completo de uso:** [docs/es-ES/manual/](docs/es-ES/manual/README.es-ES.md) — 19 capítulos, desde la instalación hasta producción.

## Índice

- [Manual de uso](#manual-de-uso)
- [Instalación](#instalación)
- [Mapa de paquetes](#mapa-de-paquetes)
- [Inicio rápido](#inicio-rápido)
- [Guía de uso](#guía-de-uso)
  - [Modelos y proveedores](#1-modelos-y-proveedores)
  - [Cadenas (prompt, modelo, parser)](#2-cadenas-prompt-modelo-parser)
  - [Salida estructurada](#3-salida-estructurada)
  - [Tools](#4-tools)
  - [RAG: splitters, embeddings, vector store](#5-rag-splitters-embeddings-vector-store)
  - [LangGraph: StateGraph](#6-langgraph-stategraph)
  - [Persistencia y human-in-the-loop](#7-persistencia-y-human-in-the-loop)
  - [Streaming](#8-streaming)
  - [Agents con middleware](#9-agents-con-middleware)
  - [Deep Agents](#10-deep-agents)
  - [MCP](#11-mcp-model-context-protocol)
  - [Grafos remotos](#12-grafos-remotos)
  - [Tracing con LangSmith](#13-tracing-con-langsmith)
  - [API funcional (entrypoint + task)](#14-api-funcional-entrypoint--task)
  - [Memoria a largo plazo (LangMem)](#15-memoria-a-largo-plazo-langmem)
  - [Agent Protocol, ciam y Open SWE](#16-agent-protocol-ciam-y-open-swe)
- [Configuración](#configuración)
- [Ejemplos ejecutables](#ejemplos-ejecutables)
- [Pruebas](#pruebas)
- [Equivalencia con las libs de Python](#equivalencia-con-las-libs-de-python)
- [Limitaciones](#limitaciones)

## Manual de uso

Guía detallada, con ejemplos y troubleshooting: [docs/es-ES/manual/README.es-ES.md](docs/es-ES/manual/README.es-ES.md). Todos los documentos de referencia están listados en [docs/es-ES/README.es-ES.md](docs/es-ES/README.es-ES.md).

| # | Capítulo |
|---|---|
| 1 | [Introducción e instalación](docs/es-ES/manual/01-introducao-e-instalacao.md) |
| 2 | [Conceptos fundamentales](docs/es-ES/manual/02-conceitos.md) |
| 3 | [Modelos y proveedores](docs/es-ES/manual/03-modelos-e-provedores.md) |
| 4 | [Prompts, cadenas y parsers](docs/es-ES/manual/04-chains-prompts-parsers.md) |
| 5 | [Tools](docs/es-ES/manual/05-tools.md) |
| 6 | [RAG](docs/es-ES/manual/06-rag.md) |
| 7 | [LangGraph: StateGraph](docs/es-ES/manual/07-langgraph.md) |
| 8 | [Persistencia, human-in-the-loop y streaming](docs/es-ES/manual/08-persistencia-hitl-streaming.md) |
| 9 | [Agents y middleware](docs/es-ES/manual/09-agents-e-middleware.md) |
| 10 | [Deep Agents](docs/es-ES/manual/10-deep-agents.md) |
| 11 | [MCP, servidor y grafos remotos](docs/es-ES/manual/11-mcp-remote-servidor.md) |
| 12 | [Observabilidad](docs/es-ES/manual/12-observabilidade.md) |
| 13 | [Integraciones](docs/es-ES/manual/13-integracoes.md) |
| 14 | [Producción, seguridad y troubleshooting](docs/es-ES/manual/14-producao-seguranca-troubleshooting.md) |
| 15 | [Ejemplos y recetas](docs/es-ES/manual/15-exemplos-e-receitas.md) |
| 20 | [RAG, CAG y KAG](docs/es-ES/manual/20-rag-cag-kag.md) |

## Instalación

```bash
go get github.com/fsvxavier/golang-chain-eco@latest
```

Requiere Go 1.27 o superior (versión declarada en el `go.mod`). Para desarrollar en este repositorio:

```bash
git clone https://github.com/fsvxavier/golang-chain-eco
cd golang-chain-eco
go build ./...
go test -race ./...
```

## Mapa de paquetes

| Paquete | Qué hace | Equivalente en Python |
|---|---|---|
| `core/messages` | Mensajes, tool calls, reducer `AddMessages`, trim/filter/merge | `langchain_core.messages` |
| `core/prompts` | Plantillas de prompt y de chat | `langchain_core.prompts` |
| `core/runnables` | `Runnable[I,O]`, `Pipe`, `Parallel`, `Branch`, retry, fallbacks | LCEL / `Runnable` |
| `core/models` | Interfaz `ChatModel`, `Structured[T]`, caché, rate limiter, `Fake` | `BaseChatModel` |
| `core/outputparsers` | `Str`, `JSON`, `Struct[T]`, `CommaList`, `ToolCalls` | output parsers |
| `core/tools` | `tools.New[Args]`, `NewRaw`, validación por JSON Schema | `@tool` |
| `core/documents`, `textsplitters`, `embeddings`, `vectorstores`, `retrievers` | Pipeline de RAG | idem |
| `knowledge`, `rag`, `cag`, `kag` | `Answerer` común, router, RAG híbrido con indexación incremental, CAG con prefijo cacheable, KAG con grafo de conocimiento y razonamiento multi-hop (véase [docs/es-ES/KNOWLEDGE.md](docs/es-ES/KNOWLEDGE.md)) | RAG / CAG / KAG |
| `core/callbacks` | Eventos, tracer, logger | callbacks |
| `core/chathistory` | Historial de chat (memoria, archivo, store) | `ChatMessageHistory` |
| `chatmodels` | `Init("anthropic:claude-...")` | `init_chat_model` |
| `providers/{openai,anthropic,ollama}` | Clientes HTTP, streaming SSE, tools, embeddings | `langchain-openai` etc. |
| `providers/router` | Chat model con failover / round-robin / ponderado sobre varios proveedores y gateways (`router:<model>`) | — |
| `langgraph/graph` | `StateGraph`, runtime Pregel, `Command`, `Send`, `Interrupt`, durabilidad, estado tipado opcional (`NewTyped[T]`) | `langgraph` |
| `langgraph/channels` | Canales de estado y reducers | `langgraph.channels` |
| `langgraph/checkpoint` | `Saver` y `Memory` (con namespaces de subgrafo), serde JSON | `langgraph.checkpoint` |
| `langgraph/checkpoint/sqlsaver` | Saver SQLite/Postgres mediante `database/sql` | `checkpoint-sqlite/postgres` |
| `langgraph/store` | Memoria a largo plazo entre threads | `BaseStore` |
| `langgraph/prebuilt` | `ToolNode`, `CreateReactAgent` | `langgraph.prebuilt` |
| `langgraph/functional` | `Entrypoint` y `Task` | `@entrypoint` / `@task` |
| `agents`, `agents/middleware` | `agents.Create` + middlewares listos | `create_agent` |
| `deepagents` (+ `backends`, `middleware`) | Agent con planificación, filesystem virtual, subagents | `deepagents` |
| `compress` | Compresión de llamadas: `Summary`, `RuleDecider`, fallback `Guarded`, `HTTPDecider` (servidor Laya), compresor de texto Caveman, directiva Ponytail, codificador TOON para resultados JSON de herramientas y enrutado de shell por RTK; `middleware.Compression` en `agents/middleware`; laya-go compilado solo con la tag `laya_native` (`full_team/native`), y `decider_laya_native` como módulo propio ([docs/es-ES/COMPRESSION.md](docs/es-ES/COMPRESSION.md)) | — |
| `mcp` | Cliente MCP (stdio y HTTP) | `langchain-mcp-adapters` |
| `remote` | Cliente del Agent Server y `RemoteGraph` | `langgraph_sdk` |
| `tracing/{langsmith,langfuse,otlp}` | Envío de runs a LangSmith, Langfuse y OpenTelemetry | tracers |
| `integrations/vectorstore` | 25 backends (los índices locales `Flat`, `Annoy` y `HNSW` incluidos, con lectura de archivos `.ann` y FAISS flat; más Redis, FalkorDB, MongoDB/Atlas y Memgraph en sus propios paquetes = 29): Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, pgvector, Typesense, Upstash, Neo4j, SurrealDB, MariaDB, ClickHouse, CrateDB, CockroachDB, Oracle 23ai, Solr, ArcadeDB, Infinispan, Couchbase, Azure AI Search, Astra | `langchain-community` y socios |
| `integrations/{redis,mongo,dynamodb,s3}` | `checkpoint.Saver` y `store.Store` en Redis, MongoDB, DynamoDB y S3 (AWS, MinIO, Tigris, R2...); Redis, MongoDB/Atlas y FalkorDB también como vector store | checkpointers/stores |
| `integrations/{search,loaders,retrieval}` | ~30 buscadores y scrapers (Tavily, Exa, Brave, You.com, Zotero, YouTube...), loaders, BM25 y rerank | `langchain-community` |
| `integrations/graphstore` | `kag.GraphStore` en Neo4j (HTTP) y Memgraph (Bolt), con tipos de relación validados | grafos de conocimiento |
| `integrations/{graphretriever,spicedb,permit,vectara,aws}` | Graph RAG, retrievers filtrados por permiso (SpiceDB, Permit), Vectara, Bedrock Knowledge Bases, Kendra, tool Lambda | retrievers |
| `integrations/{kubernetes,jenkins,salesforce,messaging,memgraph,agui}` | Sandbox en pod, Jenkins, SOQL de solo lectura, SMS (Plivo/Telnyx), Memgraph (Bolt), protocolo AG-UI | tools / protocolos |
| `providers/{google,cohere,bedrock,watsonx,replicate}` | Gemini/Vertex, Cohere (chat, embeddings, rerank), Bedrock Converse, IBM watsonx.ai, Replicate | socios |
| `agentprotocol` | API abierta del Agent Protocol (agents, threads, runs, store) + streaming por thread (SSE y WebSocket) y cliente | `agent-protocol` |
| `ciam` | Contexto para asistentes: índice incremental, búsqueda híbrida, ADR/PRD/planes, memoria, tools y servidor MCP | `context-ia-manager` |
| `mcp/server` | Servidor MCP genérico para cualquier `[]tools.Tool` (stdio y HTTP) | — |
| `openswe/{prompts,github,sandbox,guards,review,babysit,triggers,slack,linear}` y `openswe` | Fábrica de software: ingeniero con sandbox por thread, revisor de PRs, vigilante de CI, webhooks, cron, automatizaciones, Slack y Linear | `open-swe` |
| `langmem` | Memoria a largo plazo: managers semántico/episódico, tools de memoria, optimización de prompts, resumen incremental, background y middleware | `langmem` |
| `remote/server` | Servidor de grafos compatible con la API de threads/runs | Agent Server |
| `langgraph/store/sqlstore` | Store a largo plazo en SQLite/Postgres | `PostgresStore` |
| `evals` | Evaluación offline: datasets JSONL, evaluadores (exacto, regex, JSON, embedding, LLM-as-judge, trayectoria de tools), informe en texto/JSON | `langsmith.evaluation`, `openevals`, `agentevals` |
| `langgraph/dev` y `cmd/langgraph` | Servidor de desarrollo (Agent Protocol) para tus grafos; `langgraph dev` sirve un agent de demostración | `langgraph dev` |

## Inicio rápido

Un agent con una tool, en menos de 30 líneas:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type WeatherArgs struct {
	City string `json:"city" description:"Nombre de la ciudad"`
}

func main() {
	weather := tools.New("get_weather", "Clima actual de una ciudad.",
		func(ctx context.Context, a WeatherArgs) (any, error) {
			return "Soleado, 24°C en " + a.City, nil
		})

	agent, err := agents.Create(agents.Options{
		Model:        chatmodels.MustInit("anthropic:claude-sonnet-4-5"), // requiere ANTHROPIC_API_KEY
		Tools:        []tools.Tool{weather},
		SystemPrompt: "Eres un asistente de clima.",
	})
	if err != nil {
		log.Fatal(err)
	}

	out, err := agent.Invoke(context.Background(), graph.State{"messages": "¿Qué tiempo hace en Madrid?"})
	if err != nil {
		log.Fatal(err)
	}
	msgs := out["messages"].([]messages.Message)
	fmt.Println(msgs[len(msgs)-1].Text())
}
```

El estado del grafo es un `map[string]any` (`graph.State`). La clave `messages` acepta una string (se convierte en mensaje humano), un `messages.Message` o un `[]messages.Message`.

## Guía de uso

### 1. Modelos y proveedores

```go
import (
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

// Por string "proveedor:modelo" (equivale a init_chat_model).
m, err := chatmodels.Init("openai:gpt-4o-mini", models.WithTemperature(0))
m2 := chatmodels.MustInit("ollama:llama3.1")
m3 := chatmodels.MustInit("groq:llama-3.3-70b-versatile") // compatible con OpenAI, lee GROQ_API_KEY

// Construcción directa, con endpoint propio (ej.: Azure, vLLM, LM Studio).
local := &openai.Chat{Model: "qwen2.5", BaseURL: "http://localhost:8000/v1", APIKey: "x"}

// Llamada simple y streaming.
resp, err := m.Generate(ctx, []messages.Message{messages.System("Sé breve."), messages.Human("Hola")})
for chunk, err := range m.Stream(ctx, msgs) {
	fmt.Print(chunk.Text())
}
```

Proveedores con cliente propio: `openai`, `anthropic`, `ollama`, `google_genai`, `cohere`, `bedrock`, `azure_openai`, `ibm`/`watsonx`, `replicate`. 75 entradas son compatibles con OpenAI (clave en `$<PROVEEDOR>_API_KEY`): `groq`, `together`, `openrouter`, `deepseek`, `xai`, `mistralai`, `fireworks`, `sarvam`, `telnyx`...; las de endpoint por tenant (`databricks`, `cloudflare`, `snowflake`, `runpod` y gateways de terceros) requieren `$<PROVEEDOR>_BASE_URL`. Matriz completa de los 504 proveedores de LangChain: [docs/es-ES/PROVIDERS.md](docs/es-ES/PROVIDERS.md). Sin prefijo, `gpt-*`/`o1`/`o3`/`o4` van a OpenAI y `claude*` a Anthropic.

Utilidades que envuelven cualquier modelo:

```go
cached := models.WithCache(m, &models.MemoryCache{})            // respuestas repetidas
bound := models.BindTools(m, weather)                           // tools fijas
limited := models.NewRateLimiter(5, 10)                         // token bucket (req/s, burst)
embedder, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small")
```

### 2. Cadenas (prompt, modelo, parser)

`Pipe3` es el equivalente tipado de `prompt | model | parser`:

```go
prompt := prompts.Chat(
	[2]string{"system", "Responde en una frase."},
	[2]string{"human", "Explica {topic}."},
)
chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
	prompt, models.AsRunnable(m), outputparsers.Str{},
)
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})

// streaming de tokens de la cadena
for tok, err := range runnables.Stream[prompts.Values, string](ctx, chain, prompts.Values{"topic": "channels"}) {
	fmt.Print(tok)
}
```

Combinadores útiles en `core/runnables`: `Parallel`, `Assign`, `Pick`, `Branch`, `WithRetry`, `WithFallbacks`, `Map`, `Batch`, `Lambda`.

```go
robust := runnables.WithRetry(chain, runnables.RetryOptions{MaxAttempts: 4})
safe := runnables.WithFallbacks(robust, backupChain)
```

### 3. Salida estructurada

```go
type Person struct {
	Name string `json:"name" description:"Nombre completo"`
	Age  int    `json:"age"`
}

extract := models.Structured[Person](m, models.MethodTool) // o MethodJSONSchema
p, err := extract.Invoke(ctx, []messages.Message{messages.Human("Ana Souza tiene 31 años.")})
// p == Person{Name: "Ana Souza", Age: 31}
```

### 4. Tools

El schema de argumentos se genera por reflexión a partir de la struct (tags `json`, `description` y `jsonschema:"enum=a|b,required"`):

```go
type SearchArgs struct {
	Query string `json:"query" description:"Término de búsqueda"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
}

search := tools.New("search", "Busca en la base.", func(ctx context.Context, a SearchArgs) (any, error) {
	return doSearch(a.Query, a.Limit)
})

// Los errores del tipo tools.Error se muestran al modelo en lugar de abortar la ejecución:
return nil, tools.Errorf("ciudad %q no encontrada", a.City)

// Tool terminal (termina el loop del agent con el resultado):
tools.New("finish", "Termina.", fn, tools.WithReturnDirect())
```

Una tool también puede devolver `graph.Command{Update: ...}` para cambiar el estado del grafo, o `tools.ContentAndArtifact{Content, Artifact}` para adjuntar datos que no llegan al modelo. Dentro de una tool, `prebuilt.StateOf(ctx)` devuelve el estado actual y `tools.CallID(ctx)` el ID de la llamada.

### 5. RAG: splitters, embeddings, vector store

```go
text := loadBigText()
chunks := textsplitters.NewRecursive(800, 100).SplitText(text)

docs := make([]documents.Document, len(chunks))
for i, c := range chunks {
	docs[i] = documents.Document{PageContent: c, Metadata: map[string]any{"source": "manual.txt"}}
}

emb, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small") // o embeddings.Fake{Dim: 64} en pruebas
store, err := vectorstores.FromDocuments(ctx, emb, docs)

retriever := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4, Type: retrievers.MMR})
hits, err := retriever.Invoke(ctx, "¿cómo cancelo mi pedido?")

// Como tool para un agent (RAG agéntico):
ragTool := retrievers.AsTool(retriever, "search_docs", "Busca en la documentación.")
```

`textsplitters.ForLanguage("go", 1000, 100)` divide código respetando funciones y tipos; `MarkdownHeaders` divide por encabezados. `vectorstores.Memory` persiste en archivo (`Save`/`Load`).

### 6. LangGraph: StateGraph

El estado se compone de **canales**. Cada clave tiene un reducer; sin declararlo, rige "último valor". Cada nodo recibe una copia del estado y devuelve una actualización parcial.

```go
schema := graph.Schema{
	"messages": graph.Messages(), // acumula con el reducer add_messages
	"steps":    graph.Sum(),      // suma enteros
	"log":      graph.Append(),   // concatena listas
	"topic":    channels.LastValue(),
}

g := graph.NewStateGraph(schema).
	AddNode("research", func(ctx context.Context, s graph.State) (any, error) {
		return graph.State{"log": []any{"investigó " + s["topic"].(string)}, "steps": 1}, nil
	}).
	AddNode("write", writeNode).
	AddNode("review", reviewNode).
	AddEdge(graph.Start, "research").
	AddEdge("research", "write").
	// enrutamiento condicional: el router devuelve el nombre del siguiente nodo (o graph.End)
	AddConditionalEdges("write", func(ctx context.Context, s graph.State) (any, error) {
		if s["steps"].(int) > 3 {
			return graph.End, nil
		}
		return "review", nil
	}).
	AddEdge("review", "write")

app, err := g.Compile()
out, err := app.Invoke(ctx, graph.State{"topic": "Go generics"})
fmt.Println(app.Mermaid()) // diagrama
```

Recursos del runtime:

| Recurso | Cómo usarlo |
|---|---|
| Paralelismo | Varios `AddEdge(a, x)`, `AddEdge(a, y)`: `x` e `y` corren en el mismo super-step |
| Espera por varios nodos | `AddJoin([]string{"x","y"}, "merge")` |
| Fan-out (map-reduce) | El router o un nodo devuelve `[]graph.Send{{Node: "worker", Arg: graph.State{...}}}` |
| Control desde el nodo | Devolver `graph.Command{Update: ..., Goto: []string{"otro"}}` |
| Retry por nodo | `AddNode("n", fn, graph.WithRetry(runnables.RetryOptions{MaxAttempts: 3}))` |
| Caché por nodo | `graph.WithCachePolicy(graph.CachePolicy{TTL: time.Minute})` + `Compile(graph.WithCache(graph.NewMemoryCache()))` |
| Nodo diferido | `graph.Deferred()` corre solo cuando no quedan otras tasks pendientes |
| Límite de pasos | `runnables.Config{RecursionLimit: 50}` (por defecto 25), error `*graph.GraphRecursionError` |
| Subgrafos | Invocar otro `*graph.Compiled` dentro de un nodo |
| Memoria entre threads | `Compile(graph.WithStore(store.NewMemory()))` y `graph.StoreFrom(ctx)` en el nodo |

Fan-out con `Send`:

```go
AddConditionalEdges("plan", func(ctx context.Context, s graph.State) (any, error) {
	var sends []graph.Send
	for _, topic := range s["topics"].([]string) {
		sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"topic": topic}})
	}
	return sends, nil
})
```

### 7. Persistencia y human-in-the-loop

Con un checkpointer, cada super-step se guarda por `thread_id`. Esto permite retomar conversaciones, pausar para aprobación humana, inspeccionar y volver atrás en el tiempo.

```go
saver := checkpoint.NewMemory()
app, _ := g.Compile(graph.WithCheckpointer(saver))

ctx := runnables.WithConfig(context.Background(), graph.Thread("usuario-42"))
out, _ := app.Invoke(ctx, graph.State{"messages": "hola"})
out, _ = app.Invoke(ctx, graph.State{"messages": "¿y ahora?"}) // continúa la misma conversación
```

**Pausar y reanudar** con `graph.Interrupt`:

```go
approve := func(ctx context.Context, s graph.State) (any, error) {
	// La primera ejecución se pausa aquí. Al reanudar, Interrupt devuelve el valor del resume.
	answer := graph.Interrupt(ctx, map[string]any{"pregunta": "¿Aprobar el envío?"})
	if answer != "sí" {
		return graph.State{"status": "cancelado"}, nil
	}
	return graph.State{"status": "enviado"}, nil
}

out, _ := app.Invoke(ctx, graph.State{"status": "nuevo"})
for _, i := range graph.Interrupts(out) {
	fmt.Println("pausado en", i.Node, i.Value)
}
out, _ = app.Invoke(ctx, graph.Command{Resume: "sí"}) // reanuda
```

El nodo **se reejecuta desde el inicio** al reanudar; mantén idempotente lo que va antes del `Interrupt`. Pausas estáticas: `Compile(graph.InterruptBefore("tools"))` y `graph.InterruptAfter(...)`; para continuar, `app.Invoke(ctx, nil)`.

**Inspección y viaje en el tiempo**:

```go
snap, _ := app.GetState(ctx)                   // Values, Next, Interrupts, CheckpointID
hist, _ := app.GetStateHistory(ctx, 10)        // más recientes primero
app.UpdateState(ctx, graph.State{"topic": "otro"}, "research") // edita como si fuera el nodo "research"

// Fork desde un checkpoint antiguo:
old := runnables.WithConfig(ctx, runnables.Config{Configurable: map[string]any{"checkpoint_id": hist[3].CheckpointID}})
app.Invoke(old, nil)
```

**Persistencia en base de datos** (SQLite o Postgres, con el driver que prefieras):

```go
import (
	"database/sql"
	_ "modernc.org/sqlite" // o _ "github.com/jackc/pgx/v5/stdlib"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
)

db, _ := sql.Open("sqlite", "checkpoints.db")
saver, err := sqlsaver.New(ctx, db, sqlsaver.SQLite) // o sqlsaver.Postgres
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

Los valores se escriben en JSON. Si tu estado usa tipos propios, regístralos para que sobrevivan al round trip:

```go
checkpoint.Register("miapp.Pedido", Pedido{})
checkpoint.Register("[]miapp.Pedido", []Pedido{})
```

Los enteros vuelven como `int` y los demás números como `float64`. También puedes implementar `checkpoint.Saver` (4 métodos) para otro backend.

### 8. Streaming

```go
for ev, err := range app.StreamEvents(ctx, input, graph.ModeUpdates, graph.ModeMessages, graph.ModeCustom) {
	if err != nil {
		log.Fatal(err)
	}
	switch ev.Mode {
	case graph.ModeUpdates:  // {nodo: actualización} cada vez que un nodo termina
	case graph.ModeValues:   // estado completo en cada paso
	case graph.ModeMessages: // tokens del LLM a medida que llegan (ev.Data es messages.Message)
	case graph.ModeCustom:   // lo que el nodo envió con graph.StreamWriter(ctx)(valor)
	}
}
```

### 9. Agents con middleware

`agents.Create` monta el loop `modelo ⇄ tools` y te permite enchufar comportamiento en puntos definidos: `BeforeAgent`, `BeforeModel`, `WrapModelCall`, `AfterModel`, `WrapToolCall`, `AfterAgent`.

```go
agent, _ := agents.Create(agents.Options{
	Model: m,
	Tools: []tools.Tool{search, sendEmail},
	Middleware: []agents.Middleware{
		middleware.ModelCallLimit{RunLimit: 10},
		middleware.ToolRetry{Options: runnables.RetryOptions{MaxAttempts: 3}},
		middleware.Summarization{Model: m, MaxTokens: 8000, KeepMessages: 12},
		middleware.PII{Kinds: []middleware.PIIKind{middleware.PIIEmail, middleware.PIICreditCard}, Strategy: middleware.PIIRedact},
		middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{"send_email": {}}},
	},
	Compile: []graph.CompileOption{graph.WithCheckpointer(saver)}, // HITL exige checkpointer
	ResponseFormat: &models.ResponseFormat{ /* JSON Schema opcional; resultado en state["structured_response"] */ },
})
```

Middlewares incluidos (`agents/middleware`): `ModelCallLimit`, `ToolCallLimit`, `ModelFallback`, `ModelRetry`, `ToolRetry`, `Summarization`, `ContextEditing`, `TodoList`, `PII`, `HumanInTheLoop`.

**Aprobación humana** de una tool sensible:

```go
out, _ := agent.Invoke(ctx, graph.State{"messages": "Envía el informe a Bob"})
// se pausa antes de send_email; out trae la solicitud en graph.Interrupts(out)

out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
	Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	// o {Type: middleware.Edit, EditedAction: &middleware.ActionRequest{Name: "send_email", Args: ...}}
	// o {Type: middleware.Reject, Message: "no lo envíes"}
}})
```

**Middleware propio**: implementa `Name()` y cualquier interfaz de hook.

```go
type Timing struct{}

func (Timing) Name() string { return "timing" }
func (Timing) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	t := time.Now()
	out, err := next(ctx, req) // req.System, req.Messages, req.Tools y req.Model pueden cambiarse antes
	log.Println("el modelo tardó", time.Since(t))
	return out, err
}
```

Un hook puede desviar el flujo devolviendo `graph.State{"jump_to": agents.JumpEnd}` (o `JumpModel`, `JumpTools`). Los middlewares también pueden declarar tools (`Tools()`) y canales de estado (`StateSchema()`).

Para el caso mínimo sin middleware existe `prebuilt.CreateReactAgent(prebuilt.ReactOptions{Model, Tools, Prompt})`.

### 10. Deep Agents

Agent para tareas largas: lista de tareas (`write_todos`), filesystem virtual (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), delegación a subagents (`task`), resumen automático, memoria y skills.

```go
agent, err := deepagents.Create(deepagents.Options{
	Model:        m,
	SystemPrompt: "Eres un analista de investigación.",
	Tools:        []tools.Tool{webSearch},
	SubAgents: []deepagents.SubAgentSpec{{
		Name:         "critico",
		Description:  "Revisa borradores y señala fallas.",
		SystemPrompt: "Eres un revisor riguroso.",
	}},
	Memory:       []string{"/AGENTS.md"}, // se inyecta en el prompt (si existe en el backend)
	Skills:       []string{"/skills/"},   // cada carpeta con SKILL.md (name/description en el front matter)
	Checkpointer: saver,
})

out, _ := agent.Invoke(ctx, graph.State{"messages": "Escribe un informe sobre baterías de sodio."})
for path, f := range backends.FilesFromState(out) { // archivos creados por el agent
	fmt.Println(path, len(f.Content))
}
```

Siempre existe un subagent `general-purpose`. Cada llamada a la tool `task` ejecuta un agent aislado y devuelve solo la respuesta final, manteniendo pequeño el contexto principal.

**Backends de archivos** (`deepagents/backends`):

| Backend | Dónde viven los archivos |
|---|---|
| `backends.State{}` (predeterminado) | En el estado del grafo, por thread |
| `backends.Store{Store: s, Namespace: ...}` | En un `store.Store`, compartido entre threads |
| `backends.Filesystem{Root: "./work"}` | En disco, confinado a la raíz (bloquea `..` y symlinks que escapan) |
| `backends.LocalShell{...}` | Disco + tool `execute` (**ejecuta comandos en el host, sin sandbox: solo para uso confiable**; `AllowedCommands` restringe los programas) |
| `backends.Composite{Default: ..., Routes: ...}` | Enruta por prefijo, p. ej. `/memories/` al Store |

```go
be := backends.Composite{
	Default: backends.State{},
	Routes:  map[string]backends.Backend{"/memories/": backends.Store{Store: store.NewMemory(), Namespace: []string{"mem"}}},
}
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: be})
```

**Permisos** por operación y ruta (vale la primera regla que coincida; también aplica a los subagents):

```go
deepagents.Options{
	Model: m,
	Backend: backends.Filesystem{Root: "./work"},
	Permissions: []backends.Permission{
		{Paths: []string{"/secrets/**"}, Deny: true},
		{Operations: []string{backends.OpWrite}, Paths: []string{"/docs/**"}, Deny: true}, // docs de solo lectura
	},
}
```

**Otros middlewares de Deep Agents** (`deepagents/middleware`), pasados en `Options.Middleware`:

```go
// Rúbrica: un evaluador revisa la respuesta final y devuelve feedback hasta cumplir los criterios.
dmw.Rubric{Grader: m, Criteria: []string{"Cita una fuente para cada número", "Tiene un resumen de 3 líneas"}, MaxIterations: 3}

// Subagents asíncronos: el agent dispara trabajo largo y lo consulta después.
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
	{Name: "investigador", Description: "Investigaciones largas.", Runner: &dmw.LocalRunner{Agent: researcher}},
	// o Runner: dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "investigador"},
}}
```

### 11. MCP (Model Context Protocol)

```go
import "github.com/fsvxavier/golang-chain-eco/mcp"

// Varios servidores a la vez; las tools reciben el prefijo "<servidor>_".
ts, closeAll, err := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"fs":   {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
	"docs": {URL: "https://mcp.ejemplo.com/mcp", Headers: map[string]string{"Authorization": "Bearer ..."}},
})
if err != nil { log.Fatal(err) }
defer closeAll()

agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

Uso de bajo nivel: `mcp.Connect(ctx, mcp.HTTP(url, headers))` o `mcp.Stdio(...)`, luego `client.ListTools` y `client.CallTool`. Los servidores stdio son procesos que inicias tú con tus permisos: ejecuta solo servidores confiables.

### 12. Grafos remotos

Usa un grafo publicado en un Agent Server (LangGraph Platform) con la misma API de los grafos locales:

```go
c := remote.NewClient("https://mi-servidor.ejemplo.com") // clave por LANGGRAPH_API_KEY o c.APIKey
rg := &remote.Graph{Client: c, AssistantID: "mi-agent"}

ctx := runnables.WithConfig(ctx, graph.Thread("thread-1"))
out, err := rg.Invoke(ctx, graph.State{"messages": []map[string]any{{"role": "user", "content": "hola"}}})
for st, err := range rg.Stream(ctx, graph.Command{Resume: "sí"}) { /* estados parciales */ }
```

`remote.Client` también expone `CreateThread`, `CreateRun`, `GetRun`, `Join`, `CancelRun`, `GetState` y `UpdateState`.

### 13. Tracing con LangSmith

```go
tr := langsmith.New(langsmith.Options{Project: "mi-proyecto"}) // LANGSMITH_API_KEY en el entorno
defer tr.Close()                                               // vacía la cola

ctx := callbacks.WithHandlers(context.Background(), tr)
agent.Invoke(ctx, input) // cadenas, modelos, tools y nodos aparecen como runs anidadas
```

El envío es asíncrono y de mejor esfuerzo: los fallos de red nunca tiran el programa (usa `Options.OnError` para observarlos). Para depuración local, `callbacks.StdOut(os.Stderr)` y `callbacks.Logger(slog.Default())` son handlers listos.

### 14. API funcional (entrypoint + task)

Escribe el flujo como código Go normal; las tasks se memorizan en el checkpoint, así que al reanudar no se repite trabajo ya hecho.

```go
wf := functional.New("informe", saver, func(ctx context.Context, tema string) (string, error) {
	datos, err := functional.Task(ctx, "recolectar", func(ctx context.Context) (string, error) {
		return recolectar(tema) // los efectos secundarios van dentro de tasks
	}).Result()
	if err != nil {
		return "", err
	}
	ok := graph.Interrupt(ctx, "¿Publicar?").(string) // human-in-the-loop
	if ok != "sí" {
		return "descartado", nil
	}
	return publicar(datos)
})

ctx := runnables.WithConfig(ctx, graph.Thread("wf-1"))
r, _ := wf.Invoke(ctx, "baterías")   // r.Interrupt != nil: esperando
r, _ = wf.Resume(ctx, "sí")          // recolectar NO se ejecuta de nuevo
```

### Integraciones adicionales

```go
// Gemini, Cohere, Bedrock, Azure y decenas de proveedores compatibles con OpenAI, con el mismo Init:
m := chatmodels.MustInit("google_genai:gemini-2.0-flash")
m = chatmodels.MustInit("bedrock:anthropic.claude-3-5-sonnet-20241022-v2:0") // AWS_ACCESS_KEY_ID/SECRET/REGION
m = chatmodels.MustInit("perplexity:sonar")                                  // PERPLEXITY_API_KEY
emb, _ := chatmodels.InitEmbeddings("cohere:embed-v4.0")

// Vector store externo (Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, pgvector):
store := vectorstore.New(emb, &vectorstore.Qdrant{URL: "http://localhost:6333", Collection: "docs"})
store.AddDocuments(ctx, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})

// Búsqueda en la web como tool de un agent:
web := search.Tool("web_search", "Busca en la web.", search.NewTavily(""), 5)   // TAVILY_API_KEY

// Reranking y BM25 sin base de datos:
rr, _ := chatmodels.NewReranker("cohere:rerank-v3.5")
better := retrieval.WithRerank(retr, rr)
kw := retrieval.NewBM25(docs)

// Tracing con OpenTelemetry (Jaeger, Honeycomb, Datadog...):
ex := otlp.New(otlp.Options{Endpoint: "http://localhost:4318"}); defer ex.Close()
ctx = callbacks.WithHandlers(ctx, ex)
```

Persistencia en otras bases de datos, con el mismo contrato de `checkpoint.Saver`/`store.Store`:

```go
rc := redis.New("localhost:6379")                       // o mongo.Connect, dynamodb.Client, s3.Client
app, _ := g.Compile(graph.WithCheckpointer(redis.NewSaver(rc)), graph.WithStore(redis.NewStore(rc)))
```

Permisos, grafo, sandbox y protocolo de frontend:

```go
// Devuelve solo lo que el usuario puede leer (SpiceDB; permit.FilterRetriever es equivalente):
safe := spicedb.FilterRetriever(retr, client, spicedb.Subject{Type: "user", ID: "alice"}, "document", "view", idOf)

// Graph RAG: sigue enlaces de metadatos desde los mejores resultados.
gr := &graphretriever.Retriever{Store: vs, K: 6, StartK: 2, Depth: 2, Edges: []graphretriever.Edge{{From: "links", To: "id"}}}

// Un pod de Kubernetes como sandbox de un deep agent (archivos + shell aislados):
sb, _ := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
defer sb.Close(ctx)
agent := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})

// Servir un grafo a CopilotKit/assistant-ui mediante el protocolo AG-UI:
http.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any { return graph.State{} }))
```

Lista completa, limitaciones y extensión: [docs/es-ES/INTEGRATIONS.md](docs/es-ES/INTEGRATIONS.md). **Nivel de verificación:** la mayoría de las bases de datos y servicios de infraestructura se probaron contra el servicio real en Docker; los proveedores de API de pago/SaaS (watsonx, Replicate, Azure AI Search, Vectara, Permit, Salesforce, AWS Kendra/Knowledge Bases, SMS...) solo tienen pruebas de contrato con `httptest`. Lo que quedó fuera y por qué: [docs/es-ES/PROVIDERS-ANALISE.md](docs/es-ES/PROVIDERS-ANALISE.md).

### 15. Memoria a largo plazo (LangMem)

El paquete `langmem` es el port de [LangMem](https://github.com/langchain-ai/langmem) sobre `langgraph/store`: el agent recuerda entre conversaciones, aprende con feedback y cabe en contextos largos. Detalles: [docs/es-ES/LANGMEM.md](docs/es-ES/LANGMEM.md) y [capítulo 16 del manual](docs/es-ES/manual/16-memoria-langmem.md).

```go
st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{}) // búsqueda semántica sobre cualquier store
ns := langmem.Namespace{"memories", "{langgraph_user_id}"}           // aislamiento por usuario (de Configurable)

// 1) hot path: el agent gestiona su propia memoria
cfg := langmem.ToolConfig{Namespace: ns}
agent, _ := agents.Create(agents.Options{
	Model:   model,
	Tools:   []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
	Compile: []graph.CompileOption{graph.WithStore(st)},
})

// 2) background: extrae después de la conversación, con debounce
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st))
ex := &langmem.ReflectionExecutor{Manager: sm}
ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 30 * time.Minute})

// 3) procedural: el system prompt aprende con feedback
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{})
nuevo, _ := opt.Optimize(ctx, []langmem.Trajectory{{Messages: conversacion, Feedback: "quería código"}}, langmem.Prompt{Name: "main", Prompt: actual})

// 4) corto plazo: resumen incremental dentro de agents.Create
mw := langmem.SummarizationMiddleware{SummarizeOptions: langmem.SummarizeOptions{Model: m, MaxTokens: 4000}}
```

Perfil (un documento), episodios, varios prompts, `Middleware` listo (inyecta memorias + tools + formación en background): mira los 5 ejemplos en `examples/langmem/`.

### 16. Agent Protocol, ciam y Open SWE

Tres sistemas integrados al ecosistema (referencias: [Agent Protocol](docs/es-ES/AGENT-PROTOCOL.md), [ciam](docs/es-ES/CIAM.md), [Open SWE](docs/es-ES/OPENSWE.md); tutoriales: capítulos [17](docs/es-ES/manual/17-agent-protocol.md), [18](docs/es-ES/manual/18-ciam-contexto.md) y [19](docs/es-ES/manual/19-open-swe.md) del manual).

```go
// Agent Protocol: cualquier grafo compilado se vuelve una API abierta (agents, threads, runs, store, streaming)
srv, _ := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "bot", Graph: agent}}})
http.ListenAndServe(":8123", srv)
c := agentprotocol.NewClient("http://localhost:8123", "")
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{AgentID: "bot", Input: json.RawMessage(`"hola"`)})

// ciam: contexto quirúrgico para asistentes (búsqueda híbrida + ADR/PRD) como tools y como servidor MCP
m := ciam.New(embedder, nil)
m.Index(ctx, "/proyecto", ciam.IndexOptions{})
dc, _ := m.DecisionContext(ctx, "proyecto", "agregar refresh token", 3) // código + ADR + PRD + planes
ciam.Toolkit{M: m, Project: "/proyecto"}.MCPServer(nil).ServeStdio(ctx, os.Stdin, os.Stdout)

// Open SWE: ingeniero (sandbox por thread, PR mediante la tool auditada), revisor, vigilante de CI y disparadores
eng, _ := openswe.NewAgent(openswe.Config{Model: model, GitHub: gh, Sandbox: sb, DraftPRs: true})
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: openswe.AgentRunner{Graph: eng}}, Queue: queue}
http.Handle("/webhooks/github", &triggers.Webhook{Secret: secret, Dispatcher: d, Bot: "openswe", BotLogins: []string{"openswe[bot]"}})
```

Ejemplos offline en `examples/agentprotocol/`, `examples/ciam/` y `examples/openswe/`. La CLI `cmd/ciam` indexa proyectos y sirve MCP (`go install ./cmd/ciam`); la CLI `cmd/knowledge` indexa documentos y responde con RAG, CAG o KAG (`go install ./cmd/knowledge`, véase [docs/es-ES/KNOWLEDGE.md](docs/es-ES/KNOWLEDGE.md)); `cmd/langgraph dev` levanta un servidor de desarrollo ([docs/es-ES/DEV.md](docs/es-ES/DEV.md)).

## Configuración

| Variable | Uso |
|---|---|
| `ANTHROPIC_API_KEY` | Proveedor `anthropic` |
| `OPENAI_API_KEY`, `OPENAI_BASE_URL` | Proveedor `openai` (y endpoints compatibles) |
| `<PROVEEDOR>_API_KEY` | `GROQ_API_KEY`, `TOGETHER_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `MISTRALAI_API_KEY`, `FIREWORKS_API_KEY` |
| `OLLAMA_HOST` | Dirección de Ollama (predeterminado `http://localhost:11434`) |
| `LANGSMITH_API_KEY`, `LANGSMITH_PROJECT`, `LANGSMITH_ENDPOINT` | Tracing |
| `LANGGRAPH_API_KEY` | Cliente `remote` |
| `<PROVEEDOR>_BASE_URL` | Endpoint de proveedores sin URL predeterminada (`DATABRICKS_BASE_URL`, `CLOUDFLARE_BASE_URL`, gateways) y sobrescritura de cualquier proveedor compatible |
| `WATSONX_API_KEY`, `WATSONX_URL`, `WATSONX_PROJECT_ID` | Proveedor `ibm`/`watsonx` |
| `REPLICATE_API_TOKEN` | Proveedor `replicate` |
| `PERMIT_API_KEY`, `VECTARA_API_KEY`, `ZOTERO_API_KEY`, `YOUTUBE_API_KEY`, `PERIGON_API_KEY`, `OXYLABS_CREDENTIALS` | Integraciones de búsqueda y permisos |

Nunca pongas claves en el código ni en repositorios; usa variables de entorno o un gestor de secretos.

## Ejemplos ejecutables

74 ejemplos en [`examples/`](examples/) (36 portados de los proyectos base, 3 propios de LangGraph, 1 de evals, 6 de integraciones, 6 de RAG/CAG/KAG, 5 de LangMem, 2 de Agent Protocol, 2 de ciam, 4 de Open SWE y 9 de compresión de llamadas) (índice en [examples/README.es-ES.md](examples/README.es-ES.md)). Sin clave de API usan un modelo falso. Cada carpeta de ejemplo tiene un README en inglés, portugués y español.

```bash
go run ./examples/langchain/chain         # prompt | modelo | parser
go run ./examples/langgraph/react_agent_middleware   # agent con tool y middleware
go run ./examples/langgraph/hitl_agent          # se pausa para aprobación humana
go run ./examples/deepagents/research     # deep agent con subagents
go run ./examples/integrations/graph_rag  # retrieval que sigue enlaces
go run ./examples/integrations/agui_server # grafo servido por el protocolo AG-UI
go run ./examples/langmem/hot_path        # agent con memoria a largo plazo por usuario
go run ./examples/agentprotocol/serve_and_call  # agent servido por Agent Protocol + cliente
go run ./examples/ciam/project_context    # índice, búsqueda híbrida, ADR y MCP
go run ./examples/openswe/engineer        # issue → sandbox → PR en borrador, con las reglas impuestas

ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch   # con modelo real
```

## Pruebas

```bash
go test -race ./...                                  # todo; las pruebas de servicios reales se omiten solas (sin red)
go test ./internal/sqlitetest/...                    # sqlsaver contra SQLite real (driver modernc.org/sqlite)
PG_DSN=... go test ./internal/pgtest/...             # PostgreSQL, MariaDB, CockroachDB, Oracle (drivers pgx, mysql, go-ora)
```

Las pruebas de los proveedores usan `httptest`; ninguna prueba llama APIs externas. Las pruebas de integración con bases de datos y servicios corren contra el servicio real y **se omiten solas** cuando la variable de entorno no está definida (`REDIS_ADDR`, `MONGO_ADDR`,
`DYNAMODB_ENDPOINT`, `S3_ENDPOINT`, `SOLR_URL`, `CLICKHOUSE_URL`, `K8S_SERVER`, ...). Todas usan la suite común de
`internal/conformance`. Las que necesitan un driver SQL viven en el paquete `internal/pgtest` (pgvector, MariaDB, CockroachDB, Oracle,
`sqlsaver`, `sqlstore`). Ejemplos de cómo levantar cada servicio: [docs/es-ES/INTEGRATIONS.md](docs/es-ES/INTEGRATIONS.md#pruebas-contra-servicios-reales).

## Equivalencia con las libs de Python

| Python | Go |
|---|---|
| `prompt \| model \| parser` | `runnables.Pipe3(prompt, models.AsRunnable(m), parser)` |
| `model.with_structured_output(T)` | `models.Structured[T](m, models.MethodTool)` |
| `@tool` | `tools.New[Args](name, desc, fn)` |
| `StateGraph(State)` + `Annotated[list, add_messages]` | `graph.NewStateGraph(graph.Schema{"messages": graph.Messages()})` |
| `graph.compile(checkpointer=...)` | `g.Compile(graph.WithCheckpointer(...))` |
| `config={"configurable": {"thread_id": ...}}` | `runnables.WithConfig(ctx, graph.Thread(id))` |
| `interrupt(v)` / `Command(resume=v)` | `graph.Interrupt(ctx, v)` / `graph.Command{Resume: v}` |
| `Send("n", {...})` / `Command(goto=...)` | `graph.Send{Node, Arg}` / `graph.Command{Goto}` |
| `graph.stream(..., stream_mode=[...])` | `app.StreamEvents(ctx, in, modes...)` |
| `create_agent(model, tools, middleware=[...])` | `agents.Create(agents.Options{...})` |
| `create_deep_agent(...)` | `deepagents.Create(deepagents.Options{...})` |
| `RemoteGraph(...)` | `remote.Graph{...}` |
| `create_memory_store_manager(...)` | `langmem.NewStoreManager(...)` |
| `create_manage_memory_tool(...)` | `langmem.ManageMemory(langmem.ToolConfig{...})` |
| `create_prompt_optimizer(model, kind=...)` | `langmem.NewPromptOptimizer(model, kind, cfg)` |
| `summarize_messages(...)` | `langmem.SummarizeMessages(ctx, msgs, running, opts)` |
| Agent Protocol `POST /runs/wait` | `agentprotocol.NewClient(url, key).CreateAndWait(ctx, req)` |
| `ciam index` / `ciam_decision_context` | `ciam.Manager.Index` / `Manager.DecisionContext` |
| Open SWE `open_pull_request`, reviewer, baby-sit | `openswe.NewAgent`, `openswe.ReviewerRunner`, `babysit.Watcher` |
| Open SWE Slack / Linear | `slack.Handler` + `slack.Notifier`, `linear.Webhook` + `linear.Notifier` |

Detalles de diseño en [docs/es-ES/ARCHITECTURE.md](docs/es-ES/ARCHITECTURE.md).

## Limitaciones

- El estado es un `map[string]any`, no un TypedDict/Pydantic; `graph.NewTyped[T]` ofrece una capa tipada opcional (schema por tags de struct) y `tools.Decode` enlaza valores en structs.
- En `sqlsaver`, los escalares de Go (`int64`, `float32`, `time.Time`, `[]byte`…) vuelven con su tipo original; los números sin tipo explícito vuelven como `int` o `float64`; los tipos propios requieren `checkpoint.Register` (`checkpoint.Strict(true)` hace que un struct no registrado falle al escribir). SQLite y PostgreSQL se probaron contra bases de datos reales.
- En `graph.Interrupt`, el nodo se reejecuta desde el inicio, como en LangGraph de Python; los subgrafos (con o sin checkpointer propio) propagan los interrupts al grafo padre. Cuando varios nodos de un subgrafo se detienen en el mismo super-step, las pausas aparecen y las respuestas se entregan en el orden de las tasks.
- `langmem`: actualización por reemplazo por defecto (JSON patch RFC 6902 con `WithPatchUpdates()`); `ReflectionExecutor` remoto mediante `remote/server`; búsqueda semántica por escaneo o por índice vectorial opcional (`vectorstore.NewHNSW`); lista completa en [docs/es-ES/LANGMEM.md](docs/es-ES/LANGMEM.md#diferencias-respecto-a-langmem-de-python).
- `agentprotocol`: los subgrafos se vuelven namespaces hijos con `graph.Subgraphs()`; los registros de runs persisten con `NewStoreRunStore`, pero el buffer de eventos y las runs en ejecución viven en el proceso: varias réplicas requerirían un log de eventos compartido, un lease de runs (la recuperación marca como error todo run `pending` al iniciar) y enrutar las solicitudes de cancelación/comandos a la réplica que ejecuta la run; hoy, usa afinidad por `thread_id`. `ciam`: API HTTP (`ciam api`) y MCP, índice vectorial opcional; `openswe`: Slack y Linear como disparadores (sin botones/tarjetas de revisión de Slack ni proyectos/ciclos de Linear; verificados con los payloads documentados y el schema público, sin workspace real), el webhook de GitHub procesa los eventos de un mismo issue/PR en orden de llegada solo dentro del proceso; sin dashboard/cuentas, y los guards inspeccionan comandos y scripts escritos por el agent (son barreras de protección, no un límite de seguridad). Detalles en [docs/es-ES/AGENT-PROTOCOL.md](docs/es-ES/AGENT-PROTOCOL.md), [docs/es-ES/CIAM.md](docs/es-ES/CIAM.md) y [docs/es-ES/OPENSWE.md](docs/es-ES/OPENSWE.md).
- No portado: Studio, build/deploy de la CLI (`langgraph dev` es un servidor de demostración; tus grafos se sirven con `dev.Serve`), backends gestionados de LangSmith y experimentos/colas de anotación alojados (los evals corren localmente, véase [docs/es-ES/EVALS.md](docs/es-ES/EVALS.md)).
- `LocalShell` y los servidores MCP vía stdio ejecutan código en el host (registran un aviso en la primera ejecución; `LocalShell.AllowedCommands` restringe los programas); úsalos solo con agents y servidores confiables. Para aislamiento, usa `kubernetes.Sandbox` u otro runner con `backends.Remote`.
- `internal/jsonpatch` pasa la suite oficial `json-patch-tests` (RFC 6902). Los savers Redis, MongoDB, DynamoDB y S3 pasan la suite `internal/conformance` (incluidos los namespaces de subgrafo) contra servicios reales en docker.
- Las integraciones de SaaS/API de pago solo tienen pruebas de contrato; 123 proveedores de la lista de LangChain dependen de documentación de API que no estaba disponible ([docs/es-ES/PROVIDERS-ANALISE.md](docs/es-ES/PROVIDERS-ANALISE.md)).
- Los vector stores `Flat`/`Annoy`/`HNSW` son equivalentes en Go puro a FAISS/Annoy/hnswlib. `LoadAnnoyFile` y `LoadFAISSFlat` leen los archivos nativos (solo lectura; Annoy angular/euclidiano, FAISS flat e IDMap; no IVF, PQ ni HNSW de FAISS). Ambos se prueban contra archivos generados por annoy 1.17.3 y faiss-cpu 1.15.1.
- Las herramientas de envío (SMS) y de lectura de datos (SOQL) actúan sobre sistemas reales: restringe destinos/alcance y exige aprobación humana.
