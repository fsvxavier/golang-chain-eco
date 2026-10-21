# Ejemplos

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Cada ejemplo es un programa independiente: `go run ./examples/<grupo>/<nombre>`.
Sin clave de API usan un modelo falso con respuestas calculadas (salida determinista);
con `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` usan el proveedor real.
Cada carpeta tiene `README.md` (inglés), `README.pt-BR.md` y `README.es-ES.md` que explican qué hace el ejemplo, cómo aplicar el patrón, cómo ejecutarlo y cómo probarlo en modo productivo.
Los grupos `langchain`, `langgraph` y `deepagents` se portaron de los repositorios del mismo nombre; `langmem`, `agentprotocol`, `ciam` y `openswe` se portaron de los proyectos del mismo nombre (LangMem, Agent Protocol, context-ia-manager y Open SWE) e `integrations` son ejemplos propios de este proyecto.

## langchain

| Ejemplo | Muestra | Origen |
|---|---|---|
| `langchain/chain` | `prompt \| modelo \| parser` tipado | LCEL |
| `langchain/rag_chain` | splitter, embeddings, retriever y chain RAG | LCEL / RAG |
| `langchain/middleware_stack` | PII + retry + fallback + límites compuestos | `langchain_v1/agents/middleware` |

## langgraph

| Ejemplo | Muestra | Origen |
|---|---|---|
| `langgraph/react_from_scratch` | bucle ReAct montado a mano con `StateGraph` | `react-agent-from-scratch.ipynb` |
| `langgraph/react_agent_middleware` | `agents.Create` + streaming de updates | `create_agent` |
| `langgraph/structured_output` | agent que termina con una respuesta tipada | `react-agent-structured-output.ipynb` |
| `langgraph/tool_calling` | tools en paralelo, error de tool y tool inexistente | `tool-calling.ipynb` |
| `langgraph/wait_user_input` | `Interrupt` + `Command{Resume}` + checkpoint | `human_in_the_loop/wait-user-input.ipynb` |
| `langgraph/hitl_agent` | aprobación humana de una tool (middleware) | HITL de `create_agent` |
| `langgraph/customer_support` | tools libres + tool sensible con aprobación/rechazo | `customer-support.ipynb` |
| `langgraph/subgraph` | grafo compilado usado como nodo | `subgraph.ipynb` |
| `langgraph/plan_and_execute` | planner, executor y replanner | `plan-and-execute.ipynb` |
| `langgraph/rewoo` | plan completo con placeholders `#E1`, ejecución y solver | `rewoo/rewoo.ipynb` |
| `langgraph/llm_compiler` | DAG de tareas ejecutado en paralelo con `Send` | `llm-compiler/LLMCompiler.ipynb` |
| `langgraph/reflection` | generador y crítico en bucle | `reflection.ipynb` |
| `langgraph/code_assistant` | generar, comprobar, reflexionar y reintentar | `code_assistant/*.ipynb` |
| `langgraph/extraction_retries` | extracción validada con reintento guiado por el error | `extraction/retries.ipynb` |
| `langgraph/info_gather` | chatbot que recoge requisitos y cambia de fase | `chatbots/information-gather-prompting.ipynb` |
| `langgraph/self_discover` | pipeline de 4 prompts (seleccionar, adaptar, estructurar, resolver) | `self-discover/self-discover.ipynb` |
| `langgraph/multi_agent_supervisor` | supervisor que enruta a especialistas | `multi_agent/multi-agent-collaboration.ipynb` |
| `langgraph/hierarchical_teams` | supervisor de supervisores (subgrafos por equipo) | `multi_agent/hierarchical_agent_teams.ipynb` |
| `langgraph/chatbot_simulation` | usuario simulado + evaluador | `chatbot-simulation-evaluation/*.ipynb` |
| `langgraph/corrective_rag` | RAG con evaluación de documentos y fallback web | `rag/langgraph_crag.ipynb` |
| `langgraph/self_rag` | RAG que comprueba que la respuesta esté fundamentada y regenera | `rag/langgraph_self_rag.ipynb` |
| `langgraph/adaptive_rag` | un router elige vector store o web | `rag/langgraph_adaptive_rag.ipynb` |
| `langgraph/sql_agent` | agent de SQL con guarda de solo lectura | `tutorials/sql-agent.ipynb` |
| `langgraph/map_reduce` | fan-out con `Send` y reducción | Send API |
| `langgraph/typed_state` | estado tipado con struct (`NewTyped`, tags de reducer, `Validate`) | propio |
| `langgraph/engineering_team` | equipo de ingeniería (gerente, PO, arquitecto, líder, 4 devs, 2 revisores, 2 QA) con handoff por `Send`/`Command`, registro de disponibilidad con reserva atómica y skills por agente en `skills/` | propio |
| `langgraph/independent_engineering_team` | el mismo equipo como servicios separados: hub (reserva atómica con leases, cola de jobs, RAG/CAG/KAG y memoria del equipo vía MCP), gerente en `remote/server`, workers en Agent Protocol; modelo por agente vía env; Docker compose | propio |
| `langgraph/decider_fallback` | `Decider`, `RuleDecider` y el fallback `Guarded`, sin modelo | propio |
| `langgraph/compress_history_summary` | `Summary` de un historial por encima de 8k tokens, que cabe en el presupuesto de 700 tokens | propio |
| `langgraph/compress_shell_rtk` | salida de shell por RTK cuando el decider lo decide | propio |
| `langgraph/compress_prompt_caveman` | compresión de prompt y de respuesta (Caveman y la directiva de respuesta breve) | propio |
| `langgraph/compress_toon` | resultado JSON de herramienta en TOON, tamaños antes y después, fallback `TOONShorter` | propio |
| `langgraph/compress_ponytail` | nota Ponytail por nivel tal como la recibe el modelo; system prompt intacto | propio |
| `langgraph/decider_laya_http` | `HTTPDecider` contra `laya-serve` (servidor falso offline) | propio |
| `langgraph/decider_laya_native` | decider laya-go nativo, módulo separado, tag `laya_native` | propio |
| `langgraph/full_team` | el equipo independiente con compresión de llamadas; decider y modelo falsos por defecto | propio |

