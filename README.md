# golang-chain-eco

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Go port of the LangChain ecosystem: **langchain-core**, **LangGraph**, the agent with middleware (`create_agent`), **Deep Agents**, **MCP** client and server, **RemoteGraph**, **LangSmith** tracing, **LangMem** long-term memory, **Agent Protocol**, **ciam** (context for assistants) and **Open SWE** (a software factory built on agents).

- The library code uses **only the standard library**: no production package imports a third-party dependency. `go.mod` lists SQL drivers (`pgx`, `mysql`, `go-ora`, `modernc.org/sqlite`), but they are used only by the tests in `internal/pgtest` and `internal/sqlitetest`; whoever uses the library brings the driver they want.
- Typed with generics: `Runnable[I, O]`, `Structured[T]`, `tools.New[Args]`.
- Native concurrency: graph nodes run in goroutines, streaming uses `iter.Seq2`.
- Every example runs offline with a fake model; just set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` to use a real provider.

> **Full usage manual:** [docs/en-US/manual/](docs/en-US/manual/README.md) — 19 chapters, from installation to production.

## Table of contents

- [Usage manual](#usage-manual)
- [Installation](#installation)
- [Package map](#package-map)
- [Quick start](#quick-start)
- [Usage guide](#usage-guide)
  - [Models and providers](#1-models-and-providers)
  - [Chains (prompt, model, parser)](#2-chains-prompt-model-parser)
  - [Structured output](#3-structured-output)
  - [Tools](#4-tools)
  - [RAG: splitters, embeddings, vector store](#5-rag-splitters-embeddings-vector-store)
  - [LangGraph: StateGraph](#6-langgraph-stategraph)
  - [Persistence and human-in-the-loop](#7-persistence-and-human-in-the-loop)
  - [Streaming](#8-streaming)
  - [Agents with middleware](#9-agents-with-middleware)
  - [Deep Agents](#10-deep-agents)
  - [MCP](#11-mcp-model-context-protocol)
  - [Remote graphs](#12-remote-graphs)
  - [Tracing with LangSmith](#13-tracing-with-langsmith)
  - [Functional API (entrypoint + task)](#14-functional-api-entrypoint--task)
  - [Long-term memory (LangMem)](#15-long-term-memory-langmem)
  - [Agent Protocol, ciam and Open SWE](#16-agent-protocol-ciam-and-open-swe)
- [Configuration](#configuration)
- [Runnable examples](#runnable-examples)
- [Tests](#tests)
- [Equivalence with the Python libs](#equivalence-with-the-python-libs)
- [Limitations](#limitations)

## Usage manual

Detailed guide, with examples and troubleshooting: [docs/en-US/manual/README.md](docs/en-US/manual/README.md). All reference documents are listed in [docs/en-US/README.md](docs/en-US/README.md).

| # | Chapter |
|---|---|
| 1 | [Introduction and installation](docs/en-US/manual/01-introducao-e-instalacao.md) |
| 2 | [Core concepts](docs/en-US/manual/02-conceitos.md) |
| 3 | [Models and providers](docs/en-US/manual/03-modelos-e-provedores.md) |
| 4 | [Prompts, chains and parsers](docs/en-US/manual/04-chains-prompts-parsers.md) |
| 5 | [Tools](docs/en-US/manual/05-tools.md) |
| 6 | [RAG](docs/en-US/manual/06-rag.md) |
| 7 | [LangGraph: StateGraph](docs/en-US/manual/07-langgraph.md) |
| 8 | [Persistence, human-in-the-loop and streaming](docs/en-US/manual/08-persistencia-hitl-streaming.md) |
| 9 | [Agents and middleware](docs/en-US/manual/09-agents-e-middleware.md) |
| 10 | [Deep Agents](docs/en-US/manual/10-deep-agents.md) |
| 11 | [MCP, server and remote graphs](docs/en-US/manual/11-mcp-remote-servidor.md) |
| 12 | [Observability](docs/en-US/manual/12-observabilidade.md) |
| 13 | [Integrations](docs/en-US/manual/13-integracoes.md) |
| 14 | [Production, security and troubleshooting](docs/en-US/manual/14-producao-seguranca-troubleshooting.md) |
| 15 | [Examples and recipes](docs/en-US/manual/15-exemplos-e-receitas.md) |
| 20 | [RAG, CAG and KAG](docs/en-US/manual/20-rag-cag-kag.md) |

## Installation

```bash
go get github.com/fsvxavier/golang-chain-eco@latest
```

Requires Go 1.27 or later (the version declared in `go.mod`). To develop in this repository:

```bash
git clone https://github.com/fsvxavier/golang-chain-eco
cd golang-chain-eco
go build ./...
go test -race ./...
```

## Package map

| Package | What it does | Python equivalent |
|---|---|---|
| `core/messages` | Messages, tool calls, `AddMessages` reducer, trim/filter/merge | `langchain_core.messages` |
| `core/prompts` | Prompt and chat templates | `langchain_core.prompts` |
| `core/runnables` | `Runnable[I,O]`, `Pipe`, `Parallel`, `Branch`, retry, fallbacks | LCEL / `Runnable` |
| `core/models` | `ChatModel` interface, `Structured[T]`, cache, rate limiter, `Fake` | `BaseChatModel` |
| `core/outputparsers` | `Str`, `JSON`, `Struct[T]`, `CommaList`, `ToolCalls` | output parsers |
| `core/tools` | `tools.New[Args]`, `NewRaw`, validation by JSON Schema | `@tool` |
| `core/documents`, `textsplitters`, `embeddings`, `vectorstores`, `retrievers` | RAG pipeline | same |
| `knowledge`, `rag`, `cag`, `kag` | Common `Answerer`, router, hybrid RAG with incremental indexing, CAG with a cacheable prefix, KAG with a knowledge graph and multi-hop reasoning (see [docs/en-US/KNOWLEDGE.md](docs/en-US/KNOWLEDGE.md)) | RAG / CAG / KAG |
| `core/callbacks` | Events, tracer, logger | callbacks |
| `core/chathistory` | Chat history (memory, file, store) | `ChatMessageHistory` |
| `chatmodels` | `Init("anthropic:claude-...")` | `init_chat_model` |
| `providers/{openai,anthropic,ollama}` | HTTP clients, SSE streaming, tools, embeddings | `langchain-openai` etc. |
| `providers/router` | Failover / round-robin / weighted chat model over several providers and gateways (`router:<model>`) | — |
| `langgraph/graph` | `StateGraph`, Pregel runtime, `Command`, `Send`, `Interrupt`, durability, optional typed state (`NewTyped[T]`) | `langgraph` |
| `langgraph/channels` | State channels and reducers | `langgraph.channels` |
| `langgraph/checkpoint` | `Saver` and `Memory` (with subgraph namespaces), JSON serde | `langgraph.checkpoint` |
| `langgraph/checkpoint/sqlsaver` | SQLite/Postgres saver via `database/sql` | `checkpoint-sqlite/postgres` |
| `langgraph/store` | Long-term memory across threads | `BaseStore` |
| `langgraph/prebuilt` | `ToolNode`, `CreateReactAgent` | `langgraph.prebuilt` |
| `langgraph/functional` | `Entrypoint` and `Task` | `@entrypoint` / `@task` |
| `agents`, `agents/middleware` | `agents.Create` plus ready-made middleware | `create_agent` |
| `deepagents` (+ `backends`, `middleware`) | Agent with planning, virtual filesystem, subagents | `deepagents` |
| `compress` | Call compression: `Summary`, `RuleDecider`, `Guarded` fallback, `HTTPDecider` (Laya server), Caveman text compressor and RTK shell routing; `middleware.Compression` in `agents/middleware`; laya-go compiled only with the `laya_native` tag (`full_team/native`), and `decider_laya_native` as its own module ([docs/en-US/COMPRESSION.md](docs/en-US/COMPRESSION.md)) | — |
| `mcp` | MCP client (stdio and HTTP) | `langchain-mcp-adapters` |
| `remote` | Agent Server client and `RemoteGraph` | `langgraph_sdk` |
| `tracing/{langsmith,langfuse,otlp}` | Sends runs to LangSmith, Langfuse and OpenTelemetry | tracers |
| `integrations/vectorstore` | 25 backends (the local indexes `Flat`, `Annoy` and `HNSW` included, with reading of `.ann` files and flat FAISS; plus Redis, FalkorDB, MongoDB/Atlas and Memgraph in their own packages = 29): Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, pgvector, Typesense, Upstash, Neo4j, SurrealDB, MariaDB, ClickHouse, CrateDB, CockroachDB, Oracle 23ai, Solr, ArcadeDB, Infinispan, Couchbase, Azure AI Search, Astra | `langchain-community` and partners |
| `integrations/{redis,mongo,dynamodb,s3}` | `checkpoint.Saver` and `store.Store` on Redis, MongoDB, DynamoDB and S3 (AWS, MinIO, Tigris, R2...); Redis, MongoDB/Atlas and FalkorDB also as vector stores | checkpointers/stores |
| `integrations/{search,loaders,retrieval}` | ~30 search engines and scrapers (Tavily, Exa, Brave, You.com, Zotero, YouTube...), loaders, BM25 and rerank | `langchain-community` |
| `integrations/graphstore` | `kag.GraphStore` on Neo4j (HTTP) and Memgraph (Bolt), with validated relation types | knowledge graphs |
| `integrations/{graphretriever,spicedb,permit,vectara,aws}` | Graph RAG, permission-filtered retrievers (SpiceDB, Permit), Vectara, Bedrock Knowledge Bases, Kendra, Lambda tool | retrievers |
| `integrations/{kubernetes,jenkins,salesforce,messaging,memgraph,agui}` | Pod sandbox, Jenkins, read-only SOQL, SMS (Plivo/Telnyx), Memgraph (Bolt), AG-UI protocol | tools / protocols |
| `providers/{google,cohere,bedrock,watsonx,replicate}` | Gemini/Vertex, Cohere (chat, embeddings, rerank), Bedrock Converse, IBM watsonx.ai, Replicate | partners |
| `agentprotocol` | Open Agent Protocol API (agents, threads, runs, store) + per-thread streaming (SSE and WebSocket) and client | `agent-protocol` |
| `ciam` | Context for assistants: incremental index, hybrid search, ADR/PRD/plans, memory, tools and MCP server | `context-ia-manager` |
| `mcp/server` | Generic MCP server for any `[]tools.Tool` (stdio and HTTP) | — |
| `openswe/{prompts,github,sandbox,guards,review,babysit,triggers,slack,linear}` and `openswe` | Software factory: engineer with a per-thread sandbox, PR reviewer, CI watcher, webhooks, cron, automations, Slack and Linear | `open-swe` |
| `langmem` | Long-term memory: semantic/episodic managers, memory tools, prompt optimization, incremental summarization, background and middleware | `langmem` |
| `remote/server` | Graph server compatible with the threads/runs API | Agent Server |
| `langgraph/store/sqlstore` | Long-term store on SQLite/Postgres | `PostgresStore` |
| `evals` | Offline evaluation: JSONL datasets, evaluators (exact, regex, JSON, embedding, LLM-as-judge, tool trajectory), text/JSON report | `langsmith.evaluation`, `openevals`, `agentevals` |
| `langgraph/dev` and `cmd/langgraph` | Development server (Agent Protocol) for your graphs; `langgraph dev` serves a demo agent | `langgraph dev` |

## Quick start

An agent with one tool, in under 30 lines:

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
	City string `json:"city" description:"City name"`
}

func main() {
	weather := tools.New("get_weather", "Current weather for a city.",
		func(ctx context.Context, a WeatherArgs) (any, error) {
			return "Sunny, 24°C in " + a.City, nil
		})

	agent, err := agents.Create(agents.Options{
		Model:        chatmodels.MustInit("anthropic:claude-sonnet-4-5"), // requires ANTHROPIC_API_KEY
		Tools:        []tools.Tool{weather},
		SystemPrompt: "You are a weather assistant.",
	})
	if err != nil {
		log.Fatal(err)
	}

	out, err := agent.Invoke(context.Background(), graph.State{"messages": "What's the weather in Lisbon?"})
	if err != nil {
		log.Fatal(err)
	}
	msgs := out["messages"].([]messages.Message)
	fmt.Println(msgs[len(msgs)-1].Text())
}
```

