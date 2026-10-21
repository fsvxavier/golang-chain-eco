# Examples

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Each example is an independent program: `go run ./examples/<group>/<name>`.
Without an API key they use a fake model with computed replies (deterministic output);
with `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` they use the real provider.
Each folder has `README.md` (English), `README.pt-BR.md` and `README.es-ES.md` explaining what the example does, how to apply the pattern, how to run it and how to test it in production mode.
The `langchain`, `langgraph` and `deepagents` groups were ported from the repositories of the same name; `langmem`, `agentprotocol`, `ciam` and `openswe` were ported from the projects of the same name (LangMem, Agent Protocol, context-ia-manager and Open SWE) and `integrations` are this project's own examples.

## langchain

| Example | Shows | Origin |
|---|---|---|
| `langchain/chain` | typed `prompt \| model \| parser` | LCEL |
| `langchain/rag_chain` | splitter, embeddings, retriever and RAG chain | LCEL / RAG |
| `langchain/middleware_stack` | PII + retry + fallback + limits composed | `langchain_v1/agents/middleware` |

## langgraph

| Example | Shows | Origin |
|---|---|---|
| `langgraph/react_from_scratch` | ReAct loop wired by hand with `StateGraph` | `react-agent-from-scratch.ipynb` |
| `langgraph/react_agent_middleware` | `agents.Create` + streaming updates | `create_agent` |
| `langgraph/structured_output` | agent that finishes with a typed answer | `react-agent-structured-output.ipynb` |
| `langgraph/tool_calling` | parallel tools, tool error and unknown tool | `tool-calling.ipynb` |
| `langgraph/wait_user_input` | `Interrupt` + `Command{Resume}` + checkpoint | `human_in_the_loop/wait-user-input.ipynb` |
| `langgraph/hitl_agent` | human approval of a tool (middleware) | `create_agent` HITL |
| `langgraph/customer_support` | free tools + a sensitive tool with approval/rejection | `customer-support.ipynb` |
| `langgraph/subgraph` | compiled graph used as a node | `subgraph.ipynb` |
| `langgraph/plan_and_execute` | planner, executor and replanner | `plan-and-execute.ipynb` |
| `langgraph/rewoo` | full plan with `#E1` placeholders, execution and solver | `rewoo/rewoo.ipynb` |
| `langgraph/llm_compiler` | task DAG executed in parallel with `Send` | `llm-compiler/LLMCompiler.ipynb` |
| `langgraph/reflection` | generator and critic in a loop | `reflection.ipynb` |
| `langgraph/code_assistant` | generate, check, reflect and retry | `code_assistant/*.ipynb` |
| `langgraph/extraction_retries` | validated extraction with error-guided retry | `extraction/retries.ipynb` |
| `langgraph/info_gather` | chatbot that collects requirements and changes phase | `chatbots/information-gather-prompting.ipynb` |
| `langgraph/self_discover` | 4-prompt pipeline (select, adapt, structure, solve) | `self-discover/self-discover.ipynb` |
| `langgraph/multi_agent_supervisor` | supervisor routing to specialists | `multi_agent/multi-agent-collaboration.ipynb` |
| `langgraph/hierarchical_teams` | supervisor of supervisors (subgraphs per team) | `multi_agent/hierarchical_agent_teams.ipynb` |
| `langgraph/chatbot_simulation` | simulated user + evaluator | `chatbot-simulation-evaluation/*.ipynb` |
| `langgraph/corrective_rag` | RAG with document grading and web fallback | `rag/langgraph_crag.ipynb` |
| `langgraph/self_rag` | RAG that checks the answer is grounded and regenerates | `rag/langgraph_self_rag.ipynb` |
| `langgraph/adaptive_rag` | router chooses vector store or web | `rag/langgraph_adaptive_rag.ipynb` |
| `langgraph/sql_agent` | SQL agent with a read-only guard | `tutorials/sql-agent.ipynb` |
| `langgraph/map_reduce` | fan-out with `Send` and reduction | Send API |
| `langgraph/typed_state` | typed state with a struct (`NewTyped`, reducer tags, `Validate`) | own |
| `langgraph/engineering_team` | engineering team (manager, PO, architect, leader, 4 devs, 2 reviewers, 2 QA) with `Send`/`Command` handoff, availability registry with atomic reservation and per-agent skills in `skills/` | own |
| `langgraph/independent_engineering_team` | the same team as separate services: hub (atomic reservation with leases, job queue, RAG/CAG/KAG and team memory over MCP), manager on `remote/server`, workers on Agent Protocol; model per agent by env; Docker compose | own |
| `langgraph/decider_fallback` | `Decider`, `RuleDecider` and the `Guarded` fallback, no model | own |
| `langgraph/compress_history_summary` | `Summary` of a history above 8k tokens, fitting the 700-token budget | own |
| `langgraph/compress_shell_rtk` | shell output through RTK when the decider says so | own |
| `langgraph/compress_prompt_caveman` | prompt and reply compression (Caveman and the terse-reply directive) | own |
| `langgraph/compress_toon` | JSON tool result as TOON, sizes before and after, `TOONShorter` fallback | own |
| `langgraph/compress_ponytail` | Ponytail note per level as the model receives it; system prompt untouched | own |
| `langgraph/decider_laya_http` | `HTTPDecider` against `laya-serve` (fake server offline) | own |
| `langgraph/decider_laya_native` | native laya-go decider, separate module, `laya_native` tag | own |
| `langgraph/full_team` | the independent team with call compression; fake decider and model by default | own |

