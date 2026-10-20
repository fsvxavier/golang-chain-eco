# 1. Introduction and installation

**English** | [Português](../../pt-BR/manual/01-introducao-e-instalacao.md) | [Español](../../es-ES/manual/01-introducao-e-instalacao.md)

## 1.1 What it is

`golang-chain-eco` reimplements the pieces of the LangChain ecosystem in Go:

| Piece | Package | Python origin |
|---|---|---|
| Base blocks (messages, prompts, runnables, tools, RAG) | `core/*` | `langchain-core` |
| State graphs with checkpoints, interrupts and streaming | `langgraph/*` | `langgraph` |
| Agent with a model/tools loop and middleware | `agents`, `agents/middleware` | `create_agent` |
| Agent for long tasks | `deepagents/*` | `deepagents` |
| MCP client | `mcp` | `langchain-mcp-adapters` |
| Remote graph client and server | `remote`, `remote/server` | `langgraph_sdk` |
| Tracing | `tracing/{langsmith,langfuse,otlp}` | tracers |
| Providers and integrations | `providers/*`, `integrations/*`, `chatmodels` | partners and `langchain-community` |

Project characteristics:

- **Production code uses only the standard library**: no package in the library imports a third-party dependency. The SQL drivers listed in `go.mod` (`pgx`, `mysql`, `go-ora`, `modernc.org/sqlite`) are used only by the tests; SQL databases require you to bring the driver.
- **Typed with generics**: `Runnable[I, O]`, `Structured[T]`, `tools.New[Args]`.
- **Native concurrency**: graph nodes run in goroutines; streaming uses `iter.Seq2`.
- **Offline by default in tests and examples**: a fake model lets you run everything without an API key.

## 1.2 Requirements

- Go 1.27 or later (the version declared in `go.mod`; it uses `iter.Seq2` and generics).
- To use a real LLM: an API key from the provider, or a local server (Ollama, vLLM, LM Studio).
- For database persistence: the `database/sql` driver of your choice (SQLite, Postgres) or the corresponding service (Redis, MongoDB, DynamoDB, S3).

## 1.3 Installation

As a dependency of another project:

```bash
go get github.com/fsvxavier/golang-chain-eco@latest
```

To develop or run the examples from the repository:

```bash
git clone https://github.com/fsvxavier/golang-chain-eco
cd golang-chain-eco
go build ./...
go test -race ./...
```

## 1.4 First program: an agent with one tool

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

What happens:

1. `tools.New` generates the JSON Schema of the arguments from the `WeatherArgs` struct.
2. `chatmodels.MustInit("anthropic:...")` creates the provider's HTTP client. `MustInit` panics if something is wrong (a missing key, for example); `Init` returns the error.
3. `agents.Create` builds the `model ⇄ tools` loop as a compiled graph.
4. `Invoke` receives the input state. The `messages` key accepts a `string` (becomes a human message), a `messages.Message` or a `[]messages.Message`.
5. The result is the final state; `messages` contains the whole conversation, including the tool calls.

## 1.5 Environment variables

| Variable | Use |
|---|---|
| `ANTHROPIC_API_KEY` | `anthropic` provider |
| `OPENAI_API_KEY`, `OPENAI_BASE_URL` | `openai` provider and compatible endpoints |
| `<PROVIDER>_API_KEY` | `GROQ_API_KEY`, `TOGETHER_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `MISTRALAI_API_KEY`, `FIREWORKS_API_KEY`... |
| `<PROVIDER>_BASE_URL` | Endpoint for providers with no default URL (`DATABRICKS_BASE_URL`, `CLOUDFLARE_BASE_URL`, gateways) and override for any compatible provider |
| `OLLAMA_HOST` | Ollama address (default `http://localhost:11434`) |
| `LANGSMITH_API_KEY`, `LANGSMITH_PROJECT`, `LANGSMITH_ENDPOINT` | LangSmith tracing |
| `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, `LANGFUSE_HOST` | Langfuse tracing |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry tracing (default `http://localhost:4318`) |
| `LANGGRAPH_API_KEY` | `remote` client |
| `WATSONX_API_KEY`, `WATSONX_URL`, `WATSONX_PROJECT_ID` | `ibm`/`watsonx` provider |
| `REPLICATE_API_TOKEN` | `replicate` provider |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` | Bedrock, DynamoDB, S3, Kendra, Lambda |
| `GOOGLE_OAUTH_ACCESS_TOKEN` | Google Vertex AI |
| `TAVILY_API_KEY`, `PERMIT_API_KEY`, `VECTARA_API_KEY`, `ZOTERO_API_KEY`, `YOUTUBE_API_KEY`, `PERIGON_API_KEY`, `OXYLABS_CREDENTIALS` | Search and integrations |

> **Warning:** never put keys in code or in repositories. Use environment variables or a secrets vault.

## 1.6 Running without an API key

All the examples in `examples/` use a fake model when no key is set:

```bash
go run ./examples/langchain/chain
go run ./examples/langgraph/react_agent_middleware
go run ./examples/langgraph/hitl_agent
go run ./examples/deepagents/research
```

With a key, the same code uses the real provider:

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch
```

For your own tests, use `models.NewFake(...)` (see [chapter 3](03-modelos-e-provedores.md#38-fake-model-for-tests)) and `embeddings.Fake{Dim: 64}`.

## 1.7 Repository structure

```
agents/            create_agent + middleware
chatmodels/        Init("provider:model"), InitEmbeddings, NewReranker
core/              messages, prompts, runnables, models, tools, RAG, callbacks, chathistory
deepagents/        long-task agent (+ backends, middleware)
docs/              architecture, integrations, providers and this manual
examples/          42 runnable programs
integrations/      vector stores, databases, search, loaders, sandboxes, protocols
internal/          conformance suites and test modules with drivers (sqlitetest, pgtest)
langgraph/         graph, channels, checkpoint, store, prebuilt, functional
mcp/               MCP client (stdio and HTTP)
providers/         native clients (openai, anthropic, ollama, google, cohere, bedrock, watsonx, replicate)
remote/            Agent Server client, RemoteGraph and server (remote/server)
tracing/           langsmith, langfuse, otlp
```

---

Next: [2. Core concepts](02-conceitos.md) · [Manual index](README.md)