The graph state is a `map[string]any` (`graph.State`). The `messages` key accepts a string (becomes a human message), a `messages.Message` or a `[]messages.Message`.

## Usage guide

### 1. Models and providers

```go
import (
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

// By "provider:model" string (equivalent to init_chat_model).
m, err := chatmodels.Init("openai:gpt-4o-mini", models.WithTemperature(0))
m2 := chatmodels.MustInit("ollama:llama3.1")
m3 := chatmodels.MustInit("groq:llama-3.3-70b-versatile") // OpenAI-compatible, reads GROQ_API_KEY

// Direct construction, with its own endpoint (e.g. Azure, vLLM, LM Studio).
local := &openai.Chat{Model: "qwen2.5", BaseURL: "http://localhost:8000/v1", APIKey: "x"}

// Simple call and streaming.
resp, err := m.Generate(ctx, []messages.Message{messages.System("Be brief."), messages.Human("Hi")})
for chunk, err := range m.Stream(ctx, msgs) {
	fmt.Print(chunk.Text())
}
```

Providers with their own client: `openai`, `anthropic`, `ollama`, `google_genai`, `cohere`, `bedrock`, `azure_openai`, `ibm`/`watsonx`, `replicate`. 75 entries are OpenAI-compatible (key in `$<PROVIDER>_API_KEY`): `groq`, `together`, `openrouter`, `deepseek`, `xai`, `mistralai`, `fireworks`, `sarvam`, `telnyx`...; the ones with per-tenant endpoints (`databricks`, `cloudflare`, `snowflake`, `runpod` and third-party gateways) require `$<PROVIDER>_BASE_URL`. Full matrix of LangChain's 504 providers: [docs/en-US/PROVIDERS.md](docs/en-US/PROVIDERS.md). Without a prefix, `gpt-*`/`o1`/`o3`/`o4` go to OpenAI and `claude*` to Anthropic.

