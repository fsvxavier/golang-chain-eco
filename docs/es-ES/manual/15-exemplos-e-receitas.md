# 15. Ejemplos y recetas

[English](../../en-US/manual/15-exemplos-e-receitas.md) | [Português](../../pt-BR/manual/15-exemplos-e-receitas.md) | **Español**

## 15.1 Cómo ejecutar los ejemplos

Cada ejemplo es un programa independiente. Sin clave de API usan un modelo fake determinista; con `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` usan el proveedor real.

```bash
go run ./examples/langchain/chain
go run ./examples/langgraph/react_agent_middleware
go run ./examples/langgraph/hitl_agent
go run ./examples/deepagents/research
go run ./examples/integrations/graph_rag

ANTHROPIC_API_KEY=... go run ./examples/langgraph/react_from_scratch   # con modelo real
```

## 15.2 Catálogo (64 ejemplos)

Cada carpeta de ejemplo tiene `README.md` (inglés), `README.pt-BR.md` y `README.es-ES.md` que explican qué hace, cómo aplicar el patrón, cómo ejecutarlo y cómo probarlo en modo productivo.

### `examples/langchain`

| Ejemplo | Muestra |
|---|---|
| `chain` | `prompt \| modelo \| parser` tipado |
| `rag_chain` | splitter, embeddings, retriever y chain RAG |
| `middleware_stack` | PII + retry + fallback + límites compuestos |

### `examples/langgraph`

| Ejemplo | Muestra |
|---|---|
| `react_from_scratch` | Loop ReAct armado a mano con `StateGraph` |
| `react_agent_middleware` | `agents.Create` + streaming de updates |
| `structured_output` | Agent que termina con una respuesta tipada |
| `tool_calling` | Tools en paralelo, error de tool y tool inexistente |
| `wait_user_input` | `Interrupt` + `Command{Resume}` + checkpoint |
| `hitl_agent` | Aprobación humana de una tool (middleware) |
| `customer_support` | Tools libres + tool sensible con aprobación/rechazo |
| `subgraph` | Grafo compilado usado como nodo |
| `plan_and_execute` | Planner, executor y replanner |
| `rewoo` | Plan completo con placeholders `#E1`, ejecución y solver |
| `llm_compiler` | DAG de tareas ejecutado en paralelo con `Send` |
| `reflection` | Generador y crítico en loop |
| `code_assistant` | Generar, revisar, reflexionar e intentar de nuevo |
| `extraction_retries` | Extracción validada con retry guiado por el error |
| `info_gather` | Chatbot que recoge requisitos y cambia de fase |
| `self_discover` | Pipeline de 4 prompts |
| `multi_agent_supervisor` | Supervisor enrutando a especialistas |
| `hierarchical_teams` | Supervisor de supervisores (subgrafos por equipo) |
| `chatbot_simulation` | Usuario simulado + evaluador |
| `corrective_rag` | RAG con evaluación de documentos y fallback web |
| `self_rag` | RAG que comprueba si la respuesta está fundamentada y regenera |
| `adaptive_rag` | El router elige vector store o web |
| `sql_agent` | Agent de SQL con guarda de solo lectura |
| `map_reduce` | Fan-out con `Send` y reducción |
| `typed_state` | Estado tipado con struct (`NewTyped`, tags de reducer, `Validate`) |
| `engineering_team` | Equipo de ingeniería en un grafo: handoffs con `Send`/`Command`, reserva atómica de personas, skills por agente en `skills/` |

### `examples/deepagents`

| Ejemplo | Muestra |
|---|---|
| `basic` | Planificación + filesystem virtual |
| `research` | Subagents de investigación en paralelo, informe en archivo |
| `text_to_sql` | Plan, schema en archivo, subagent validador |
| `content_builder` | Memoria (`AGENTS.md`), skills y archivos reales en disco |
| `ralph_mode` | Loop con contexto nuevo por iteración, estado solo en archivos |
| `rubric` | Evaluador con criterios y revisión iterativa |
| `async_subagents` | Tarea en background (runner local) |
| `async_subagent_server` | Servidor de agents HTTP + `RemoteRunner` |
| `mcp_docs_agent` | Tools provenientes de un servidor MCP |

### `examples/langmem`

| Ejemplo | Muestra |
|---|---|
| `hot_path` | Agent con `manage_memory`/`search_memory`, memoria por usuario |
| `background` | `StoreManager` + `ReflectionExecutor` con debounce |
| `profile_episodic` | Colección, perfil de un documento y episodios |
| `prompt_optimizer` | Prompt que aprende con feedback; varios prompts |
| `summarization` | Resumen incremental en un grafo con checkpointer |

### `examples/agentprotocol`

| Ejemplo | Muestra |
|---|---|
| `serve_and_call` | Agent servido por Agent Protocol y cliente: agents, threads, runs, historial/fork, SSE, store |
| `human_in_the_loop` | Streaming por thread: eventos de mensajes/herramientas, `input.requested` e `input.respond` |

### `examples/ciam`

