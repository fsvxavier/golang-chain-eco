# Usage manual — golang-chain-eco

**English** | [Português](../../pt-BR/manual/README.pt-BR.md) | [Español](../../es-ES/manual/README.es-ES.md)

Complete manual for using `golang-chain-eco`: a Go port of the LangChain ecosystem (langchain-core, LangGraph, `create_agent` with middleware, Deep Agents, MCP client, RemoteGraph and tracing).

Audience: Go developers who want to build chains, agents and workflows with LLMs. Knowledge of the Python version is not required.

## How to read

- First time: read chapters 1 and 2 and run the example from chapter 1.
- Just want a working agent: chapters 3, 5 and 9.
- Want RAG: chapters 3, 4 and 6.
- Want durable flows with human approval: chapters 7 and 8.
- Going to production: chapters 12 and 14.
- Looking for ready-made code: chapter 15.
- Agent that remembers across conversations and learns from feedback: chapter 16.
- Exposing your agents through an open API (UI, other services): chapter 17.
- Giving cheap and precise project context to AI assistants / editors: chapter 18.
- Agent that opens PRs, reviews code, watches CI and responds on GitHub: chapter 19.
- Giving the model knowledge with RAG, CAG (prompt cache) or KAG (knowledge graph), and letting a router choose: chapter 20.

## Index

| # | Chapter | Content |
|---|---|---|
| 1 | [Introduction and installation](01-introducao-e-instalacao.md) | Requirements, installation, first agent, environment variables, running offline |
| 2 | [Core concepts](02-conceitos.md) | Layers, `Runnable`, messages, state, channels, context and configuration |
| 3 | [Models and providers](03-modelos-e-provedores.md) | `chatmodels.Init`, native and compatible providers, router across gateways, embeddings, cache, rate limit, fake model |
| 4 | [Prompts, chains and parsers](04-chains-prompts-parsers.md) | Templates, `Pipe`, combinators, retry, fallbacks, structured output, chat history |
| 5 | [Tools](05-tools.md) | Typed tools, schema, errors, terminal tools, artifacts, `Command` |
| 6 | [RAG](06-rag.md) | Loaders, splitters, embeddings, vector stores, retrievers, agentic RAG |
| 7 | [LangGraph: StateGraph](07-langgraph.md) | Schema, typed state (`NewTyped`), nodes, edges, parallelism, `Send`, `Command`, subgraphs, functional API |
| 8 | [Persistence, human-in-the-loop and streaming](08-persistencia-hitl-streaming.md) | Checkpointers, durability, threads, `Interrupt`, subgraphs with namespace, time travel, store, streaming |
| 9 | [Agents and middleware](09-agents-e-middleware.md) | `agents.Create`, ready-made middleware, your own middleware, structured output |
| 10 | [Deep Agents](10-deep-agents.md) | Planning, virtual filesystem, subagents, backends, permissions, rubric, async |
| 11 | [MCP, server and remote graphs](11-mcp-remote-servidor.md) | MCP client, `remote.Client`, `remote.Graph`, `remote/server`, AG-UI |
| 12 | [Observability](12-observabilidade.md) | Callbacks, LangSmith, Langfuse, OpenTelemetry, logs |
| 13 | [Integrations](13-integracoes.md) | Vector stores (incl. HNSW and reading Annoy/FAISS indexes), databases, web search, loaders, permissions, sandboxes |
| 14 | [Production, security and troubleshooting](14-producao-seguranca-troubleshooting.md) | Checklist, risks, tests, common errors, limitations |
| 15 | [Examples and recipes](15-exemplos-e-receitas.md) | Catalog of the examples, recipes, equivalence with Python, glossary |
| 16 | [Long-term memory (LangMem)](16-memoria-langmem.md) | Semantic, episodic and procedural memory, tools, background, prompt optimization, summarization |
| 17 | [Agent Protocol](17-agent-protocol.md) | Serving agents through an open API: agents, threads, runs, store, typed streaming, human approval, WebSocket |
| 18 | [ciam: project context](18-ciam-contexto.md) | Local index with hybrid search, ADR/PRD/plans, memory, MCP server for editors |
| 19 | [Open SWE](19-open-swe.md) | Software factory: engineer with a per-thread sandbox, PR reviewer, CI watcher, webhooks and automations |
| 20 | [RAG, CAG and KAG](20-rag-cag-kag.md) | When to use each one, `rag` (hybrid search, incremental indexing), `cag` (corpus in a cacheable prefix), `kag` (graph, multi-hop planning) and the router |

## Related documents

- [Project README](../../../README.md) — overview and quick start
- [`docs/` index](../README.md) — all reference documents, one line each
- [ARCHITECTURE.md](../ARCHITECTURE.md) — design decisions
- [AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md), [CIAM.md](../CIAM.md) and [OPENSWE.md](../OPENSWE.md) — reference for the `agentprotocol`, `ciam` and `openswe` packages
- [KNOWLEDGE.md](../KNOWLEDGE.md) — reference for the `knowledge`, `rag`, `cag` and `kag` packages
- [LANGMEM.md](../LANGMEM.md) — reference for the `langmem` package and differences from Python LangMem
- [EVALS.md](../EVALS.md) — offline evaluation of agents and chains (`evals`)
- [DEV.md](../DEV.md) — development server (`langgraph/dev`, `cmd/langgraph`)
- [INTEGRATIONS.md](../INTEGRATIONS.md) — integrations and the verification level of each one
- [PROVIDERS.md](../PROVIDERS.md) and [PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md) — coverage of LangChain's 504 providers
- [examples/README.md](../../../examples/README.md) — index of the runnable examples

## Manual conventions

- Snippets use the real imports of the `github.com/fsvxavier/golang-chain-eco/...` module. To keep them short, repeated imports may be omitted.
- `ctx` is a `context.Context` and `m` is a `models.ChatModel`, unless stated otherwise.
- Errors are handled with `log.Fatal` in the `main` examples; in real code, handle them.
- Warnings about **security** and **surprising behavior** appear in "Warning" blocks.