Utilities that wrap any model:

```go
cached := models.WithCache(m, &models.MemoryCache{})            // repeated responses
bound := models.BindTools(m, weather)                           // fixed tools
limited := models.NewRateLimiter(5, 10)                         // token bucket (req/s, burst)
embedder, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small")
```

### 2. Chains (prompt, model, parser)

`Pipe3` is the typed equivalent of `prompt | model | parser`:

```go
prompt := prompts.Chat(
	[2]string{"system", "Answer in one sentence."},
	[2]string{"human", "Explain {topic}."},
)
chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
	prompt, models.AsRunnable(m), outputparsers.Str{},
)
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})

// token streaming from the chain
for tok, err := range runnables.Stream[prompts.Values, string](ctx, chain, prompts.Values{"topic": "channels"}) {
	fmt.Print(tok)
}
```

Useful combinators in `core/runnables`: `Parallel`, `Assign`, `Pick`, `Branch`, `WithRetry`, `WithFallbacks`, `Map`, `Batch`, `Lambda`.

```go
robust := runnables.WithRetry(chain, runnables.RetryOptions{MaxAttempts: 4})
safe := runnables.WithFallbacks(robust, backupChain)
```

### 3. Structured output

```go
type Person struct {
	Name string `json:"name" description:"Full name"`
	Age  int    `json:"age"`
}

extract := models.Structured[Person](m, models.MethodTool) // or MethodJSONSchema
p, err := extract.Invoke(ctx, []messages.Message{messages.Human("Ana Souza is 31 years old.")})
// p == Person{Name: "Ana Souza", Age: 31}
```

### 4. Tools

The argument schema is generated by reflection from the struct (`json`, `description` tags and `jsonschema:"enum=a|b,required"`):

```go
type SearchArgs struct {
	Query string `json:"query" description:"Search term"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
}

search := tools.New("search", "Searches the knowledge base.", func(ctx context.Context, a SearchArgs) (any, error) {
	return doSearch(a.Query, a.Limit)
})

// tools.Error values are shown to the model instead of aborting the run:
return nil, tools.Errorf("city %q not found", a.City)

// Terminal tool (ends the agent loop with its result):
tools.New("finish", "Ends the run.", fn, tools.WithReturnDirect())
```

A tool can also return `graph.Command{Update: ...}` to change the graph state, or `tools.ContentAndArtifact{Content, Artifact}` to attach data that is not sent to the model. Inside a tool, `prebuilt.StateOf(ctx)` returns the current state and `tools.CallID(ctx)` the call ID.

### 5. RAG: splitters, embeddings, vector store

```go
text := loadBigText()
chunks := textsplitters.NewRecursive(800, 100).SplitText(text)

docs := make([]documents.Document, len(chunks))
for i, c := range chunks {
	docs[i] = documents.Document{PageContent: c, Metadata: map[string]any{"source": "manual.txt"}}
}

emb, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small") // or embeddings.Fake{Dim: 64} in tests
store, err := vectorstores.FromDocuments(ctx, emb, docs)

retriever := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4, Type: retrievers.MMR})
hits, err := retriever.Invoke(ctx, "how do I cancel my order?")

