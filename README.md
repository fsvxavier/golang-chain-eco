# golang-chain-eco

Port em Go do ecossistema LangChain: **langchain-core**, **LangGraph**, o agent com middleware (`create_agent`), **Deep Agents**, cliente **MCP**, **RemoteGraph** e tracing **LangSmith**.

- Biblioteca com **zero dependências de terceiros** (apenas a stdlib, Go 1.23+).
- Tipado com generics: `Runnable[I, O]`, `Structured[T]`, `tools.New[Args]`.
- Concorrência nativa: nós de um grafo rodam em goroutines, streaming usa `iter.Seq2`.
- Todo exemplo roda offline com um modelo fake; basta definir `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` para usar um provedor real.

## Sumário

- [Instalação](#instalação)
- [Mapa de pacotes](#mapa-de-pacotes)
- [Início rápido](#início-rápido)
- [Guia de uso](#guia-de-uso)
  - [Modelos e provedores](#1-modelos-e-provedores)
  - [Chains (prompt, modelo, parser)](#2-chains-prompt--modelo--parser)
  - [Saída estruturada](#3-saída-estruturada)
  - [Tools](#4-tools)
  - [RAG: splitters, embeddings, vector store](#5-rag-splitters-embeddings-vector-store)
  - [LangGraph: StateGraph](#6-langgraph-stategraph)
  - [Persistência e human-in-the-loop](#7-persistência-e-human-in-the-loop)
  - [Streaming](#8-streaming)
  - [Agents com middleware](#9-agents-com-middleware)
  - [Deep Agents](#10-deep-agents)
  - [MCP](#11-mcp-model-context-protocol)
  - [Graphs remotos](#12-graphs-remotos)
  - [Tracing com LangSmith](#13-tracing-com-langsmith)
  - [API funcional](#14-api-funcional-entrypoint--task)
- [Configuração](#configuração)
- [Exemplos executáveis](#exemplos-executáveis)
- [Testes](#testes)
- [Equivalência com as libs Python](#equivalência-com-as-libs-python)
- [Limitações](#limitações)

## Instalação

```bash
go get github.com/fsvxavier/golang-chain-eco@latest
```

Requer Go 1.23 ou superior. Para desenvolver neste repositório:

```bash
git clone https://github.com/fsvxavier/golang-chain-eco
cd golang-chain-eco
go build ./...
go test -race ./...
```

## Mapa de pacotes

| Pacote | O que faz | Equivalente Python |
|---|---|---|
| `core/messages` | Mensagens, tool calls, reducer `AddMessages`, trim/filter/merge | `langchain_core.messages` |
| `core/prompts` | Templates de prompt e chat | `langchain_core.prompts` |
| `core/runnables` | `Runnable[I,O]`, `Pipe`, `Parallel`, `Branch`, retry, fallbacks | LCEL / `Runnable` |
| `core/models` | Interface `ChatModel`, `Structured[T]`, cache, rate limiter, `Fake` | `BaseChatModel` |
| `core/outputparsers` | `Str`, `JSON`, `Struct[T]`, `CommaList`, `ToolCalls` | output parsers |
| `core/tools` | `tools.New[Args]`, `NewRaw`, validação por JSON Schema | `@tool` |
| `core/documents`, `textsplitters`, `embeddings`, `vectorstores`, `retrievers` | Pipeline de RAG | idem |
| `core/callbacks` | Eventos, tracer, logger | callbacks |
| `core/chathistory` | Histórico de chat (memória, arquivo, store) | `ChatMessageHistory` |
| `chatmodels` | `Init("anthropic:claude-...")` | `init_chat_model` |
| `providers/{openai,anthropic,ollama}` | Clientes HTTP, streaming SSE, tools, embeddings | `langchain-openai` etc. |
| `langgraph/graph` | `StateGraph`, runtime Pregel, `Command`, `Send`, `Interrupt` | `langgraph` |
| `langgraph/channels` | Canais de estado e reducers | `langgraph.channels` |
| `langgraph/checkpoint` | `Saver` e `Memory`, serde JSON | `langgraph.checkpoint` |
| `langgraph/checkpoint/sqlsaver` | Saver SQLite/Postgres via `database/sql` | `checkpoint-sqlite/postgres` |
| `langgraph/store` | Memória de longo prazo entre threads | `BaseStore` |
| `langgraph/prebuilt` | `ToolNode`, `CreateReactAgent` | `langgraph.prebuilt` |
| `langgraph/functional` | `Entrypoint` e `Task` | `@entrypoint` / `@task` |
| `agents`, `agents/middleware` | `agents.Create` + middlewares prontos | `create_agent` |
| `deepagents` (+ `backends`, `middleware`) | Agent com planejamento, filesystem virtual, subagents | `deepagents` |
| `mcp` | Cliente MCP (stdio e HTTP) | `langchain-mcp-adapters` |
| `remote` | Cliente do Agent Server e `RemoteGraph` | `langgraph_sdk` |
| `tracing/{langsmith,langfuse,otlp}` | Envio de runs ao LangSmith, Langfuse e OpenTelemetry | tracers |
| `integrations/{vectorstore,search,loaders,retrieval}` | Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, pgvector; Tavily, Exa, Brave, Serper, Wikipedia, arXiv...; loaders; BM25 e rerank | `langchain-community` e parceiros |
| `providers/{google,cohere,bedrock}` | Gemini/Vertex, Cohere (chat, embeddings, rerank), Bedrock Converse | parceiros |
| `remote/server` | Servidor de grafos compatível com a API de threads/runs | Agent Server |
| `langgraph/store/sqlstore` | Store de longo prazo em SQLite/Postgres | `PostgresStore` |

## Início rápido

Um agent com uma tool, em menos de 30 linhas:

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

O estado do grafo é um `map[string]any` (`graph.State`). A chave `messages` aceita uma string (vira mensagem humana), uma `messages.Message` ou uma `[]messages.Message`.

## Guia de uso

### 1. Modelos e provedores

```go
import (
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

// Por string "provedor:modelo" (equivale a init_chat_model).
m, err := chatmodels.Init("openai:gpt-4o-mini", models.WithTemperature(0))
m2 := chatmodels.MustInit("ollama:llama3.1")
m3 := chatmodels.MustInit("groq:llama-3.3-70b-versatile") // OpenAI-compatível, lê GROQ_API_KEY

// Construção direta, com endpoint próprio (ex.: Azure, vLLM, LM Studio).
local := &openai.Chat{Model: "qwen2.5", BaseURL: "http://localhost:8000/v1", APIKey: "x"}

// Chamada simples e streaming.
resp, err := m.Generate(ctx, []messages.Message{messages.System("Seja breve."), messages.Human("Oi")})
for chunk, err := range m.Stream(ctx, msgs) {
	fmt.Print(chunk.Text())
}
```

Provedores com prefixo próprio: `openai`, `anthropic`, `ollama`. São OpenAI-compatíveis (chave em `$<PROVEDOR>_API_KEY`): `groq`, `together`, `openrouter`, `deepseek`, `xai`, `mistralai`, `fireworks`. Sem prefixo, `gpt-*`/`o1`/`o3`/`o4` vão para OpenAI e `claude*` para Anthropic.

Utilitários que envolvem qualquer modelo:

```go
cached := models.WithCache(m, &models.MemoryCache{})            // respostas repetidas
bound := models.BindTools(m, weather)                           // tools fixas
limited := models.NewRateLimiter(5, 10)                         // token bucket (req/s, burst)
embedder, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small")
```

### 2. Chains (prompt, modelo, parser)

`Pipe3` é o equivalente tipado do `prompt | model | parser`:

```go
prompt := prompts.Chat(
	[2]string{"system", "Responda em uma frase."},
	[2]string{"human", "Explique {topic}."},
)
chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
	prompt, models.AsRunnable(m), outputparsers.Str{},
)
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})

// streaming de tokens da chain
for tok, err := range runnables.Stream[prompts.Values, string](ctx, chain, prompts.Values{"topic": "channels"}) {
	fmt.Print(tok)
}
```

Combinadores úteis em `core/runnables`: `Parallel`, `Assign`, `Pick`, `Branch`, `WithRetry`, `WithFallbacks`, `Map`, `Batch`, `Lambda`.

```go
robust := runnables.WithRetry(chain, runnables.RetryOptions{MaxAttempts: 4})
safe := runnables.WithFallbacks(robust, backupChain)
```

### 3. Saída estruturada

```go
type Person struct {
	Name string `json:"name" description:"Nome completo"`
	Age  int    `json:"age"`
}

extract := models.Structured[Person](m, models.MethodTool) // ou MethodJSONSchema
p, err := extract.Invoke(ctx, []messages.Message{messages.Human("Ana Souza tem 31 anos.")})
// p == Person{Name: "Ana Souza", Age: 31}
```

### 4. Tools

O schema de argumentos é gerado por reflexão a partir da struct (tags `json`, `description`, e `jsonschema:"enum=a|b,required"`):

```go
type SearchArgs struct {
	Query string `json:"query" description:"Termo de busca"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
}

search := tools.New("search", "Busca na base.", func(ctx context.Context, a SearchArgs) (any, error) {
	return doSearch(a.Query, a.Limit)
})

// Erros do tipo tools.Error são mostrados ao modelo em vez de abortar a execução:
return nil, tools.Errorf("cidade %q não encontrada", a.City)

// Tool terminal (encerra o loop do agent com o resultado):
tools.New("finish", "Encerra.", fn, tools.WithReturnDirect())
```

Uma tool também pode retornar `graph.Command{Update: ...}` para alterar o estado do grafo, ou `tools.ContentAndArtifact{Content, Artifact}` para anexar dados que não vão ao modelo. Dentro de uma tool, `prebuilt.StateOf(ctx)` devolve o estado atual e `tools.CallID(ctx)` o ID da chamada.

### 5. RAG: splitters, embeddings, vector store

```go
text := loadBigText()
chunks := textsplitters.NewRecursive(800, 100).SplitText(text)

docs := make([]documents.Document, len(chunks))
for i, c := range chunks {
	docs[i] = documents.Document{PageContent: c, Metadata: map[string]any{"source": "manual.txt"}}
}

emb, _ := chatmodels.InitEmbeddings("openai:text-embedding-3-small") // ou embeddings.Fake{Dim: 64} em testes
store, err := vectorstores.FromDocuments(ctx, emb, docs)

retriever := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4, Type: retrievers.MMR})
hits, err := retriever.Invoke(ctx, "como cancelar o pedido?")

// Como tool para um agent (RAG agêntico):
ragTool := retrievers.AsTool(retriever, "search_docs", "Busca na documentação.")
```

`textsplitters.ForLanguage("go", 1000, 100)` divide código respeitando funções e tipos; `MarkdownHeaders` divide por cabeçalhos. `vectorstores.Memory` persiste em arquivo (`Save`/`Load`).

### 6. LangGraph: StateGraph

O estado é composto por **canais**. Cada chave tem um reducer; sem declarar, vale "último valor". Cada nó recebe uma cópia do estado e devolve uma atualização parcial.

```go
schema := graph.Schema{
	"messages": graph.Messages(), // acumula com o reducer add_messages
	"steps":    graph.Sum(),      // soma inteiros
	"log":      graph.Append(),   // concatena listas
	"topic":    channels.LastValue(),
}

g := graph.NewStateGraph(schema).
	AddNode("research", func(ctx context.Context, s graph.State) (any, error) {
		return graph.State{"log": []any{"pesquisou " + s["topic"].(string)}, "steps": 1}, nil
	}).
	AddNode("write", writeNode).
	AddNode("review", reviewNode).
	AddEdge(graph.Start, "research").
	AddEdge("research", "write").
	// roteamento condicional: o router devolve o nome do próximo nó (ou graph.End)
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

Recursos do runtime:

| Recurso | Como usar |
|---|---|
| Paralelismo | Vários `AddEdge(a, x)`, `AddEdge(a, y)`: `x` e `y` rodam no mesmo super-step |
| Espera por vários nós | `AddJoin([]string{"x","y"}, "merge")` |
| Fan-out (map-reduce) | Router ou nó retorna `[]graph.Send{{Node: "worker", Arg: graph.State{...}}}` |
| Controle no nó | Retornar `graph.Command{Update: ..., Goto: []string{"outro"}}` |
| Retry por nó | `AddNode("n", fn, graph.WithRetry(runnables.RetryOptions{MaxAttempts: 3}))` |
| Cache por nó | `graph.WithCachePolicy(graph.CachePolicy{TTL: time.Minute})` + `Compile(graph.WithCache(graph.NewMemoryCache()))` |
| Nó adiado | `graph.Deferred()` roda só quando não há outras tasks pendentes |
| Limite de passos | `runnables.Config{RecursionLimit: 50}` (padrão 25), erro `*graph.GraphRecursionError` |
| Subgrafos | Invocar outro `*graph.Compiled` dentro de um nó |
| Memória entre threads | `Compile(graph.WithStore(store.NewMemory()))` e `graph.StoreFrom(ctx)` no nó |

Fan-out com `Send`:

```go
AddConditionalEdges("plan", func(ctx context.Context, s graph.State) (any, error) {
	var sends []graph.Send
	for _, topic := range s["topics"].([]string) {
		sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"topic": topic}})
	}
	return sends, nil
})
```

### 7. Persistência e human-in-the-loop

Com um checkpointer, cada super-step é salvo por `thread_id`. Isso habilita retomar conversas, pausar para aprovação humana, inspecionar e voltar no tempo.

```go
saver := checkpoint.NewMemory()
app, _ := g.Compile(graph.WithCheckpointer(saver))

ctx := runnables.WithConfig(context.Background(), graph.Thread("usuario-42"))
out, _ := app.Invoke(ctx, graph.State{"messages": "oi"})
out, _ = app.Invoke(ctx, graph.State{"messages": "e agora?"}) // continua a mesma conversa
```

**Pausar e retomar** com `graph.Interrupt`:

```go
approve := func(ctx context.Context, s graph.State) (any, error) {
	// A primeira execução pausa aqui. Na retomada, Interrupt devolve o valor do resume.
	answer := graph.Interrupt(ctx, map[string]any{"pergunta": "Aprovar o envio?"})
	if answer != "sim" {
		return graph.State{"status": "cancelado"}, nil
	}
	return graph.State{"status": "enviado"}, nil
}

out, _ := app.Invoke(ctx, graph.State{"status": "novo"})
for _, i := range graph.Interrupts(out) {
	fmt.Println("pausado em", i.Node, i.Value)
}
out, _ = app.Invoke(ctx, graph.Command{Resume: "sim"}) // retoma
```

O nó **reexecuta desde o início** na retomada; mantenha idempotente o que vem antes do `Interrupt`. Pausas estáticas: `Compile(graph.InterruptBefore("tools"))` e `graph.InterruptAfter(...)`; para continuar, `app.Invoke(ctx, nil)`.

**Inspeção e time travel**:

```go
snap, _ := app.GetState(ctx)                   // Values, Next, Interrupts, CheckpointID
hist, _ := app.GetStateHistory(ctx, 10)        // mais recentes primeiro
app.UpdateState(ctx, graph.State{"topic": "outro"}, "research") // edita como se fosse o nó "research"

// Fork a partir de um checkpoint antigo:
old := runnables.WithConfig(ctx, runnables.Config{Configurable: map[string]any{"checkpoint_id": hist[3].CheckpointID}})
app.Invoke(old, nil)
```

**Persistência em banco** (SQLite ou Postgres, com o driver que você preferir):

```go
import (
	"database/sql"
	_ "modernc.org/sqlite" // ou _ "github.com/jackc/pgx/v5/stdlib"

	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint/sqlsaver"
)

db, _ := sql.Open("sqlite", "checkpoints.db")
saver, err := sqlsaver.New(ctx, db, sqlsaver.SQLite) // ou sqlsaver.Postgres
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

Os valores são gravados em JSON. Se o seu estado usa tipos próprios, registre-os para que sobrevivam ao round trip:

```go
checkpoint.Register("meuapp.Pedido", Pedido{})
checkpoint.Register("[]meuapp.Pedido", []Pedido{})
```

Inteiros voltam como `int` e demais números como `float64`. Você também pode implementar `checkpoint.Saver` (4 métodos) para outro backend.

### 8. Streaming

```go
for ev, err := range app.StreamEvents(ctx, input, graph.ModeUpdates, graph.ModeMessages, graph.ModeCustom) {
	if err != nil {
		log.Fatal(err)
	}
	switch ev.Mode {
	case graph.ModeUpdates:  // {nó: atualização} a cada nó concluído
	case graph.ModeValues:   // estado completo a cada passo
	case graph.ModeMessages: // tokens do LLM conforme chegam (ev.Data é messages.Message)
	case graph.ModeCustom:   // o que o nó enviou via graph.StreamWriter(ctx)(valor)
	}
}
```

### 9. Agents com middleware

`agents.Create` monta o loop `modelo ⇄ tools` e deixa você plugar comportamento em pontos definidos: `BeforeAgent`, `BeforeModel`, `WrapModelCall`, `AfterModel`, `WrapToolCall`, `AfterAgent`.

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
	ResponseFormat: &models.ResponseFormat{ /* JSON Schema opcional; resultado em state["structured_response"] */ },
})
```

Middlewares incluídos (`agents/middleware`): `ModelCallLimit`, `ToolCallLimit`, `ModelFallback`, `ModelRetry`, `ToolRetry`, `Summarization`, `ContextEditing`, `TodoList`, `PII`, `HumanInTheLoop`.

**Aprovação humana** de uma tool sensível:

```go
out, _ := agent.Invoke(ctx, graph.State{"messages": "Envie o relatório para o Bob"})
// pausa antes de send_email; out traz o pedido em graph.Interrupts(out)

out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{
	Decisions: []middleware.HITLDecision{{Type: middleware.Approve}},
	// ou {Type: middleware.Edit, EditedAction: &middleware.ActionRequest{Name: "send_email", Args: ...}}
	// ou {Type: middleware.Reject, Message: "não envie"}
}})
```

**Middleware próprio**: implemente `Name()` e qualquer interface de hook.

```go
type Timing struct{}

func (Timing) Name() string { return "timing" }
func (Timing) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	t := time.Now()
	out, err := next(ctx, req) // req.System, req.Messages, req.Tools e req.Model podem ser alterados antes
	log.Println("modelo levou", time.Since(t))
	return out, err
}
```

Um hook pode desviar o fluxo retornando `graph.State{"jump_to": agents.JumpEnd}` (ou `JumpModel`, `JumpTools`). Middlewares também podem declarar tools (`Tools()`) e canais de estado (`StateSchema()`).

Para o caso mínimo sem middleware, existe `prebuilt.CreateReactAgent(prebuilt.ReactOptions{Model, Tools, Prompt})`.

### 10. Deep Agents

Agent para tarefas longas: lista de tarefas (`write_todos`), filesystem virtual (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), delegação a subagents (`task`), sumarização automática, memória e skills.

```go
agent, err := deepagents.Create(deepagents.Options{
	Model:        m,
	SystemPrompt: "Você é um analista de pesquisa.",
	Tools:        []tools.Tool{webSearch},
	SubAgents: []deepagents.SubAgentSpec{{
		Name:         "critico",
		Description:  "Revisa rascunhos e aponta falhas.",
		SystemPrompt: "Você é um revisor rigoroso.",
	}},
	Memory:       []string{"/AGENTS.md"}, // injetado no prompt (se existir no backend)
	Skills:       []string{"/skills/"},   // cada pasta com SKILL.md (name/description no front matter)
	Checkpointer: saver,
})

out, _ := agent.Invoke(ctx, graph.State{"messages": "Escreva um relatório sobre baterias de sódio."})
for path, f := range backends.FilesFromState(out) { // arquivos criados pelo agent
	fmt.Println(path, len(f.Content))
}
```

Sempre existe um subagent `general-purpose`. Cada chamada à tool `task` roda um agent isolado e devolve só a resposta final, mantendo o contexto principal pequeno.

**Backends de arquivos** (`deepagents/backends`):

| Backend | Onde ficam os arquivos |
|---|---|
| `backends.State{}` (padrão) | No estado do grafo, por thread |
| `backends.Store{Store: s, Namespace: ...}` | Em um `store.Store`, compartilhado entre threads |
| `backends.Filesystem{Root: "./work"}` | Em disco, confinado à raiz (bloqueia `..` e symlinks que escapam) |
| `backends.LocalShell{...}` | Disco + tool `execute` (**roda comandos no host, sem sandbox: só para uso confiável**) |
| `backends.Composite{Default: ..., Routes: ...}` | Roteia por prefixo, ex.: `/memories/` para o Store |

```go
be := backends.Composite{
	Default: backends.State{},
	Routes:  map[string]backends.Backend{"/memories/": backends.Store{Store: store.NewMemory(), Namespace: []string{"mem"}}},
}
agent, _ := deepagents.Create(deepagents.Options{Model: m, Backend: be})
```

**Permissões** por operação e caminho (a primeira regra que casa vale; vale também para subagents):

```go
deepagents.Options{
	Model: m,
	Backend: backends.Filesystem{Root: "./work"},
	Permissions: []backends.Permission{
		{Paths: []string{"/secrets/**"}, Deny: true},
		{Operations: []string{backends.OpWrite}, Paths: []string{"/docs/**"}, Deny: true}, // docs somente leitura
	},
}
```

**Outros middlewares do Deep Agents** (`deepagents/middleware`), passados em `Options.Middleware`:

```go
// Rubrica: um grader avalia a resposta final e devolve feedback até satisfazer os critérios.
dmw.Rubric{Grader: m, Criteria: []string{"Cita uma fonte para cada número", "Tem resumo de 3 linhas"}, MaxIterations: 3}

// Subagents assíncronos: o agent dispara trabalho longo e consulta depois.
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{
	{Name: "pesquisador", Description: "Pesquisas longas.", Runner: &dmw.LocalRunner{Agent: researcher}},
	// ou Runner: dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "pesquisador"},
}}
```

### 11. MCP (Model Context Protocol)

```go
import "github.com/fsvxavier/golang-chain-eco/mcp"

// Vários servidores de uma vez; as tools ganham o prefixo "<servidor>_".
ts, closeAll, err := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"fs":   {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
	"docs": {URL: "https://mcp.exemplo.com/mcp", Headers: map[string]string{"Authorization": "Bearer ..."}},
})
if err != nil { log.Fatal(err) }
defer closeAll()

agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

Uso de baixo nível: `mcp.Connect(ctx, mcp.HTTP(url, headers))` ou `mcp.Stdio(...)`, depois `client.ListTools`, `client.CallTool`. Servidores stdio são processos iniciados por você com as suas permissões: só execute servidores confiáveis.

### 12. Graphs remotos

Use um grafo publicado em um Agent Server (LangGraph Platform) com a mesma API dos grafos locais:

```go
c := remote.NewClient("https://meu-servidor.exemplo.com") // chave via LANGGRAPH_API_KEY ou c.APIKey
rg := &remote.Graph{Client: c, AssistantID: "meu-agent"}

ctx := runnables.WithConfig(ctx, graph.Thread("thread-1"))
out, err := rg.Invoke(ctx, graph.State{"messages": []map[string]any{{"role": "user", "content": "oi"}}})
for st, err := range rg.Stream(ctx, graph.Command{Resume: "sim"}) { /* estados parciais */ }
```

`remote.Client` também expõe `CreateThread`, `CreateRun`, `GetRun`, `Join`, `CancelRun`, `GetState` e `UpdateState`.

### 13. Tracing com LangSmith

```go
tr := langsmith.New(langsmith.Options{Project: "meu-projeto"}) // LANGSMITH_API_KEY no ambiente
defer tr.Close()                                                // descarrega a fila

ctx := callbacks.WithHandlers(context.Background(), tr)
agent.Invoke(ctx, input) // chains, modelos, tools e nós aparecem como runs aninhadas
```

O envio é assíncrono e best-effort: falhas de rede nunca derrubam o programa (use `Options.OnError` para observá-las). Para depuração local, `callbacks.StdOut(os.Stderr)` e `callbacks.Logger(slog.Default())` são handlers prontos.

### 14. API funcional (entrypoint + task)

Escreva o fluxo como código Go comum; tasks são memoizadas no checkpoint, então uma retomada não repete trabalho já feito.

```go
wf := functional.New("relatorio", saver, func(ctx context.Context, tema string) (string, error) {
	dados, err := functional.Task(ctx, "coletar", func(ctx context.Context) (string, error) {
		return coletar(tema) // efeitos colaterais ficam dentro de tasks
	}).Result()
	if err != nil {
		return "", err
	}
	ok := graph.Interrupt(ctx, "Publicar?").(string) // human-in-the-loop
	if ok != "sim" {
		return "descartado", nil
	}
	return publicar(dados)
})

ctx := runnables.WithConfig(ctx, graph.Thread("wf-1"))
r, _ := wf.Invoke(ctx, "baterias")   // r.Interrupt != nil: aguardando
r, _ = wf.Resume(ctx, "sim")         // coletar NÃO roda de novo
```

### Integrações adicionais

```go
// Gemini, Cohere, Bedrock, Azure e dezenas de provedores compatíveis com OpenAI, pelo mesmo Init:
m := chatmodels.MustInit("google_genai:gemini-2.0-flash")
m = chatmodels.MustInit("bedrock:anthropic.claude-3-5-sonnet-20241022-v2:0") // AWS_ACCESS_KEY_ID/SECRET/REGION
m = chatmodels.MustInit("perplexity:sonar")                                  // PERPLEXITY_API_KEY
emb, _ := chatmodels.InitEmbeddings("cohere:embed-v4.0")

// Vector store externo (Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate, Milvus, pgvector):
store := vectorstore.New(emb, &vectorstore.Qdrant{URL: "http://localhost:6333", Collection: "docs"})
store.AddDocuments(ctx, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})

// Busca na web como tool de um agent:
web := search.Tool("web_search", "Busca na web.", search.NewTavily(""), 5)   // TAVILY_API_KEY

// Reranking e BM25 sem banco:
rr, _ := chatmodels.NewReranker("cohere:rerank-v3.5")
better := retrieval.WithRerank(retr, rr)
kw := retrieval.NewBM25(docs)

// Tracing em OpenTelemetry (Jaeger, Honeycomb, Datadog...):
ex := otlp.New(otlp.Options{Endpoint: "http://localhost:4318"}); defer ex.Close()
ctx = callbacks.WithHandlers(ctx, ex)
```

Lista completa, limitações e extensão: [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md).

## Configuração

| Variável | Uso |
|---|---|
| `ANTHROPIC_API_KEY` | Provedor `anthropic` |
| `OPENAI_API_KEY`, `OPENAI_BASE_URL` | Provedor `openai` (e endpoints compatíveis) |
| `<PROVEDOR>_API_KEY` | `GROQ_API_KEY`, `TOGETHER_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `MISTRALAI_API_KEY`, `FIREWORKS_API_KEY` |
| `OLLAMA_HOST` | Endereço do Ollama (padrão `http://localhost:11434`) |
| `LANGSMITH_API_KEY`, `LANGSMITH_PROJECT`, `LANGSMITH_ENDPOINT` | Tracing |
| `LANGGRAPH_API_KEY` | Cliente `remote` |

Nunca coloque chaves no código nem em repositórios; use variáveis de ambiente ou um cofre de segredos.

## Exemplos executáveis

36 exemplos portados dos projetos base em [`examples/`](examples/) (índice em [examples/README.md](examples/README.md)). Sem chave de API usam um modelo fake.

```bash
go run ./examples/langchain/chain         # prompt | modelo | parser
go run ./examples/langgraph/react_agent_middleware   # agent com tool e middleware
go run ./examples/langgraph/hitl_agent          # pausa para aprovação humana
go run ./examples/deepagents/research     # deep agent com subagents

ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch   # com modelo real
```

## Testes

```bash
go test -race ./...                                  # módulo principal (sem rede, sem dependências)
cd internal/sqlitetest && go test ./...              # sqlsaver contra SQLite real (módulo separado)
```

Os testes dos provedores usam `httptest`; nenhum teste chama APIs externas.

## Equivalência com as libs Python

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

Detalhes de design em [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Limitações

- O estado é um `map[string]any`, não um TypedDict/Pydantic; use `tools.Decode` para ligar em structs.
- No `sqlsaver`, números inteiros voltam como `int` e demais como `float64`; tipos próprios exigem `checkpoint.Register`. Apenas SQLite foi testado contra um banco real.
- Em `graph.Interrupt`, o nó reexecuta desde o início; subgrafos com checkpointer próprio não propagam interrupts para o grafo pai.
- Não portado: modos de durabilidade, Studio/CLI, backends gerenciados do LangSmith, evals.
- `LocalShell` e servidores MCP via stdio executam código no host; use apenas com agents e servidores confiáveis.
