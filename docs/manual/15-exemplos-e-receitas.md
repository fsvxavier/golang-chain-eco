# 15. Exemplos e receitas

## 15.1 Como rodar os exemplos

Cada exemplo é um programa independente. Sem chave de API usam um modelo fake determinístico; com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` usam o provedor real.

```bash
go run ./examples/langchain/chain
go run ./examples/langgraph/react_agent_middleware
go run ./examples/langgraph/hitl_agent
go run ./examples/deepagents/research
go run ./examples/integrations/graph_rag

ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch   # com modelo real
```

## 15.2 Catálogo (42 exemplos)

### `examples/langchain`

| Exemplo | Mostra |
|---|---|
| `chain` | `prompt \| modelo \| parser` tipado |
| `rag_chain` | splitter, embeddings, retriever e chain RAG |
| `middleware_stack` | PII + retry + fallback + limites compostos |

### `examples/langgraph`

| Exemplo | Mostra |
|---|---|
| `react_from_scratch` | Loop ReAct montado à mão com `StateGraph` |
| `react_agent_middleware` | `agents.Create` + streaming de updates |
| `structured_output` | Agent que termina com resposta tipada |
| `tool_calling` | Tools em paralelo, erro de tool e tool inexistente |
| `wait_user_input` | `Interrupt` + `Command{Resume}` + checkpoint |
| `hitl_agent` | Aprovação humana de uma tool (middleware) |
| `customer_support` | Tools livres + tool sensível com aprovação/rejeição |
| `subgraph` | Grafo compilado usado como nó |
| `plan_and_execute` | Planner, executor e replanner |
| `rewoo` | Plano completo com placeholders `#E1`, execução e solver |
| `llm_compiler` | DAG de tarefas executado em paralelo com `Send` |
| `reflection` | Gerador e crítico em loop |
| `code_assistant` | Gerar, checar, refletir e tentar de novo |
| `extraction_retries` | Extração validada com retry guiado pelo erro |
| `info_gather` | Chatbot que coleta requisitos e muda de fase |
| `self_discover` | Pipeline de 4 prompts |
| `multi_agent_supervisor` | Supervisor roteando para especialistas |
| `hierarchical_teams` | Supervisor de supervisores (subgrafos por time) |
| `chatbot_simulation` | Usuário simulado + avaliador |
| `corrective_rag` | RAG com avaliação de documentos e fallback web |
| `self_rag` | RAG que checa se a resposta é fundamentada e regenera |
| `adaptive_rag` | Roteador escolhe vector store ou web |
| `sql_agent` | Agent de SQL com guarda somente-leitura |
| `map_reduce` | Fan-out com `Send` e redução |

### `examples/deepagents`

| Exemplo | Mostra |
|---|---|
| `basic` | Planejamento + filesystem virtual |
| `research` | Subagents de pesquisa em paralelo, relatório em arquivo |
| `text_to_sql` | Plano, schema em arquivo, subagent validador |
| `content_builder` | Memória (`AGENTS.md`), skills e arquivos reais em disco |
| `ralph_mode` | Loop com contexto novo por iteração, estado só em arquivos |
| `rubric` | Grader com critérios e revisão iterativa |
| `async_subagents` | Tarefa em background (runner local) |
| `async_subagent_server` | Servidor de agents HTTP + `RemoteRunner` |
| `mcp_docs_agent` | Tools vindas de um servidor MCP |

### `examples/integrations`

| Exemplo | Mostra |
|---|---|
| `graph_rag` | Retriever que segue links de metadados |
| `agui_server` | Grafo servido pelo protocolo AG-UI |
| `permission_rag` | Retriever filtrado por permissão |
| `remote_sandbox` | `backends.Remote` sobre um runner qualquer |
| `vector_indexes` | `Flat` (exato) e `Annoy` (aproximado), recall e persistência |
| `durable_threads` | Estado de thread num checkpointer (S3 se `S3_ENDPOINT` definido) |

Índice oficial: [examples/README.md](../../examples/README.md).

## 15.3 Receitas

### A. Chatbot com memória persistente em SQLite

```go
db, _ := sql.Open("sqlite", "chat.db")
saver, _ := sqlsaver.New(ctx, db, sqlsaver.SQLite)

agent, _ := agents.Create(agents.Options{
	Model:        chatmodels.MustInit("openai:gpt-4o-mini"),
	SystemPrompt: "Você é um assistente prestativo.",
	Compile:      []graph.CompileOption{graph.WithCheckpointer(saver)},
})

chat := func(user, text string) string {
	ctx := runnables.WithConfig(context.Background(), graph.Thread(user))
	out, err := agent.Invoke(ctx, graph.State{"messages": text})
	if err != nil {
		return "erro: " + err.Error()
	}
	msgs := out["messages"].([]messages.Message)
	return msgs[len(msgs)-1].Text()
}
```

### B. Agent de documentação com RAG e fontes

```go
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4, Type: retrievers.MMR})
agent, _ := agents.Create(agents.Options{
	Model: m,
	Tools: []tools.Tool{retrievers.AsTool(retr, "search_docs", "Busca na documentação. Devolve trechos com a fonte.")},
	SystemPrompt: "Responda só com base em search_docs. Cite a fonte (metadata.source). Se não achar, diga que não sabe.",
})
```

### C. Extração de dados em lote

