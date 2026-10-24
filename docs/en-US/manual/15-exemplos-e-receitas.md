# 15. Examples and recipes

**English** | [Português](../../pt-BR/manual/15-exemplos-e-receitas.md) | [Español](../../es-ES/manual/15-exemplos-e-receitas.md)

## 15.1 Running the examples

Each example is an independent program. Without an API key they use a deterministic fake model; with `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` they use the real provider.

```bash
go run ./examples/langchain/chain
go run ./examples/langgraph/react_agent_middleware
go run ./examples/langgraph/hitl_agent
go run ./examples/deepagents/research
go run ./examples/integrations/graph_rag

ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch   # with a real model
```

## 15.2 Catalog (64 examples)

Each example folder has a `README.md` (English), `README.pt-BR.md` and `README.es-ES.md` that explain what it does, how to apply the pattern, how to run it and how to test it in production mode.

### `examples/langchain`

| Example | Shows |
|---|---|
| `chain` | Typed `prompt \| model \| parser` |
| `rag_chain` | Splitter, embeddings, retriever and RAG chain |
| `middleware_stack` | PII + retry + fallback + composed limits |

### `examples/langgraph`

| Example | Shows |
|---|---|
| `react_from_scratch` | ReAct loop assembled by hand with `StateGraph` |
| `react_agent_middleware` | `agents.Create` + update streaming |
| `structured_output` | Agent that ends with a typed response |
| `tool_calling` | Parallel tools, tool error and non-existent tool |
| `wait_user_input` | `Interrupt` + `Command{Resume}` + checkpoint |
| `hitl_agent` | Human approval of a tool (middleware) |
| `customer_support` | Free tools + sensitive tool with approval/rejection |
| `subgraph` | Compiled graph used as a node |
| `plan_and_execute` | Planner, executor and replanner |
| `rewoo` | Full plan with `#E1` placeholders, execution and solver |
| `llm_compiler` | Task DAG executed in parallel with `Send` |
| `reflection` | Generator and critic in a loop |
| `code_assistant` | Generate, check, reflect and try again |
| `extraction_retries` | Validated extraction with retry guided by the error |
| `info_gather` | Chatbot that gathers requirements and changes phase |
| `self_discover` | 4-prompt pipeline |
| `multi_agent_supervisor` | Supervisor routing to specialists |
| `hierarchical_teams` | Supervisor of supervisors (subgraphs per team) |
| `chatbot_simulation` | Simulated user + evaluator |
| `corrective_rag` | RAG with document evaluation and web fallback |
| `self_rag` | RAG that checks whether the answer is grounded and regenerates |
| `adaptive_rag` | Router chooses vector store or web |
| `sql_agent` | SQL agent with a read-only guard |
| `map_reduce` | Fan-out with `Send` and reduction |
| `typed_state` | Typed state with a struct (`NewTyped`, reducer tags, `Validate`) |
| `engineering_team` | Engineering team as one graph: handoffs with `Send`/`Command`, atomic reservation of people, per-agent skills in `skills/` |
| `independent_engineering_team` | The same team as separate services: atomic reservation with leases, job queue, RAG/CAG/KAG over MCP, Agent Protocol workers, a model per agent by env |
| `full_team` | `independent_engineering_team` with the compression layer on every worker (caveman, Ponytail, TOON, RTK); `go run ./examples/langgraph/full_team serve` serves it as an MCP server on stdio, and `/present` is a roll call of the whole team |
| `full_team_front` | A copy of `full_team` for a front-end and UX team: the PO reads the named backend through read-only guarded tools, joint specialist and tech-lead review, QA writes spec-traced tests, skills vendored per persona; `go run ./examples/langgraph/full_team_front serve` serves it as an MCP server on stdio, and `/present` is a roll call |

### `examples/deepagents`

| Example | Shows |
|---|---|
| `basic` | Planning + virtual filesystem |
| `research` | Research subagents in parallel, report in a file |
| `text_to_sql` | Plan, schema in a file, validator subagent |
| `content_builder` | Memory (`AGENTS.md`), skills and real files on disk |
| `ralph_mode` | Loop with fresh context per iteration, state only in files |
| `rubric` | Grader with criteria and iterative revision |
| `async_subagents` | Background task (local runner) |
| `async_subagent_server` | HTTP agent server + `RemoteRunner` |
| `mcp_docs_agent` | Tools coming from an MCP server |

### `examples/langmem`

| Example | Shows |
|---|---|
| `hot_path` | Agent with `manage_memory`/`search_memory`, per-user memory |
| `background` | `StoreManager` + `ReflectionExecutor` with debounce |
| `profile_episodic` | Collection, single-document profile and episodes |
| `prompt_optimizer` | Prompt that learns from feedback; several prompts |
| `summarization` | Incremental summary in a graph with a checkpointer |

### `examples/agentprotocol`