// As a tool for an agent (agentic RAG):
ragTool := retrievers.AsTool(retriever, "search_docs", "Searches the documentation.")
```

`textsplitters.ForLanguage("go", 1000, 100)` splits code respecting functions and types; `MarkdownHeaders` splits by headings. `vectorstores.Memory` persists to a file (`Save`/`Load`).

### 6. LangGraph: StateGraph

The state is made of **channels**. Each key has a reducer; without a declaration, "last value wins" applies. Each node receives a copy of the state and returns a partial update.

```go
schema := graph.Schema{
	"messages": graph.Messages(), // accumulates with the add_messages reducer
	"steps":    graph.Sum(),      // sums integers
	"log":      graph.Append(),   // concatenates lists
	"topic":    channels.LastValue(),
}

g := graph.NewStateGraph(schema).
	AddNode("research", func(ctx context.Context, s graph.State) (any, error) {
		return graph.State{"log": []any{"researched " + s["topic"].(string)}, "steps": 1}, nil
	}).
	AddNode("write", writeNode).
	AddNode("review", reviewNode).
	AddEdge(graph.Start, "research").
	AddEdge("research", "write").
	// conditional routing: the router returns the name of the next node (or graph.End)
	AddConditionalEdges("write", func(ctx context.Context, s graph.State) (any, error) {
		if s["steps"].(int) > 3 {
			return graph.End, nil
		}
		return "review", nil
	}).
	AddEdge("review", "write")

app, err := g.Compile()
out, err := app.Invoke(ctx, graph.State{"topic": "Go generics"})
fmt.Println(app.Mermaid()) // diagram
```

Runtime features:

| Feature | How to use it |
|---|---|
| Parallelism | Several `AddEdge(a, x)`, `AddEdge(a, y)`: `x` and `y` run in the same super-step |
| Waiting for several nodes | `AddJoin([]string{"x","y"}, "merge")` |
| Fan-out (map-reduce) | Router or node returns `[]graph.Send{{Node: "worker", Arg: graph.State{...}}}` |
| Control from the node | Return `graph.Command{Update: ..., Goto: []string{"other"}}` |
| Per-node retry | `AddNode("n", fn, graph.WithRetry(runnables.RetryOptions{MaxAttempts: 3}))` |
| Per-node cache | `graph.WithCachePolicy(graph.CachePolicy{TTL: time.Minute})` + `Compile(graph.WithCache(graph.NewMemoryCache()))` |
| Deferred node | `graph.Deferred()` runs only when there are no other pending tasks |
| Step limit | `runnables.Config{RecursionLimit: 50}` (default 25), error `*graph.GraphRecursionError` |
| Subgraphs | Invoke another `*graph.Compiled` inside a node |
| Memory across threads | `Compile(graph.WithStore(store.NewMemory()))` and `graph.StoreFrom(ctx)` in the node |

Fan-out with `Send`:

```go
AddConditionalEdges("plan", func(ctx context.Context, s graph.State) (any, error) {
	var sends []graph.Send
	for _, topic := range s["topics"].([]string) {
		sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"topic": topic}})
	}
	return sends, nil
})
```

### 7. Persistence and human-in-the-loop

With a checkpointer, each super-step is saved by `thread_id`. This enables resuming conversations, pausing for human approval, inspecting and going back in time.

```go
saver := checkpoint.NewMemory()
app, _ := g.Compile(graph.WithCheckpointer(saver))

ctx := runnables.WithConfig(context.Background(), graph.Thread("user-42"))
out, _ := app.Invoke(ctx, graph.State{"messages": "hi"})
out, _ = app.Invoke(ctx, graph.State{"messages": "and now?"}) // continues the same conversation
```

**Pause and resume** with `graph.Interrupt`:

```go
approve := func(ctx context.Context, s graph.State) (any, error) {
	// The first run pauses here. On resume, Interrupt returns the resume value.
	answer := graph.Interrupt(ctx, map[string]any{"question": "Approve sending?"})
	if answer != "yes" {
		return graph.State{"status": "cancelled"}, nil
	}
	return graph.State{"status": "sent"}, nil
}

out, _ := app.Invoke(ctx, graph.State{"status": "new"})
for _, i := range graph.Interrupts(out) {
	fmt.Println("paused at", i.Node, i.Value)
}
out, _ = app.Invoke(ctx, graph.Command{Resume: "yes"}) // resumes
```

The node **re-executes from the beginning** on resume; keep whatever comes before the `Interrupt` idempotent. Static pauses: `Compile(graph.InterruptBefore("tools"))` and `graph.InterruptAfter(...)`; to continue, `app.Invoke(ctx, nil)`.

**Inspection and time travel**:

```go
snap, _ := app.GetState(ctx)                   // Values, Next, Interrupts, CheckpointID
hist, _ := app.GetStateHistory(ctx, 10)        // most recent first
app.UpdateState(ctx, graph.State{"topic": "other"}, "research") // edits as if it were the "research" node

// Fork from an old checkpoint:
old := runnables.WithConfig(ctx, runnables.Config{Configurable: map[string]any{"checkpoint_id": hist[3].CheckpointID}})
app.Invoke(old, nil)
```

**Database persistence** (SQLite or Postgres, with the driver you prefer):

```go
import (
	"database/sql"
	_ "modernc.org/sqlite" // or _ "github.com/jackc/pgx/v5/stdlib"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
)

db, _ := sql.Open("sqlite", "checkpoints.db")
saver, err := sqlsaver.New(ctx, db, sqlsaver.SQLite) // or sqlsaver.Postgres
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

Values are written as JSON. If your state uses your own types, register them so they survive the round trip:

```go
checkpoint.Register("myapp.Order", Order{})
checkpoint.Register("[]myapp.Order", []Order{})
```

