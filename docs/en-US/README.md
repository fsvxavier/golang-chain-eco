# Documentation

**English** | [Português](../pt-BR/README.md) | [Español](../es-ES/README.md)

Starting point for everything in `docs/`. The step-by-step usage guide is the [manual](manual/README.md); the other files are reference for a package or a decision.

## Where to start

| I want to... | Read |
|---|---|
| Install and run the first agent | [Manual, chapter 1](manual/01-introducao-e-instalacao.md) |
| Understand the layers and the core types | [Manual, chapter 2](manual/02-conceitos.md) and [ARCHITECTURE.md](ARCHITECTURE.md) |
| Give knowledge to a model (RAG, prompt cache, graph) | [Manual, chapter 20](manual/20-rag-cag-kag.md) and [KNOWLEDGE.md](KNOWLEDGE.md) |
| Take it to production | [Manual, chapter 14](manual/14-producao-seguranca-troubleshooting.md) |
| Find a ready-made example | [Manual, chapter 15](manual/15-exemplos-e-receitas.md) and [examples/README.md](../../examples/README.md) |
| Check whether a provider or database is supported | [INTEGRATIONS.md](INTEGRATIONS.md) and [PROVIDERS.md](PROVIDERS.md) |

## Manual (usage guide)

The [manual index](manual/README.md) has the 20 chapters, from installation to RAG/CAG/KAG, with a suggested reading path for each goal.

## Reference by package

| Document | Packages | Subject |
|---|---|---|
| [KNOWLEDGE.md](KNOWLEDGE.md) | `knowledge`, `rag`, `cag`, `kag`, `integrations/graphstore`, `cmd/knowledge` | RAG, CAG and KAG over a common contract, router, tools and MCP, graph on Neo4j and Memgraph |
| [LANGMEM.md](LANGMEM.md) | `langmem` | Long-term memory (semantic, episodic, procedural) and differences from Python LangMem |
| [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md) | `agentprotocol` | Agent Protocol server and client, typed streaming |
| [CIAM.md](CIAM.md) | `ciam`, `cmd/ciam` | Project context for AI assistants: index, hybrid search, ADR/PRD/plans, MCP |
| [OPENSWE.md](OPENSWE.md) | `openswe` | Engineer agent, PR reviewer, CI watcher and triggers |
| [EVALS.md](EVALS.md) | `evals` | Offline evaluation of agents and chains |
| [DEV.md](DEV.md) | `cmd/langgraph` | Development server (`langgraph dev`) |

## Architecture and coverage

| Document | Content |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Layers, design decisions and how the packages build on each other |
| [INTEGRATIONS.md](INTEGRATIONS.md) | What each integration covers, how it was verified and how to test against real services |
| [PROVIDERS.md](PROVIDERS.md) | Matrix of LangChain's 504 providers: what exists and what does not |
| [PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md) | Analysis of the missing providers and why |

## Design and plans

- superpowers/plans/: implementation plans, one file per feature.

## Conventions

- Documents are written in English, the canonical language; translations to Brazilian Portuguese (`docs/pt-BR/`) and Spanish (`docs/es-ES/`) mirror this structure. Identifiers and code comments are in English.
- Code snippets in the manuals are minimal and follow the real API; for exact usage, the `*_test.go` files of each package and the programs in `examples/` are the source of truth.
- Counts (packages, examples, providers) change with the code: check `examples/README.md` and `PROVIDERS.md` for the current numbers.