| Ejemplo | Muestra |
|---|---|
| `project_context` | Índice incremental, búsqueda híbrida, compresión, ADR/PRD/plan, contexto de decisión, memoria, MCP |
| `agent_with_context` | Agent que consulta el contexto de decisión antes de implementar |

### `examples/openswe`

| Ejemplo | Muestra |
|---|---|
| `engineer` | Issue → sandbox por thread → atajo `gh pr create` bloqueado → PR en borrador → comentario |
| `reviewer` | Revisión única con finding anclado, push rechazado, re-revisión con reconciliación |
| `factory` | Webhooks (mención, cola, duplicado, ajenos), PR nuevo, cron, vigilante de CI |
| `slack_trigger` | Slack: `url_verification`, mención en un thread, retry ignorado, respuesta final publicada en el thread |

### `examples/knowledge`

| Ejemplo | Muestra |
|---|---|
| `rag` | Indexación incremental (`Added`/`Skipped`/`Removed`), búsqueda vectorial vs híbrida, respuesta con citas |
| `cag` | Corpus en prefijo estable, hash y tokens, `cache_hit`, fallback cuando el corpus no cabe |
| `kag` | Grafo extraído de fragmentos, pregunta de 3 saltos, verificación, grafo con ciclo |
| `router` | Router que elige KAG, CAG o RAG, `Force` y fallback a RAG |
| `agent_tools` | Agent que usa las tools de `knowledge.Toolkit` |
| `mcp_server` | Las mismas tools como servidor MCP (`-demo` muestra `tools/list` y `tools/call`) |

### `examples/evals`

| Ejemplo | Muestra |
|---|---|
| `basic` | `evals.Evaluate` con exacto, contains, embedding y LLM-as-judge; informe en texto/JSON |

### `examples/integrations`

| Ejemplo | Muestra |
|---|---|
| `graph_rag` | Retriever que sigue enlaces de metadatos |
| `agui_server` | Grafo servido por el protocolo AG-UI |
| `permission_rag` | Retriever filtrado por permiso |
| `remote_sandbox` | `backends.Remote` sobre un runner cualquiera |
| `vector_indexes` | `Flat` (exacto) y `Annoy` (aproximado), recall y persistencia |
| `durable_threads` | Estado de thread en un checkpointer (S3 si `S3_ENDPOINT` está definido) |

Índice oficial: [examples/README.es-ES.md](../../../examples/README.es-ES.md).

## 15.3 Recetas

### A. Chatbot con memoria persistente en SQLite

```go
db, _ := sql.Open("sqlite", "chat.db")
saver, _ := sqlsaver.New(ctx, db, sqlsaver.SQLite)

agent, _ := agents.Create(agents.Options{
	Model:        chatmodels.MustInit("openai:gpt-4o-mini"),
	SystemPrompt: "Eres un asistente útil.",
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

### B. Agent de documentación con RAG y fuentes

```go
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4, Type: retrievers.MMR})
agent, _ := agents.Create(agents.Options{
	Model: m,
	Tools: []tools.Tool{retrievers.AsTool(retr, "search_docs", "Busca en la documentación. Devuelve fragmentos con la fuente.")},
	SystemPrompt: "Responde solo con base en search_docs. Cita la fuente (metadata.source). Si no encuentras nada, di que no lo sabes.",
})
```

### C. Extracción de datos en lote

```go
type Invoice struct {
	Number string  `json:"number"`
	Total  float64 `json:"total"`
}
extract := models.Structured[Invoice](m, models.MethodTool)
robust := runnables.WithRetry(extract, runnables.RetryOptions{MaxAttempts: 3})

inputs := make([][]messages.Message, len(texts))
for i, t := range texts {
	inputs[i] = []messages.Message{messages.System("Extrae los datos de la factura."), messages.Human(t)}
}
cfg := runnables.Config{MaxConcurrency: 4}
results, err := runnables.Batch(runnables.WithConfig(ctx, cfg), robust, inputs)
```

### D. Agent con aprobación humana y API HTTP

Flujo de dos llamadas: la primera devuelve la solicitud de aprobación, la segunda reanuda.

```go
type reply struct {
	Text     string `json:"text,omitempty"`
	Approval any    `json:"approval,omitempty"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	ctx := runnables.WithConfig(r.Context(), graph.Thread(sessionOf(r)))
	var in any = graph.State{"messages": textOf(r)}
	if decision, ok := decisionOf(r); ok { // el front envió approve/reject
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

### E. Supervisor con especialistas

Cada especialista es un agent compilado, llamado dentro de un nodo del grafo supervisor; el router lee `s["next"]` y desvía. Mira `examples/langgraph/multi_agent_supervisor` y `hierarchical_teams`.

### F. Investigación profunda con informe evaluado

```go
agent, _ := deepagents.Create(deepagents.Options{
	Model:   m,
	Tools:   []tools.Tool{search.Tool("web_search", "Busca en la web.", search.NewTavily(""), 5)},
	SubAgents: []deepagents.SubAgentSpec{{Name: "investigador", Description: "Investiga un subtema y lo resume.", SystemPrompt: "..."}},
	Middleware: []agents.Middleware{
		dmw.Rubric{Grader: m, Criteria: []string{"Cada afirmación tiene fuente", "Resumen de 3 líneas al inicio"}},
	},
})
```

### G. Agent con herramientas de un servidor MCP

```go
ts, closeAll, _ := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"docs": {URL: "https://mcp.ejemplo.com/mcp"},
})
defer closeAll()
agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