Integers come back as `int` and other numbers as `float64`. You can also implement `checkpoint.Saver` (4 methods) for another backend.

### 8. Streaming

```go
for ev, err := range app.StreamEvents(ctx, input, graph.ModeUpdates, graph.ModeMessages, graph.ModeCustom) {
	if err != nil {
		log.Fatal(err)
	}
	switch ev.Mode {
	case graph.ModeUpdates:  // {node: update} each time a node finishes
	case graph.ModeValues:   // full state at each step
	case graph.ModeMessages: // LLM tokens as they arrive (ev.Data is messages.Message)
	case graph.ModeCustom:   // whatever the node sent via graph.StreamWriter(ctx)(value)
	}
}
```

### 9. Agents with middleware

`agents.Create` builds the `model ⇄ tools` loop and lets you plug behavior into defined points: `BeforeAgent`, `BeforeModel`, `WrapModelCall`, `AfterModel`, `WrapToolCall`, `AfterAgent`.

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
	Compile: []graph.CompileOption{graph.WithCheckpointer(saver)}, // HITL requires a checkpointer
	ResponseFormat: &models.ResponseFormat{ /* optional JSON Schema; result in state["structured_response"] */ },
})
```

Included middleware (`agents/middleware`): `ModelCallLimit`, `ToolCallLimit`, `ModelFallback`, `ModelRetry`, `ToolRetry`, `Summarization`, `ContextEditing`, `TodoList`, `PII`, `HumanInTheLoop`.

**Human approval** of a sensitive tool:

```go
out, _ := agent.Invoke(ctx, graph.State{"messages": "Send the report to Bob"})
// pauses before send_email; out carries the request in graph.Interrupts(out)

out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
	Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	// or {Type: middleware.Edit, EditedAction: &middleware.ActionRequest{Name: "send_email", Args: ...}}
	// or {Type: middleware.Reject, Message: "do not send"}
}})
```

**Your own middleware**: implement `Name()` and any hook interface.

```go
type Timing struct{}

func (Timing) Name() string { return "timing" }
func (Timing) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	t := time.Now()
	out, err := next(ctx, req) // req.System, req.Messages, req.Tools and req.Model can be changed before
	log.Println("model took", time.Since(t))
	return out, err
}
```

A hook can divert the flow by returning `graph.State{"jump_to": agents.JumpEnd}` (or `JumpModel`, `JumpTools`). Middleware can also declare tools (`Tools()`) and state channels (`StateSchema()`).

For the minimal case without middleware, there is `prebuilt.CreateReactAgent(prebuilt.ReactOptions{Model, Tools, Prompt})`.

### 10. Deep Agents

Agent for long tasks: task list (`write_todos`), virtual filesystem (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), delegation to subagents (`task`), automatic summarization, memory and skills.

```go
agent, err := deepagents.Create(deepagents.Options{
	Model:        m,
	SystemPrompt: "You are a research analyst.",
	Tools:        []tools.Tool{webSearch},
	SubAgents: []deepagents.SubAgentSpec{{
		Name:         "critic",
		Description:  "Reviews drafts and points out flaws.",
		SystemPrompt: "You are a strict reviewer.",
	}},
	Memory:       []string{"/AGENTS.md"}, // injected into the prompt (if it exists in the backend)
	Skills:       []string{"/skills/"},   // each folder with SKILL.md (name/description in the front matter)
	Checkpointer: saver,
})

out, _ := agent.Invoke(ctx, graph.State{"messages": "Write a report on sodium batteries."})
for path, f := range backends.FilesFromState(out) { // files created by the agent
	fmt.Println(path, len(f.Content))
}
```

There is always a `general-purpose` subagent. Each call to the `task` tool runs an isolated agent and returns only the final answer, keeping the main context small.

**File backends** (`deepagents/backends`):

| Backend | Where files live |
|---|---|
| `backends.State{}` (default) | In the graph state, per thread |
| `backends.Store{Store: s, Namespace: ...}` | In a `store.Store`, shared across threads |
| `backends.Filesystem{Root: "./work"}` | On disk, confined to the root (blocks `..` and symlinks that escape) |
| `backends.LocalShell{...}` | Disk + `execute` tool (**runs commands on the host, with no sandbox: only for trusted use**; `AllowedCommands` restricts the programs) |
| `backends.Composite{Default: ..., Routes: ...}` | Routes by prefix, e.g. `/memories/` to the Store |

```go
be := backends.Composite{
	Default: backends.State{},
	Routes:  map[string]backends.Backend{"/memories/": backends.Store{Store: store.NewMemory(), Namespace: []string{"mem"}}},
}
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: be})
```

**Permissions** per operation and path (the first matching rule wins; it also applies to subagents):

```go
deepagents.Options{
	Model: m,
	Backend: backends.Filesystem{Root: "./work"},
	Permissions: []backends.Permission{
		{Paths: []string{"/secrets/**"}, Deny: true},
		{Operations: []string{backends.OpWrite}, Paths: []string{"/docs/**"}, Deny: true}, // docs are read-only
	},
}
```

**Other Deep Agents middleware** (`deepagents/middleware`), passed in `Options.Middleware`:

```go
// Rubric: a grader evaluates the final answer and returns feedback until the criteria are met.
dmw.Rubric{Grader: m, Criteria: []string{"Cites a source for every number", "Has a 3-line summary"}, MaxIterations: 3}

// Async subagents: the agent starts long work and checks on it later.
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
	{Name: "researcher", Description: "Long research.", Runner: &dmw.LocalRunner{Agent: researcher}},
	// or Runner: dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "researcher"},
}}
```

### 11. MCP (Model Context Protocol)

```go
import "github.com/fsvxavier/golang-chain-eco/mcp"