| Example | Shows |
|---|---|
| `serve_and_call` | Agent served by Agent Protocol and client: agents, threads, runs, history/fork, SSE, store |
| `human_in_the_loop` | Per-thread streaming: message/tool events, `input.requested` and `input.respond` |

### `examples/ciam`

| Example | Shows |
|---|---|
| `project_context` | Incremental index, hybrid search, compression, ADR/PRD/plan, decision context, memory, MCP |
| `agent_with_context` | Agent that queries the decision context before implementing |

### `examples/openswe`

| Example | Shows |
|---|---|
| `engineer` | Issue → per-thread sandbox → blocked `gh pr create` shortcut → draft PR → comment |
| `reviewer` | Single review with an anchored finding, refused push, re-review with reconciliation |
| `factory` | Webhooks (mention, queue, duplicate, strangers), new PR, cron, CI watcher |
| `slack_trigger` | Slack: `url_verification`, mention in a thread, ignored retry, final answer posted in the thread |

### `examples/knowledge`

| Example | Shows |
|---|---|
| `rag` | Incremental indexing (`Added`/`Skipped`/`Removed`), vector vs hybrid search, answer with citations |
| `cag` | Corpus in a stable prefix, hash and tokens, `cache_hit`, fallback when the corpus does not fit |
| `kag` | Graph extracted from passages, 3-hop question, verification, graph with a cycle |
| `router` | Router choosing KAG, CAG or RAG, `Force` and fallback to RAG |
| `agent_tools` | Agent using the `knowledge.Toolkit` tools |
| `mcp_server` | The same tools as an MCP server (`-demo` shows `tools/list` and `tools/call`) |

### `examples/evals`

| Example | Shows |
|---|---|
| `basic` | `evals.Evaluate` with exact, contains, embedding and LLM-as-judge; text/JSON report |

### `examples/integrations`

| Example | Shows |
|---|---|
| `graph_rag` | Retriever that follows metadata links |
| `agui_server` | Graph served over the AG-UI protocol |
| `permission_rag` | Permission-filtered retriever |
| `remote_sandbox` | `backends.Remote` over any runner |
| `vector_indexes` | `Flat` (exact) and `Annoy` (approximate), recall and persistence |
| `durable_threads` | Thread state in a checkpointer (S3 if `S3_ENDPOINT` is set) |

Official index: [examples/README.md](../../../examples/README.md).

## 15.3 Recipes

### A. Chatbot with persistent memory in SQLite

```go
db, _ := sql.Open("sqlite", "chat.db")
saver, _ := sqlsaver.New(ctx, db, sqlsaver.SQLite)

agent, _ := agents.Create(agents.Options{
	Model:        chatmodels.MustInit("openai:gpt-4o-mini"),
	SystemPrompt: "You are a helpful assistant.",
	Compile:      []graph.CompileOption{graph.WithCheckpointer(saver)},
})

chat := func(user, text string) string {
	ctx := runnables.WithConfig(context.Background(), graph.Thread(user))
	out, err := agent.Invoke(ctx, graph.State{"messages": text})
	if err != nil {
		return "error: " + err.Error()
	}
	msgs := out["messages"].([]messages.Message)
	return msgs[len(msgs)-1].Text()
}
```

### B. Documentation agent with RAG and sources

```go
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4, Type: retrievers.MMR})
agent, _ := agents.Create(agents.Options{
	Model: m,
	Tools: []tools.Tool{retrievers.AsTool(retr, "search_docs", "Searches the documentation. Returns passages with the source.")},
	SystemPrompt: "Answer only based on search_docs. Cite the source (metadata.source). If you do not find it, say you do not know.",
})
```

### C. Batch data extraction

```go
type Invoice struct {
	Number string  `json:"number"`
	Total  float64 `json:"total"`
}
extract := models.Structured[Invoice](m, models.MethodTool)
robust := runnables.WithRetry(extract, runnables.RetryOptions{MaxAttempts: 3})

inputs := make([][]messages.Message, len(texts))
for i, t := range texts {
	inputs[i] = []messages.Message{messages.System("Extract the invoice data."), messages.Human(t)}
}
cfg := runnables.Config{MaxConcurrency: 4}
results, err := runnables.Batch(runnables.WithConfig(ctx, cfg), robust, inputs)
```

### D. Agent with human approval and an HTTP API

Two-call flow: the first returns the approval request, the second resumes.