### H. Preguntas sobre documentos con la técnica adecuada (RAG, CAG o KAG)

Un router decide por pregunta; el resto de la aplicación solo ve un `knowledge.Answerer`. Detalles en el [capítulo 20](20-rag-cag-kag.md).

```go
idx := rag.NewIndexer(store, nil)
idx.Index(ctx, documents.Directory("docs", "*.md", nil))

r := &knowledge.Router{
    RAG:      &rag.Pipeline{Store: store, Docs: idx.Documents(), Model: model, Hybrid: true},
    Profile:  knowledge.Profile{CorpusTokens: 30000, ContextWindow: 200000},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: "¿Cómo restablezco la contraseña del Router-X500?"})
fmt.Println(ans.Text, ans.Strategy) // respuesta con [n] y la técnica usada
```

Para exponer lo mismo a un agent o a un editor, usa `knowledge.Toolkit{Router: r}.All()` o `.MCPServer(nil)`.

## 15.4 Equivalencia con las bibliotecas de Python

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
| `create_memory_store_manager` / `create_manage_memory_tool` | `langmem.NewStoreManager` / `langmem.ManageMemory` (cap. 16) |
| `create_prompt_optimizer` / `summarize_messages` | `langmem.NewPromptOptimizer` / `langmem.SummarizeMessages` |
| Agent Protocol (`/runs`, `/threads`, `/store`, streaming) | `agentprotocol.New` / `agentprotocol.NewClient` (cap. 17) |
| Open SWE (`open_pull_request`, reviewer, baby-sit) | `openswe.NewAgent` / `openswe.ReviewerRunner` / `babysit.Watcher` (cap. 19) |

## 15.5 Glosario

| Término | Significado |
|---|---|
| **Runnable** | Unidad que recibe `I` y devuelve `O`; bloques componibles |
| **Chain** | Secuencia de runnables (`Pipe`) |
| **Estado (State)** | `map[string]any` compartido por los nodos de un grafo |
| **Canal (channel)** | Una clave del estado con su regla de actualización |
| **Reducer** | Función que combina el valor actual con una escritura nueva |
| **Super-paso** | Ronda en la que todos los nodos agendados corren en paralelo |
| **Checkpoint** | Foto del estado guardada tras cada super-paso |
| **Thread** | Línea de tiempo de checkpoints identificada por `thread_id` |
| **Interrupt** | Pausa que devuelve un valor al llamador y espera una respuesta |
| **Command** | Retorno que actualiza el estado, desvía el flujo o reanuda una pausa |
| **Send** | Dispara una tarea de nodo con entrada propia (fan-out) |
| **Store** | Memoria a largo plazo compartida entre threads |
| **Middleware** | Código que se engancha en puntos del loop del agent |
| **HITL** | Human-in-the-loop: aprobación, edición o rechazo humano |
| **Backend (Deep Agents)** | Dónde viven los archivos virtuales del agent |
| **Subagent** | Agent aislado al que el principal delega una tarea |
| **MCP** | Model Context Protocol: protocolo para exponer tools y datos a LLMs |
| **RAG** | Generación aumentada por recuperación de documentos |
| **MMR** | Relevancia marginal máxima: búsqueda con diversidad |
| **AG-UI** | Protocolo de eventos entre agents e interfaces de usuario |
| **SSRF** | Hacer que el servidor pida direcciones internas; bloqueado por el `Fetcher` |

## 15.6 Dónde seguir

- El código fuente y las pruebas de cada paquete muestran el uso exacto (`*_test.go`).
- [ARCHITECTURE.md](../ARCHITECTURE.md) explica las decisiones de diseño del runtime.
- [INTEGRATIONS.md](../INTEGRATIONS.md) y [PROVIDERS.md](../PROVIDERS.md) detallan cobertura y verificación.
- [KNOWLEDGE.md](../KNOWLEDGE.md) es la referencia de `knowledge`, `rag`, `cag` y `kag`; los demás paquetes tienen [LANGMEM.md](../LANGMEM.md), [CIAM.md](../CIAM.md), [AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md), [OPENSWE.md](../OPENSWE.md) y [EVALS.md](../EVALS.md).
- El [índice de `docs/`](../README.es-ES.md) lista todo con una línea por documento.

---

Anterior: [14. Producción, seguridad y troubleshooting](14-producao-seguranca-troubleshooting.md) · Siguiente: [16. Memoria a largo plazo](16-memoria-langmem.md) · [Índice del manual](README.es-ES.md)