## deepagents

| Ejemplo | Muestra | Origen |
|---|---|---|
| `deepagents/basic` | planificación + filesystem virtual | `deepagents` |
| `deepagents/research` | subagents de investigación en paralelo, informe en un archivo | `examples/deep_research` |
| `deepagents/text_to_sql` | plan, schema en archivo, subagent validador | `examples/text-to-sql-agent` |
| `deepagents/content_builder` | memoria (`AGENTS.md`), skills y archivos reales en disco | `examples/content-builder-agent` |
| `deepagents/ralph_mode` | bucle con contexto nuevo por iteración, estado solo en archivos | `examples/ralph_mode` |
| `deepagents/rubric` | grader con criterios y revisión iterativa | `examples/rubric_middleware` |
| `deepagents/async_subagents` | tarea en segundo plano (runner local) | `examples/async-subagent-server` |
| `deepagents/async_subagent_server` | servidor de agents HTTP + `RemoteRunner` | `examples/async-subagent-server` |
| `deepagents/mcp_docs_agent` | tools provenientes de un servidor MCP | `examples/deploy-mcp-docs-agent` |

## langmem

Memoria de largo plazo de [LangMem](https://github.com/langchain-ai/langmem) integrada al store y a `agents.Create` (paquete [`langmem`](../langmem), referencia en [docs/es-ES/LANGMEM.md](../docs/es-ES/LANGMEM.md)). Todos corren offline con un modelo falso que habla el protocolo de tool calls del extractor.

| Ejemplo | Muestra | Origen |
|---|---|---|
| `langmem/hot_path` | agent con `manage_memory` y `search_memory`, memoria aislada por usuario, búsqueda semántica | `hot_path_quickstart` |
| `langmem/background` | `StoreManager` + `ReflectionExecutor`: 3 turnos rápidos generan 1 extracción; búsqueda en las memorias | `background_quickstart`, `delayed_processing` |
| `langmem/profile_episodic` | colección (insertar/actualizar/borrar), perfil de un documento con semilla, episodios | `extract_semantic_memories`, `manage_user_profile`, `extract_episodic_memories` |
| `langmem/prompt_optimizer` | prompt que aprende del feedback (metaprompt) y optimizador de varios prompts | `optimize_memory_prompt`, `optimize_compound_system` |
| `langmem/summarization` | resumen incremental en un grafo con checkpointer; historial completo, el modelo ve el resumido | `summarization` |

## agentprotocol

Servir agents con el [Agent Protocol](https://github.com/langchain-ai/agent-protocol) (paquete [`agentprotocol`](../agentprotocol), referencia en [docs/es-ES/AGENT-PROTOCOL.md](../docs/es-ES/AGENT-PROTOCOL.md)). Corren offline (servidor `httptest` en proceso).

| Ejemplo | Muestra | Origen |
|---|---|---|
| `agentprotocol/serve_and_call` | descubrimiento de agents, threads multi-turno, runs (wait/stream), historial, fork, copia, store | OpenAPI del protocolo |
| `agentprotocol/human_in_the_loop` | streaming por thread: canales `lifecycle`/`messages`/`tools`/`input`, `run.start`, `input.respond`, `state.*` | Agent Streaming Protocol |

## ciam

Contexto quirúrgico para asistentes de IA (paquete [`ciam`](../ciam), referencia en [docs/es-ES/CIAM.md](../docs/es-ES/CIAM.md)). Corren offline con embeddings por hashing; con Ollama, usa `providers/ollama.Embeddings`.

| Ejemplo | Muestra | Origen |
|---|---|---|
| `ciam/project_context` | índice incremental, búsqueda híbrida por tipo, compresión, ADR/PRD/plan (supersede), contexto de decisión, memoria, servidor MCP | context-ia-manager |
| `ciam/agent_with_context` | agent que descubre una decisión (ADR) a través del contexto antes de implementar | context-ia-manager |

## openswe

Fábrica de software con agents (paquete [`openswe`](../openswe), referencia en [docs/es-ES/OPENSWE.md](../docs/es-ES/OPENSWE.md)). Usan GitHub, Slack y Linear simulados ([`internal/fakegithub`](internal/fakegithub), [`internal/fakeslack`](internal/fakeslack), [`internal/fakelinear`](internal/fakelinear)), modelo guionizado y git/sandbox reales en directorios temporales.

| Ejemplo | Muestra | Origen |
|---|---|---|
| `openswe/engineer` | issue → sandbox por thread → atajo `gh pr create` bloqueado → PR en borrador → comentario y vínculo PR↔thread | open-swe |
| `openswe/reviewer` | review única con finding anclado al diff, push rechazado (solo lectura), re-revisión que reconcilia la respuesta humana | open-swe |
| `openswe/factory` | webhook (mención, cola de follow-ups, duplicado, extraño en repo público), PR nuevo → revisión, cron, vigilante de CI sin desperdicio | open-swe |
| `openswe/slack_trigger` | Slack: `url_verification`, mención en un thread, retry ignorado, respuesta final publicada en el thread | open-swe |

## evals

| Ejemplo | Muestra | Origen |
|---|---|---|
| `evals/basic` | `evals.Evaluate` con exacto, contains, embedding y LLM-as-judge; informe texto/JSON | propio (evals offline de LangSmith) |

## integrations

Ejemplos de las integraciones (todos corren offline; el último usa S3 si `S3_ENDPOINT` está definido).

| Ejemplo | Muestra |
|---|---|
| `integrations/graph_rag` | retriever que sigue enlaces de metadatos: similitud pura vs recorrido de 2 saltos |
| `integrations/agui_server` | grafo servido por el protocolo AG-UI (SSE) y llamado en proceso, con los eventos impresos |
| `integrations/permission_rag` | retriever filtrado por permisos (Permit; SpiceDB tiene la misma forma), cada usuario ve lo que puede |
| `integrations/remote_sandbox` | `backends.Remote`: archivos y shell sobre cualquier runner (aquí bash local; en producción un pod) |
| `integrations/vector_indexes` | índices `Flat` (exacto) y `Annoy` (aproximado): recall vs `SearchK`, guardar y cargar |
| `integrations/durable_threads` | estado de un thread en un checkpointer (S3 si está configurado, memoria si no) |

## knowledge

RAG, CAG y KAG sobre el contrato común `knowledge.Answerer` (todos corren offline con modelo falso).

| Ejemplo | Muestra |
|---|---|
| `knowledge/rag` | indexación incremental (`Added`/`Skipped`/`Removed`), búsqueda vectorial vs híbrida (término exacto), respuesta con citas |
| `knowledge/cag` | corpus en prefijo estable, hash y tokens, `cache_hit` en la segunda pregunta, fallback cuando el corpus no cabe |
| `knowledge/kag` | grafo extraído de fragmentos, pregunta de 3 saltos que la búsqueda vectorial no responde, verificación, grafo con ciclo |
| `knowledge/agent_tools` | agent (`agents.Create`) usando las tools de `knowledge.Toolkit`; el modelo llama a `rag_answer` y cita las fuentes |
| `knowledge/mcp_server` | las mismas tools como servidor MCP; con `-demo` muestra `tools/list` y `tools/call` en proceso, sin `-demo` sirve stdio |
| `knowledge/router` | router que elige KAG, CAG o RAG según la pregunta y el perfil, `Force` y fallback a RAG |

## No portados

`web-navigation` (necesita navegador), `lats` (búsqueda en árbol con evaluación por rollouts), `reflexion`, los ejemplos `deploy-*`
(configuración de la plataforma de despliegue), `llm-wiki`, `nvidia_deep_agent`, `better-harness`, `talon`, `downloading_agents` y
`delta-channel-dump` (específicos del ecosistema Python/plataforma). Los bloques de construcción que usan ya existen aquí.
