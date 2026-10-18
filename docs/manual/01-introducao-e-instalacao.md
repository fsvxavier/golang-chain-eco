# 1. Introdução e instalação

## 1.1 O que é

`golang-chain-eco` reimplementa em Go as peças do ecossistema LangChain:

| Peça | Pacote | Origem Python |
|---|---|---|
| Blocos base (mensagens, prompts, runnables, tools, RAG) | `core/*` | `langchain-core` |
| Grafos de estado com checkpoint, interrupts e streaming | `langgraph/*` | `langgraph` |
| Agent com loop modelo/tools e middleware | `agents`, `agents/middleware` | `create_agent` |
| Agent para tarefas longas | `deepagents/*` | `deepagents` |
| Cliente MCP | `mcp` | `langchain-mcp-adapters` |
| Cliente e servidor de graphs remotos | `remote`, `remote/server` | `langgraph_sdk` |
| Tracing | `tracing/{langsmith,langfuse,otlp}` | tracers |
| Provedores e integrações | `providers/*`, `integrations/*`, `chatmodels` | parceiros e `langchain-community` |

Características de projeto:

- **Código de produção só com a stdlib**: nenhum pacote da biblioteca importa dependência de terceiros. Os drivers SQL listados no `go.mod` (`pgx`, `mysql`, `go-ora`, `modernc.org/sqlite`) são usados só pelos testes; bancos SQL exigem que você traga o driver.
- **Tipagem com generics**: `Runnable[I, O]`, `Structured[T]`, `tools.New[Args]`.
- **Concorrência nativa**: nós de um grafo rodam em goroutines; streaming usa `iter.Seq2`.
- **Offline por padrão nos testes e exemplos**: um modelo fake permite rodar tudo sem chave de API.

## 1.2 Requisitos

- Go 1.27 ou superior (versão declarada no `go.mod`; usa `iter.Seq2` e generics).
- Para usar um LLM real: uma chave de API do provedor, ou um servidor local (Ollama, vLLM, LM Studio).
- Para persistência em banco: o driver `database/sql` de sua escolha (SQLite, Postgres) ou o serviço correspondente (Redis, MongoDB, DynamoDB, S3).

## 1.3 Instalação

Como dependência de outro projeto:

```bash
go get github.com/fsvxavier/golang-chain-eco@latest
```

Para desenvolver ou rodar os exemplos a partir do repositório:

```bash
git clone https://github.com/fsvxavier/golang-chain-eco
cd golang-chain-eco
go build ./...
go test -race ./...
```

## 1.4 Primeiro programa: um agent com uma tool

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
	City string `json:"city" description:"Nome da cidade"`
}