## deepagents

| Example | Shows | Origin |
|---|---|---|
| `deepagents/basic` | planning + virtual filesystem | `deepagents` |
| `deepagents/research` | parallel research subagents, report in a file | `examples/deep_research` |
| `deepagents/text_to_sql` | plan, schema in a file, validator subagent | `examples/text-to-sql-agent` |
| `deepagents/content_builder` | memory (`AGENTS.md`), skills and real files on disk | `examples/content-builder-agent` |
| `deepagents/ralph_mode` | loop with fresh context per iteration, state only in files | `examples/ralph_mode` |
| `deepagents/rubric` | grader with criteria and iterative revision | `examples/rubric_middleware` |
| `deepagents/async_subagents` | background task (local runner) | `examples/async-subagent-server` |
| `deepagents/async_subagent_server` | HTTP agent server + `RemoteRunner` | `examples/async-subagent-server` |
| `deepagents/mcp_docs_agent` | tools coming from an MCP server | `examples/deploy-mcp-docs-agent` |

## langmem

Long-term memory from [LangMem](https://github.com/langchain-ai/langmem) integrated with the store and `agents.Create` (package [`langmem`](../langmem), reference in [docs/en-US/LANGMEM.md](../docs/en-US/LANGMEM.md)). All run offline with a fake model that speaks the extractor's tool-call protocol.

| Example | Shows | Origin |
|---|---|---|
| `langmem/hot_path` | agent with `manage_memory` and `search_memory`, per-user isolated memory, semantic search | `hot_path_quickstart` |
| `langmem/background` | `StoreManager` + `ReflectionExecutor`: 3 quick turns produce 1 extraction; search the memories | `background_quickstart`, `delayed_processing` |
| `langmem/profile_episodic` | collection (insert/update/delete), single-document profile with seed, episodes | `extract_semantic_memories`, `manage_user_profile`, `extract_episodic_memories` |
| `langmem/prompt_optimizer` | prompt that learns from feedback (metaprompt) and a multi-prompt optimizer | `optimize_memory_prompt`, `optimize_compound_system` |
| `langmem/summarization` | incremental summary in a graph with a checkpointer; full history, the model sees the summarized one | `summarization` |

## agentprotocol

Serving agents through the [Agent Protocol](https://github.com/langchain-ai/agent-protocol) (package [`agentprotocol`](../agentprotocol), reference in [docs/en-US/AGENT-PROTOCOL.md](../docs/en-US/AGENT-PROTOCOL.md)). They run offline (in-process `httptest` server).

| Example | Shows | Origin |
|---|---|---|
| `agentprotocol/serve_and_call` | agent discovery, multi-turn threads, runs (wait/stream), history, fork, copy, store | protocol OpenAPI |
| `agentprotocol/human_in_the_loop` | per-thread streaming: `lifecycle`/`messages`/`tools`/`input` channels, `run.start`, `input.respond`, `state.*` | Agent Streaming Protocol |

## ciam

Surgical context for AI assistants (package [`ciam`](../ciam), reference in [docs/en-US/CIAM.md](../docs/en-US/CIAM.md)). They run offline with hashing embeddings; with Ollama, use `providers/ollama.Embeddings`.

| Example | Shows | Origin |
|---|---|---|
| `ciam/project_context` | incremental index, hybrid search by type, compression, ADR/PRD/plan (supersede), decision context, memory, MCP server | context-ia-manager |
| `ciam/agent_with_context` | agent that finds a decision (ADR) through the context before implementing | context-ia-manager |

## openswe

Software factory with agents (package [`openswe`](../openswe), reference in [docs/en-US/OPENSWE.md](../docs/en-US/OPENSWE.md)). They use simulated GitHub, Slack and Linear ([`internal/fakegithub`](internal/fakegithub), [`internal/fakeslack`](internal/fakeslack), [`internal/fakelinear`](internal/fakelinear)), a scripted model and real git/sandbox in temporary directories.

| Example | Shows | Origin |
|---|---|---|
| `openswe/engineer` | issue → per-thread sandbox → `gh pr create` shortcut blocked → draft PR → comment and PR↔thread link | open-swe |
| `openswe/reviewer` | single review with a finding anchored to the diff, push refused (read-only), re-review that reconciles the human reply | open-swe |
| `openswe/factory` | webhook (mention, follow-up queue, duplicate, stranger on a public repo), new PR → review, cron, CI watcher without waste | open-swe |
| `openswe/slack_trigger` | Slack: `url_verification`, mention in a thread, retry ignored, final answer posted in the thread | open-swe |

## evals

| Example | Shows | Origin |
|---|---|---|
| `evals/basic` | `evals.Evaluate` with exact, contains, embedding and LLM-as-judge; text/JSON report | own (LangSmith offline evals) |

## integrations

Examples of the integrations (all run offline; the last one uses S3 if `S3_ENDPOINT` is set).

| Example | Shows |
|---|---|
| `integrations/graph_rag` | retriever that follows metadata links: plain similarity vs 2-hop traversal |
| `integrations/agui_server` | graph served over the AG-UI protocol (SSE) and called in-process, with the events printed |
| `integrations/permission_rag` | permission-filtered retriever (Permit; SpiceDB has the same shape), each user sees what they may |
| `integrations/remote_sandbox` | `backends.Remote`: files and shell over any runner (local bash here; a pod in production) |
| `integrations/vector_indexes` | `Flat` (exact) and `Annoy` (approximate) indexes: recall vs `SearchK`, save and load |
| `integrations/durable_threads` | a thread's state in a checkpointer (S3 if configured, memory if not) |

## knowledge

RAG, CAG and KAG over the common `knowledge.Answerer` contract (all run offline with a fake model).

| Example | Shows |
|---|---|
| `knowledge/rag` | incremental indexing (`Added`/`Skipped`/`Removed`), vector vs hybrid search (exact term), answer with citations |
| `knowledge/cag` | corpus in a stable prefix, hash and tokens, `cache_hit` on the second question, fallback when the corpus does not fit |
| `knowledge/kag` | graph extracted from passages, 3-hop question that vector search cannot answer, verification, graph with a cycle |
| `knowledge/agent_tools` | agent (`agents.Create`) using the `knowledge.Toolkit` tools; the model calls `rag_answer` and cites the sources |
| `knowledge/mcp_server` | the same tools as an MCP server; with `-demo` it shows `tools/list` and `tools/call` in-process, without `-demo` it serves stdio |
| `knowledge/router` | router choosing KAG, CAG or RAG by question and profile, `Force` and fallback to RAG |

## Not ported

`web-navigation` (needs a browser), `lats` (tree search with rollout evaluation), `reflexion`, the `deploy-*` examples
(deployment platform configuration), `llm-wiki`, `nvidia_deep_agent`, `better-harness`, `talon`, `downloading_agents` and
`delta-channel-dump` (specific to the Python ecosystem/platform). The building blocks they use already exist here.