```go
type Invoice struct {
	Number string  `json:"number"`
	Total  float64 `json:"total"`
}
extract := models.Structured[Invoice](m, models.MethodTool)
robust := runnables.WithRetry(extract, runnables.RetryOptions{MaxAttempts: 3})

inputs := make([][]messages.Message, len(texts))
for i, t := range texts {
	inputs[i] = []messages.Message{messages.System("Extraia os dados da fatura."), messages.Human(t)}
}
cfg := runnables.Config{MaxConcurrency: 4}
results, err := runnables.Batch(runnables.WithConfig(ctx, cfg), robust, inputs)
```

### D. Agent com aprovação humana e API HTTP

Fluxo de duas chamadas: a primeira devolve o pedido de aprovação, a segunda retoma.

```go
type reply struct {
	Text     string `json:"text,omitempty"`
	Approval any    `json:"approval,omitempty"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	ctx := runnables.WithConfig(r.Context(), graph.Thread(sessionOf(r)))
	var in any = graph.State{"messages": textOf(r)}
	if decision, ok := decisionOf(r); ok { // o front enviou approve/reject
		in = graph.Command{Resume: middleware.HITLResponse{
			Decisions: []middleware.HITLDecision{{Type: decision}},
		}}
	}
	out, err := s.agent.Invoke(ctx, in)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if reqs := graph.Interrupts(out); len(reqs) > 0 {
		json.NewEncoder(w).Encode(reply{Approval: reqs[0].Value})
		return
	}
	msgs := out["messages"].([]messages.Message)
	json.NewEncoder(w).Encode(reply{Text: msgs[len(msgs)-1].Text()})
}
```

### E. Supervisor com especialistas

Cada especialista é um agent compilado, chamado dentro de um nó do grafo supervisor; o router lê `s["next"]` e desvia. Veja `examples/langgraph/multi_agent_supervisor` e `hierarchical_teams`.

### F. Pesquisa profunda com relatório avaliado

```go
agent, _ := deepagents.Create(deepagents.Options{
	Model:   m,
	Tools:   []tools.Tool{search.Tool("web_search", "Busca na web.", search.NewTavily(""), 5)},
	SubAgents: []deepagents.SubAgentSpec{{Name: "pesquisador", Description: "Pesquisa um subtópico e resume.", SystemPrompt: "..."}},
	Middleware: []agents.Middleware{
		dmw.Rubric{Grader: m, Criteria: []string{"Cada afirmação tem fonte", "Resumo de 3 linhas no topo"}},
	},
})
```

### G. Agent com ferramentas de um servidor MCP

```go
ts, closeAll, _ := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"docs": {URL: "https://mcp.exemplo.com/mcp"},
})
defer closeAll()
agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

## 15.4 Equivalência com as bibliotecas Python

| Python | Go |
|---|---|
| `prompt \| model \| parser` | `runnables.Pipe3(prompt, models.AsRunnable(m), parser)` |
| `init_chat_model("openai:gpt-4o")` | `chatmodels.Init("openai:gpt-4o")` |
| `model.with_structured_output(T)` | `models.Structured[T](m, models.MethodTool)` |
| `model.bind_tools([...])` | `models.BindTools(m, ...)` |
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
| `MultiServerMCPClient(...).get_tools()` | `mcp.LoadTools(ctx, servers)` |
| `@entrypoint` / `@task` | `functional.New(...)` / `functional.Task(...)` |
| `InMemorySaver` / `InMemoryStore` | `checkpoint.NewMemory()` / `store.NewMemory()` |

## 15.5 Glossário

| Termo | Significado |
|---|---|
| **Runnable** | Unidade que recebe `I` e devolve `O`; blocos componíveis |
| **Chain** | Sequência de runnables (`Pipe`) |
| **Estado (State)** | `map[string]any` compartilhado pelos nós de um grafo |
| **Canal (channel)** | Uma chave do estado com sua regra de atualização |
| **Reducer** | Função que combina o valor atual com uma escrita nova |
| **Super-step** | Rodada em que todos os nós agendados rodam em paralelo |
| **Checkpoint** | Foto do estado salva após cada super-step |
| **Thread** | Linha do tempo de checkpoints identificada por `thread_id` |
| **Interrupt** | Pausa que devolve um valor ao chamador e espera uma resposta |
| **Command** | Retorno que atualiza estado, desvia fluxo ou retoma uma pausa |
| **Send** | Dispara uma tarefa de nó com entrada própria (fan-out) |
| **Store** | Memória de longo prazo compartilhada entre threads |
| **Middleware** | Código que se encaixa em pontos do loop do agent |
| **HITL** | Human-in-the-loop: aprovação, edição ou rejeição humana |
| **Backend (Deep Agents)** | Onde ficam os arquivos virtuais do agent |
| **Subagent** | Agent isolado ao qual o principal delega uma tarefa |
| **MCP** | Model Context Protocol: protocolo para expor tools e dados a LLMs |
| **RAG** | Geração aumentada por recuperação de documentos |
| **MMR** | Relevância marginal máxima: busca com diversidade |
| **AG-UI** | Protocolo de eventos entre agents e interfaces de usuário |
| **SSRF** | Fazer o servidor requisitar endereços internos; bloqueado pelo `Fetcher` |

## 15.6 Onde ir a seguir

- Código-fonte e testes de cada pacote mostram o uso exato (`*_test.go`).
- [ARCHITECTURE.md](../ARCHITECTURE.md) explica as decisões de design do runtime.
- [INTEGRATIONS.md](../INTEGRATIONS.md) e [PROVIDERS.md](../PROVIDERS.md) detalham cobertura e verificação.

Voltar ao [índice do manual](README.md).