func main() {
	weather := tools.New("get_weather", "Clima atual de uma cidade.",
		func(ctx context.Context, a WeatherArgs) (any, error) {
			return "Ensolarado, 24°C em " + a.City, nil
		})

	agent, err := agents.Create(agents.Options{
		Model:        chatmodels.MustInit("anthropic:claude-sonnet-4-5"), // exige ANTHROPIC_API_KEY
		Tools:        []tools.Tool{weather},
		SystemPrompt: "Você é um assistente de clima.",
	})
	if err != nil {
		log.Fatal(err)
	}

	out, err := agent.Invoke(context.Background(), graph.State{"messages": "Como está o tempo em Lisboa?"})
	if err != nil {
		log.Fatal(err)
	}
	msgs := out["messages"].([]messages.Message)
	fmt.Println(msgs[len(msgs)-1].Text())
}
```

O que acontece:

1. `tools.New` gera o JSON Schema dos argumentos a partir da struct `WeatherArgs`.
2. `chatmodels.MustInit("anthropic:...")` cria o cliente HTTP do provedor. `MustInit` entra em pânico se algo estiver errado (chave ausente, por exemplo); `Init` devolve o erro.
3. `agents.Create` monta o loop `modelo ⇄ tools` como um grafo compilado.
4. `Invoke` recebe o estado de entrada. A chave `messages` aceita uma `string` (vira mensagem humana), uma `messages.Message` ou uma `[]messages.Message`.
5. O resultado é o estado final; `messages` contém toda a conversa, incluindo as chamadas de tool.

## 1.5 Variáveis de ambiente

| Variável | Uso |
|---|---|
| `ANTHROPIC_API_KEY` | Provedor `anthropic` |
| `OPENAI_API_KEY`, `OPENAI_BASE_URL` | Provedor `openai` e endpoints compatíveis |
| `<PROVEDOR>_API_KEY` | `GROQ_API_KEY`, `TOGETHER_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `MISTRALAI_API_KEY`, `FIREWORKS_API_KEY`... |
| `<PROVEDOR>_BASE_URL` | Endpoint de provedores sem URL padrão (`DATABRICKS_BASE_URL`, `CLOUDFLARE_BASE_URL`, gateways) e sobrescrita de qualquer provedor compatível |
| `OLLAMA_HOST` | Endereço do Ollama (padrão `http://localhost:11434`) |
| `LANGSMITH_API_KEY`, `LANGSMITH_PROJECT`, `LANGSMITH_ENDPOINT` | Tracing LangSmith |
| `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, `LANGFUSE_HOST` | Tracing Langfuse |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Tracing OpenTelemetry (padrão `http://localhost:4318`) |
| `LANGGRAPH_API_KEY` | Cliente `remote` |
| `WATSONX_API_KEY`, `WATSONX_URL`, `WATSONX_PROJECT_ID` | Provedor `ibm`/`watsonx` |
| `REPLICATE_API_TOKEN` | Provedor `replicate` |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` | Bedrock, DynamoDB, S3, Kendra, Lambda |
| `GOOGLE_OAUTH_ACCESS_TOKEN` | Google Vertex AI |
| `TAVILY_API_KEY`, `PERMIT_API_KEY`, `VECTARA_API_KEY`, `ZOTERO_API_KEY`, `YOUTUBE_API_KEY`, `PERIGON_API_KEY`, `OXYLABS_CREDENTIALS` | Buscas e integrações |

> **Atenção:** nunca coloque chaves no código nem em repositórios. Use variáveis de ambiente ou um cofre de segredos.

## 1.6 Rodar sem chave de API

Todos os exemplos de `examples/` usam um modelo fake quando não há chave definida:

```bash
go run ./examples/langchain/chain
go run ./examples/langgraph/react_agent_middleware
go run ./examples/langgraph/hitl_agent
go run ./examples/deepagents/research
```

Com chave, o mesmo código usa o provedor real:

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch
```

Para seus próprios testes, use `models.NewFake(...)` (ver [capítulo 3](03-modelos-e-provedores.md#38-modelo-fake-para-testes)) e `embeddings.Fake{Dim: 64}`.

## 1.7 Estrutura do repositório

```
agents/            create_agent + middleware
chatmodels/        Init("provedor:modelo"), InitEmbeddings, NewReranker
core/              messages, prompts, runnables, models, tools, RAG, callbacks, chathistory
deepagents/        agent de tarefas longas (+ backends, middleware)
docs/              arquitetura, integrações, provedores e este manual
examples/          42 programas executáveis
integrations/      vector stores, bancos, buscas, loaders, sandboxes, protocolos
internal/          suítes de conformidade e módulos de teste com drivers (sqlitetest, pgtest)
langgraph/         graph, channels, checkpoint, store, prebuilt, functional
mcp/               cliente MCP (stdio e HTTP)
providers/         clientes nativos (openai, anthropic, ollama, google, cohere, bedrock, watsonx, replicate)
remote/            cliente do Agent Server, RemoteGraph e servidor (remote/server)
tracing/           langsmith, langfuse, otlp
```

Próximo: [2. Conceitos fundamentais](02-conceitos.md).