// Several servers at once; the tools get the "<server>_" prefix.
ts, closeAll, err := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"fs":   {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
	"docs": {URL: "https://mcp.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ..."}},
})
if err != nil { log.Fatal(err) }
defer closeAll()

agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

Low-level use: `mcp.Connect(ctx, mcp.HTTP(url, headers))` or `mcp.Stdio(...)`, then `client.ListTools`, `client.CallTool`. Stdio servers are processes you start with your own permissions: only run trusted servers.

### 12. Remote graphs

Use a graph published on an Agent Server (LangGraph Platform) with the same API as local graphs:

```go
c := remote.NewClient("https://my-server.example.com") // key via LANGGRAPH_API_KEY or c.APIKey
rg := &remote.Graph{Client: c, AssistantID: "my-agent"}

ctx := runnables.WithConfig(ctx, graph.Thread("thread-1"))
out, err := rg.Invoke(ctx, graph.State{"messages": []map[string]any{{"role": "user", "content": "hi"}}})
for st, err := range rg.Stream(ctx, graph.Command{Resume: "yes"}) { /* partial states */ }
```

`remote.Client` also exposes `CreateThread`, `CreateRun`, `GetRun`, `Join`, `CancelRun`, `GetState` and `UpdateState`.

### 13. Tracing with LangSmith

```go
tr := langsmith.New(langsmith.Options{Project: "my-project"}) // LANGSMITH_API_KEY in the environment
defer tr.Close()                                              // flushes the queue

ctx := callbacks.WithHandlers(context.Background(), tr)
agent.Invoke(ctx, input) // chains, models, tools and nodes appear as nested runs
```

Sending is asynchronous and best-effort: network failures never bring the program down (use `Options.OnError` to observe them). For local debugging, `callbacks.StdOut(os.Stderr)` and `callbacks.Logger(slog.Default())` are ready-made handlers.

### 14. Functional API (entrypoint + task)

Write the flow as ordinary Go code; tasks are memoized in the checkpoint, so a resume does not repeat work already done.

```go
wf := functional.New("report", saver, func(ctx context.Context, topic string) (string, error) {
	data, err := functional.Task(ctx, "collect", func(ctx context.Context) (string, error) {
		return collect(topic) // side effects live inside tasks
	}).Result()
	if err != nil {
		return "", err
	}
	ok := graph.Interrupt(ctx, "Publish?").(string) // human-in-the-loop
	if ok != "yes" {
		return "discarded", nil
	}
	return publish(data)
})

ctx := runnables.WithConfig(ctx, graph.Thread("wf-1"))
r, _ := wf.Invoke(ctx, "batteries")  // r.Interrupt != nil: waiting
r, _ = wf.Resume(ctx, "yes")         // collect does NOT run again
```

### Additional integrations

```go
// Gemini, Cohere, Bedrock, Azure and dozens of OpenAI-compatible providers, through the same Init:
m := chatmodels.MustInit("google_genai:gemini-2.0-flash")
m = chatmodels.MustInit("bedrock:anthropic.claude-3-5-sonnet-20241022-v2:0") // AWS_ACCESS_KEY_ID/SECRET/REGION
m = chatmodels.MustInit("perplexity:sonar")                                  // PERPLEXITY_API_KEY
emb, _ := chatmodels.InitEmbeddings("cohere:embed-v4.0")

// External vector store (Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, pgvector):
store := vectorstore.New(emb, &vectorstore.Qdrant{URL: "http://localhost:6333", Collection: "docs"})
store.AddDocuments(ctx, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})

// Web search as an agent tool:
web := search.Tool("web_search", "Searches the web.", search.NewTavily(""), 5)   // TAVILY_API_KEY

// Reranking and BM25 without a database:
rr, _ := chatmodels.NewReranker("cohere:rerank-v3.5")
better := retrieval.WithRerank(retr, rr)
kw := retrieval.NewBM25(docs)

// OpenTelemetry tracing (Jaeger, Honeycomb, Datadog...):
ex := otlp.New(otlp.Options{Endpoint: "http://localhost:4318"}); defer ex.Close()
ctx = callbacks.WithHandlers(ctx, ex)
```

Persistence on other databases, with the same contract as `checkpoint.Saver`/`store.Store`:

```go
rc := redis.New("localhost:6379")                       // or mongo.Connect, dynamodb.Client, s3.Client
app, _ := g.Compile(graph.WithCheckpointer(redis.NewSaver(rc)), graph.WithStore(redis.NewStore(rc)))
```

Permissions, graph, sandbox and frontend protocol:

```go
// Returns only what the user can read (SpiceDB; permit.FilterRetriever is equivalent):
safe := spicedb.FilterRetriever(retr, client, spicedb.Subject{Type: "user", ID: "alice"}, "document", "view", idOf)

// Graph RAG: follows metadata links from the best results.
gr := &graphretriever.Retriever{Store: vs, K: 6, StartK: 2, Depth: 2, Edges: []graphretriever.Edge{{From: "links", To: "id"}}}

// A Kubernetes pod as the sandbox of a deep agent (isolated files + shell):
sb, _ := kubernetes.NewSandbox(ctx, kc, "default", "debian:bookworm-slim")
defer sb.Close(ctx)
agent := deepagents.Create(deepagents.Options{Model: m, Backend: sb.Backend()})

// Serve a graph to CopilotKit/assistant-ui through the AG-UI protocol:
http.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any { return graph.State{} }))
```

Full list, limitations and extension: [docs/en-US/INTEGRATIONS.md](docs/en-US/INTEGRATIONS.md). **Verification level:** most databases and infrastructure services were tested against the real service in Docker; paid API/SaaS providers (watsonx, Replicate, Azure AI Search, Vectara, Permit, Salesforce, AWS Kendra/Knowledge Bases, SMS...) have only contract tests with `httptest`. What was left out and why: [docs/en-US/PROVIDERS-ANALISE.md](docs/en-US/PROVIDERS-ANALISE.md).