```go
type reply struct {
	Text     string `json:"text,omitempty"`
	Approval any    `json:"approval,omitempty"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	ctx := runnables.WithConfig(r.Context(), graph.Thread(sessionOf(r)))
	var in any = graph.State{"messages": textOf(r)}
	if decision, ok := decisionOf(r); ok { // the front end sent approve/reject
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

### E. Supervisor with specialists

Each specialist is a compiled agent, called inside a node of the supervisor graph; the router reads `s["next"]` and branches. See `examples/langgraph/multi_agent_supervisor` and `hierarchical_teams`.

### F. Deep research with an evaluated report

```go
agent, _ := deepagents.Create(deepagents.Options{
	Model:   m,
	Tools:   []tools.Tool{search.Tool("web_search", "Searches the web.", search.NewTavily(""), 5)},
	SubAgents: []deepagents.SubAgentSpec{{Name: "researcher", Description: "Researches a subtopic and summarizes it.", SystemPrompt: "..."}},
	Middleware: []agents.Middleware{
		dmw.Rubric{Grader: m, Criteria: []string{"Every claim has a source", "3-line summary at the top"}},
	},
})
```

### G. Agent with tools from an MCP server

```go
ts, closeAll, _ := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"docs": {URL: "https://mcp.example.com/mcp"},
})
defer closeAll()
agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

### H. Questions about documents with the right technique (RAG, CAG or KAG)

A router decides per question; the rest of the application only sees a `knowledge.Answerer`. Details in [chapter 20](20-rag-cag-kag.md).

```go
idx := rag.NewIndexer(store, nil)
idx.Index(ctx, documents.Directory("docs", "*.md", nil))

r := &knowledge.Router{
    RAG:      &rag.Pipeline{Store: store, Docs: idx.Documents(), Model: model, Hybrid: true},
    Profile:  knowledge.Profile{CorpusTokens: 30000, ContextWindow: 200000},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: "How do I reset the Router-X500 password?"})
fmt.Println(ans.Text, ans.Strategy) // answer with [n] and the technique used
```

To expose the same thing to an agent or to an editor, use `knowledge.Toolkit{Router: r}.All()` or `.MCPServer(nil)`.

## 15.4 Equivalence with the Python libraries

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
| `create_memory_store_manager` / `create_manage_memory_tool` | `langmem.NewStoreManager` / `langmem.ManageMemory` (chap. 16) |
| `create_prompt_optimizer` / `summarize_messages` | `langmem.NewPromptOptimizer` / `langmem.SummarizeMessages` |
| Agent Protocol (`/runs`, `/threads`, `/store`, streaming) | `agentprotocol.New` / `agentprotocol.NewClient` (chap. 17) |
| Open SWE (`open_pull_request`, reviewer, baby-sit) | `openswe.NewAgent` / `openswe.ReviewerRunner` / `babysit.Watcher` (chap. 19) |

## 15.5 Glossary

| Term | Meaning |
|---|---|
| **Runnable** | Unit that receives `I` and returns `O`; composable blocks |
| **Chain** | Sequence of runnables (`Pipe`) |
| **State** | `map[string]any` shared by the nodes of a graph |
| **Channel** | A key of the state with its update rule |
| **Reducer** | Function that combines the current value with a new write |
| **Super-step** | Round in which all scheduled nodes run in parallel |
| **Checkpoint** | Snapshot of the state saved after each super-step |
| **Thread** | Timeline of checkpoints identified by `thread_id` |
| **Interrupt** | Pause that returns a value to the caller and waits for an answer |
| **Command** | Return value that updates the state, redirects the flow or resumes a pause |
| **Send** | Triggers a node task with its own input (fan-out) |
| **Store** | Long-term memory shared across threads |
| **Middleware** | Code that fits into points of the agent loop |
| **HITL** | Human-in-the-loop: human approval, editing or rejection |
| **Backend (Deep Agents)** | Where the agent's virtual files live |
| **Subagent** | Isolated agent to which the main agent delegates a task |
| **MCP** | Model Context Protocol: protocol to expose tools and data to LLMs |
| **RAG** | Retrieval-augmented generation over documents |
| **MMR** | Maximal marginal relevance: search with diversity |
| **AG-UI** | Event protocol between agents and user interfaces |
| **SSRF** | Making the server request internal addresses; blocked by the `Fetcher` |

## 15.6 Where to go next

- The source code and tests of each package show the exact usage (`*_test.go`).
- [ARCHITECTURE.md](../ARCHITECTURE.md) explains the design decisions of the runtime.
- [INTEGRATIONS.md](../INTEGRATIONS.md) and [PROVIDERS.md](../PROVIDERS.md) detail coverage and verification.
- [KNOWLEDGE.md](../KNOWLEDGE.md) is the reference for `knowledge`, `rag`, `cag` and `kag`; the other packages have [LANGMEM.md](../LANGMEM.md), [CIAM.md](../CIAM.md), [AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md), [OPENSWE.md](../OPENSWE.md) and [EVALS.md](../EVALS.md).
- The [`docs/` index](../README.md) lists everything with one line per document.

---

Previous: [14. Production, security and troubleshooting](14-producao-seguranca-troubleshooting.md) · Next: [16. Long-term memory](16-memoria-langmem.md) · [Manual index](README.md)
