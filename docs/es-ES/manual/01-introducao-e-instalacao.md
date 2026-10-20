# 1. Introducción e instalación

[English](../../en-US/manual/01-introducao-e-instalacao.md) | [Português](../../pt-BR/manual/01-introducao-e-instalacao.md) | **Español**

## 1.1 Qué es

`golang-chain-eco` reimplementa en Go las piezas del ecosistema LangChain:

| Pieza | Paquete | Origen en Python |
|---|---|---|
| Bloques base (mensajes, prompts, runnables, tools, RAG) | `core/*` | `langchain-core` |
| Grafos de estado con checkpoint, interrupts y streaming | `langgraph/*` | `langgraph` |
| Agent con loop modelo/tools y middleware | `agents`, `agents/middleware` | `create_agent` |
| Agent para tareas largas | `deepagents/*` | `deepagents` |
| Cliente MCP | `mcp` | `langchain-mcp-adapters` |
| Cliente y servidor de grafos remotos | `remote`, `remote/server` | `langgraph_sdk` |
| Tracing | `tracing/{langsmith,langfuse,otlp}` | tracers |
| Proveedores e integraciones | `providers/*`, `integrations/*`, `chatmodels` | socios y `langchain-community` |

Características del proyecto:

- **Código de producción solo con la stdlib**: ningún paquete de la biblioteca importa una dependencia de terceros. Los drivers SQL listados en el `go.mod` (`pgx`, `mysql`, `go-ora`, `modernc.org/sqlite`) solo los usan las pruebas; las bases de datos SQL exigen que traigas el driver.
- **Tipado con generics**: `Runnable[I, O]`, `Structured[T]`, `tools.New[Args]`.
- **Concurrencia nativa**: los nodos de un grafo corren en goroutines; el streaming usa `iter.Seq2`.
- **Sin conexión por defecto en pruebas y ejemplos**: un modelo fake permite ejecutar todo sin clave de API.

## 1.2 Requisitos

- Go 1.27 o superior (versión declarada en el `go.mod`; usa `iter.Seq2` y generics).
- Para usar un LLM real: una clave de API del proveedor, o un servidor local (Ollama, vLLM, LM Studio).
- Para persistencia en base de datos: el driver `database/sql` de tu elección (SQLite, Postgres) o el servicio correspondiente (Redis, MongoDB, DynamoDB, S3).

## 1.3 Instalación

Como dependencia de otro proyecto:

```bash
go get github.com/fsvxavier/golang-chain-eco@latest
```

Para desarrollar o ejecutar los ejemplos desde el repositorio:

```bash
git clone https://github.com/fsvxavier/golang-chain-eco
cd golang-chain-eco
go build ./...
go test -race ./...
```

## 1.4 Primer programa: un agent con una tool

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

Qué pasa:

1. `tools.New` genera el JSON Schema de los argumentos a partir de la struct `WeatherArgs`.
2. `chatmodels.MustInit("anthropic:...")` crea el cliente HTTP del proveedor. `MustInit` entra en pánico si algo está mal (una clave ausente, por ejemplo); `Init` devuelve el error.
3. `agents.Create` monta el loop `modelo ⇄ tools` como un grafo compilado.
4. `Invoke` recibe el estado de entrada. La clave `messages` acepta un `string` (se convierte en mensaje humano), un `messages.Message` o un `[]messages.Message`.
5. El resultado es el estado final; `messages` contiene toda la conversación, incluidas las llamadas a tools.

## 1.5 Variables de entorno

| Variable | Uso |
|---|---|
| `ANTHROPIC_API_KEY` | Proveedor `anthropic` |
| `OPENAI_API_KEY`, `OPENAI_BASE_URL` | Proveedor `openai` y endpoints compatibles |
| `<PROVEEDOR>_API_KEY` | `GROQ_API_KEY`, `TOGETHER_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `MISTRALAI_API_KEY`, `FIREWORKS_API_KEY`... |
| `<PROVEEDOR>_BASE_URL` | Endpoint de proveedores sin URL predeterminada (`DATABRICKS_BASE_URL`, `CLOUDFLARE_BASE_URL`, gateways) y sobrescritura de cualquier proveedor compatible |
| `OLLAMA_HOST` | Dirección de Ollama (predeterminado `http://localhost:11434`) |
| `LANGSMITH_API_KEY`, `LANGSMITH_PROJECT`, `LANGSMITH_ENDPOINT` | Tracing de LangSmith |
| `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, `LANGFUSE_HOST` | Tracing de Langfuse |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Tracing de OpenTelemetry (predeterminado `http://localhost:4318`) |
| `LANGGRAPH_API_KEY` | Cliente `remote` |
| `WATSONX_API_KEY`, `WATSONX_URL`, `WATSONX_PROJECT_ID` | Proveedor `ibm`/`watsonx` |
| `REPLICATE_API_TOKEN` | Proveedor `replicate` |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` | Bedrock, DynamoDB, S3, Kendra, Lambda |
| `GOOGLE_OAUTH_ACCESS_TOKEN` | Google Vertex AI |
| `TAVILY_API_KEY`, `PERMIT_API_KEY`, `VECTARA_API_KEY`, `ZOTERO_API_KEY`, `YOUTUBE_API_KEY`, `PERIGON_API_KEY`, `OXYLABS_CREDENTIALS` | Búsquedas e integraciones |

> **Atención:** nunca pongas claves en el código ni en repositorios. Usa variables de entorno o un gestor de secretos.

## 1.6 Ejecutar sin clave de API

Todos los ejemplos de `examples/` usan un modelo fake cuando no hay clave definida:

```bash
go run ./examples/langchain/chain
go run ./examples/langgraph/react_agent_middleware
go run ./examples/langgraph/hitl_agent
go run ./examples/deepagents/research
```

Con clave, el mismo código usa el proveedor real:

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch
```

Para tus propias pruebas, usa `models.NewFake(...)` (véase el [capítulo 3](03-modelos-e-provedores.md#38-modelo-fake-para-pruebas)) y `embeddings.Fake{Dim: 64}`.

## 1.7 Estructura del repositorio

```
agents/            create_agent + middleware
chatmodels/        Init("proveedor:modelo"), InitEmbeddings, NewReranker
core/              messages, prompts, runnables, models, tools, RAG, callbacks, chathistory
deepagents/        agent de tareas largas (+ backends, middleware)
docs/              arquitectura, integraciones, proveedores y este manual
examples/          42 programas ejecutables
integrations/      vector stores, bases de datos, búsquedas, loaders, sandboxes, protocolos
internal/          suites de conformidad y módulos de prueba con drivers (sqlitetest, pgtest)
langgraph/         graph, channels, checkpoint, store, prebuilt, functional
mcp/               cliente MCP (stdio y HTTP)
providers/         clientes nativos (openai, anthropic, ollama, google, cohere, bedrock, watsonx, replicate)
remote/            cliente del Agent Server, RemoteGraph y servidor (remote/server)
tracing/           langsmith, langfuse, otlp
```

---

Siguiente: [2. Conceptos fundamentales](02-conceitos.md) · [Índice del manual](README.md)