### 15. Long-term memory (LangMem)

The `langmem` package is the port of [LangMem](https://github.com/langchain-ai/langmem) on top of `langgraph/store`: the agent remembers across conversations, learns from feedback and fits long contexts. Details: [docs/en-US/LANGMEM.md](docs/en-US/LANGMEM.md) and [chapter 16 of the manual](docs/en-US/manual/16-memoria-langmem.md).

```go
st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{}) // semantic search over any store
ns := langmem.Namespace{"memories", "{langgraph_user_id}"}           // per-user isolation (from Configurable)

// 1) hot path: the agent manages its own memory
cfg := langmem.ToolConfig{Namespace: ns}
agent, _ := agents.Create(agents.Options{
	Model:   model,
	Tools:   []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
	Compile: []graph.CompileOption{graph.WithStore(st)},
})

// 2) background: extracts after the conversation, with debounce
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st))
ex := &langmem.ReflectionExecutor{Manager: sm}
ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 30 * time.Minute})

// 3) procedural: the system prompt learns from feedback
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{})
updated, _ := opt.Optimize(ctx, []langmem.Trajectory{{Messages: conversation, Feedback: "wanted code"}}, langmem.Prompt{Name: "main", Prompt: current})

// 4) short term: incremental summary inside agents.Create
mw := langmem.SummarizationMiddleware{SummarizeOptions: langmem.SummarizeOptions{Model: m, MaxTokens: 4000}}
```

Profile (one document), episodes, multiple prompts, ready-made `Middleware` (injects memories + tools + background formation): see the 5 examples in `examples/langmem/`.

### 16. Agent Protocol, ciam and Open SWE

Three systems integrated into the ecosystem (references: [Agent Protocol](docs/en-US/AGENT-PROTOCOL.md), [ciam](docs/en-US/CIAM.md), [Open SWE](docs/en-US/OPENSWE.md); tutorials: chapters [17](docs/en-US/manual/17-agent-protocol.md), [18](docs/en-US/manual/18-ciam-contexto.md) and [19](docs/en-US/manual/19-open-swe.md) of the manual).

```go
// Agent Protocol: any compiled graph becomes an open API (agents, threads, runs, store, streaming)
srv, _ := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "bot", Graph: agent}}})
http.ListenAndServe(":8123", srv)
c := agentprotocol.NewClient("http://localhost:8123", "")
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{AgentID: "bot", Input: json.RawMessage(`"hi"`)})

// ciam: surgical context for assistants (hybrid search + ADR/PRD) as tools and as an MCP server
m := ciam.New(embedder, nil)
m.Index(ctx, "/project", ciam.IndexOptions{})
dc, _ := m.DecisionContext(ctx, "project", "add refresh token", 3) // code + ADR + PRD + plans
ciam.Toolkit{M: m, Project: "/project"}.MCPServer(nil).ServeStdio(ctx, os.Stdin, os.Stdout)

// Open SWE: engineer (per-thread sandbox, PR through the audited tool), reviewer, CI watcher and triggers
eng, _ := openswe.NewAgent(openswe.Config{Model: model, GitHub: gh, Sandbox: sb, DraftPRs: true})
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: openswe.AgentRunner{Graph: eng}}, Queue: queue}
http.Handle("/webhooks/github", &triggers.Webhook{Secret: secret, Dispatcher: d, Bot: "openswe", BotLogins: []string{"openswe[bot]"}})
```

Offline examples in `examples/agentprotocol/`, `examples/ciam/` and `examples/openswe/`. The `cmd/ciam` CLI indexes projects and serves MCP (`go install ./cmd/ciam`); the `cmd/knowledge` CLI indexes documents and answers with RAG, CAG or KAG (`go install ./cmd/knowledge`, see [docs/en-US/KNOWLEDGE.md](docs/en-US/KNOWLEDGE.md)); `cmd/langgraph dev` starts a development server ([docs/en-US/DEV.md](docs/en-US/DEV.md)).

## Configuration

| Variable | Use |
|---|---|
| `ANTHROPIC_API_KEY` | `anthropic` provider |
| `OPENAI_API_KEY`, `OPENAI_BASE_URL` | `openai` provider (and compatible endpoints) |
| `<PROVIDER>_API_KEY` | `GROQ_API_KEY`, `TOGETHER_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `MISTRALAI_API_KEY`, `FIREWORKS_API_KEY` |
| `OLLAMA_HOST` | Ollama address (default `http://localhost:11434`) |
| `LANGSMITH_API_KEY`, `LANGSMITH_PROJECT`, `LANGSMITH_ENDPOINT` | Tracing |
| `LANGGRAPH_API_KEY` | `remote` client |
| `<PROVIDER>_BASE_URL` | Endpoint for providers with no default URL (`DATABRICKS_BASE_URL`, `CLOUDFLARE_BASE_URL`, gateways) and override for any compatible provider |
| `WATSONX_API_KEY`, `WATSONX_URL`, `WATSONX_PROJECT_ID` | `ibm`/`watsonx` provider |
| `REPLICATE_API_TOKEN` | `replicate` provider |
| `PERMIT_API_KEY`, `VECTARA_API_KEY`, `ZOTERO_API_KEY`, `YOUTUBE_API_KEY`, `PERIGON_API_KEY`, `OXYLABS_CREDENTIALS` | Search and permission integrations |

Never put keys in code or in repositories; use environment variables or a secrets vault.

## Runnable examples

72 examples in [`examples/`](examples/) (36 ported from the base projects, 3 original to LangGraph, 1 for evals, 6 integrations, 6 for RAG/CAG/KAG, 5 for LangMem, 2 for Agent Protocol, 2 for ciam, 4 for Open SWE and 7 for call compression) (index in [examples/README.md](examples/README.md)). Without an API key they use a fake model. Each example folder has a README in English, Portuguese and Spanish.

```bash
go run ./examples/langchain/chain         # prompt | model | parser
go run ./examples/langgraph/react_agent_middleware   # agent with a tool and middleware
go run ./examples/langgraph/hitl_agent          # pauses for human approval
go run ./examples/deepagents/research     # deep agent with subagents
go run ./examples/integrations/graph_rag  # retrieval that follows links
go run ./examples/integrations/agui_server # graph served over the AG-UI protocol
go run ./examples/langmem/hot_path        # agent with per-user long-term memory
go run ./examples/agentprotocol/serve_and_call  # agent served by Agent Protocol + client
go run ./examples/ciam/project_context    # index, hybrid search, ADR and MCP
go run ./examples/openswe/engineer        # issue → sandbox → draft PR, with the rules enforced

ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch   # with a real model
```

## Tests

```bash
go test -race ./...                                  # everything; tests for real services skip themselves (no network)
go test ./internal/sqlitetest/...                    # sqlsaver against real SQLite (modernc.org/sqlite driver)
PG_DSN=... go test ./internal/pgtest/...             # PostgreSQL, MariaDB, CockroachDB, Oracle (pgx, mysql, go-ora drivers)
```

Provider tests use `httptest`; no test calls external APIs. Integration tests with databases and services run against the real service and **skip themselves** when the environment variable is not set (`REDIS_ADDR`, `MONGO_ADDR`,
`DYNAMODB_ENDPOINT`, `S3_ENDPOINT`, `SOLR_URL`, `CLICKHOUSE_URL`, `K8S_SERVER`, ...). All of them use the shared suite in
`internal/conformance`. Those that need a SQL driver live in the `internal/pgtest` package (pgvector, MariaDB, CockroachDB, Oracle,
`sqlsaver`, `sqlstore`). Examples of how to start each service: [docs/en-US/INTEGRATIONS.md](docs/en-US/INTEGRATIONS.md#testing-against-real-services).

## Equivalence with the Python libs

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

Design details in [docs/en-US/ARCHITECTURE.md](docs/en-US/ARCHITECTURE.md).

## Limitations

- The state is a `map[string]any`, not a TypedDict/Pydantic; `graph.NewTyped[T]` offers an optional typed layer (schema from struct tags) and `tools.Decode` binds values into structs.
- In `sqlsaver`, Go scalars (`int64`, `float32`, `time.Time`, `[]byte`…) come back with their original type; untyped numbers come back as `int` or `float64`; custom types require `checkpoint.Register` (`checkpoint.Strict(true)` makes an unregistered struct fail on write). SQLite and PostgreSQL were tested against real databases.
- In `graph.Interrupt`, the node re-executes from the beginning, as in Python LangGraph; subgraphs (with or without their own checkpointer) propagate interrupts to the parent graph. When several nodes of a subgraph stop in the same super-step, the pauses appear and the answers are delivered in task order.
- `langmem`: replacement updates by default (JSON patch RFC 6902 with `WithPatchUpdates()`); remote `ReflectionExecutor` via `remote/server`; semantic search by scan or by an optional vector index (`vectorstore.NewHNSW`); full list in [docs/en-US/LANGMEM.md](docs/en-US/LANGMEM.md#differences-from-python-langmem).
- `agentprotocol`: subgraphs become child namespaces with `graph.Subgraphs()`; run records persist with `NewStoreRunStore`, but the event buffer and running runs live in the process: multiple replicas would require a shared event log, a run lease (recovery marks every `pending` run as an error at startup) and routing of cancel/command requests to the replica executing the run; for now, use affinity by `thread_id`. `ciam`: HTTP API (`ciam api`) and MCP, optional vector index. `openswe`: Slack and Linear as triggers (no Slack buttons/review cards and no Linear projects/cycles; checked against the documented payloads and the public schema, without a real workspace); the GitHub webhook processes events for the same issue/PR in arrival order only within the process; no dashboard/accounts, and the guards inspect commands and scripts written by the agent (they are guardrails, not a security boundary). Details in [docs/en-US/AGENT-PROTOCOL.md](docs/en-US/AGENT-PROTOCOL.md), [docs/en-US/CIAM.md](docs/en-US/CIAM.md) and [docs/en-US/OPENSWE.md](docs/en-US/OPENSWE.md).
- Not ported: Studio, CLI build/deploy (`langgraph dev` is a demonstration server; your graphs are served with `dev.Serve`), LangSmith managed backends and hosted experiments/annotation queues (evals run locally, see [docs/en-US/EVALS.md](docs/en-US/EVALS.md)).
- `LocalShell` and MCP servers over stdio execute code on the host (they log a warning on first execution; `LocalShell.AllowedCommands` restricts the programs); use them only with trusted agents and servers. For isolation, use `kubernetes.Sandbox` or another runner with `backends.Remote`.
- `internal/jsonpatch` passes the official `json-patch-tests` suite (RFC 6902). The Redis, MongoDB, DynamoDB and S3 savers pass the `internal/conformance` suite (including subgraph namespaces) against real services in Docker.
- Paid API/SaaS integrations have only contract tests; 123 providers on LangChain's list depend on API documentation that was not available ([docs/en-US/PROVIDERS-ANALISE.md](docs/en-US/PROVIDERS-ANALISE.md)).
- The `Flat`/`Annoy`/`HNSW` vector stores are pure-Go equivalents of FAISS/Annoy/hnswlib. `LoadAnnoyFile` and `LoadFAISSFlat` read the native files (read-only; Annoy angular/euclidean, FAISS flat and IDMap; not FAISS IVF, PQ or HNSW). Both are tested against files generated by annoy 1.17.3 and faiss-cpu 1.15.1.
- The sending (SMS) and data-reading (SOQL) tools act on real systems: restrict destinations/scope and require human approval.
