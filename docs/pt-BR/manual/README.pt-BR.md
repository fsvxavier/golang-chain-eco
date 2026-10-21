# Manual de utilização — golang-chain-eco

[English](../../en-US/manual/README.md) | **Português** | [Español](../../es-ES/manual/README.es-ES.md)

Manual completo para usar o `golang-chain-eco`: um port em Go do ecossistema LangChain (langchain-core, LangGraph, `create_agent` com middleware, Deep Agents, cliente MCP, RemoteGraph e tracing).

Público: desenvolvedores Go que querem construir chains, agents e workflows com LLMs. Não é preciso conhecer a versão Python.

## Como ler

- Primeira vez: leia os capítulos 1 e 2 e rode o exemplo do capítulo 1.
- Quer só um agent funcionando: capítulos 3, 5 e 9.
- Quer RAG: capítulos 3, 4 e 6.
- Quer fluxos duráveis com aprovação humana: capítulos 7 e 8.
- Vai para produção: capítulos 12 e 14.
- Procurando um código pronto: capítulo 15.
- Agent que lembra entre conversas e aprende com feedback: capítulo 16.
- Expor seus agents por uma API aberta (UI, outros serviços): capítulo 17.
- Dar contexto de projeto barato e preciso a assistentes de IA / editores: capítulo 18.
- Agent que abre PRs, revisa código, vigia CI e responde ao GitHub: capítulo 19.
- Dar conhecimento ao modelo com RAG, CAG (cache de prompt) ou KAG (grafo de conhecimento), e deixar um roteador escolher: capítulo 20.

## Índice

| # | Capítulo | Conteúdo |
|---|---|---|
| 1 | [Introdução e instalação](01-introducao-e-instalacao.md) | Requisitos, instalação, primeiro agent, variáveis de ambiente, rodando offline |
| 2 | [Conceitos fundamentais](02-conceitos.md) | Camadas, `Runnable`, mensagens, estado, canais, contexto e configuração |
| 3 | [Modelos e provedores](03-modelos-e-provedores.md) | `chatmodels.Init`, provedores nativos e compatíveis, router entre gateways, embeddings, cache, rate limit, modelo fake |
| 4 | [Prompts, chains e parsers](04-chains-prompts-parsers.md) | Templates, `Pipe`, combinadores, retry, fallbacks, saída estruturada, histórico de chat |
| 5 | [Tools](05-tools.md) | Criar tools tipadas, schema, erros, tools terminais, artefatos, `Command` |
| 6 | [RAG](06-rag.md) | Loaders, splitters, embeddings, vector stores, retrievers, RAG agêntico |
| 7 | [LangGraph: StateGraph](07-langgraph.md) | Schema, estado tipado (`NewTyped`), nós, arestas, paralelismo, `Send`, `Command`, subgrafos, API funcional |
| 8 | [Persistência, human-in-the-loop e streaming](08-persistencia-hitl-streaming.md) | Checkpointers, durabilidade, threads, `Interrupt`, subgrafos com namespace, time travel, store, streaming |
| 9 | [Agents e middleware](09-agents-e-middleware.md) | `agents.Create`, middlewares prontos, middleware próprio, saída estruturada |
| 10 | [Deep Agents](10-deep-agents.md) | Planejamento, filesystem virtual, subagents, backends, permissões, rubrica, async |
| 11 | [MCP, servidor e graphs remotos](11-mcp-remote-servidor.md) | Cliente MCP, `remote.Client`, `remote.Graph`, `remote/server`, AG-UI |
| 12 | [Observabilidade](12-observabilidade.md) | Callbacks, LangSmith, Langfuse, OpenTelemetry, logs |
| 13 | [Integrações](13-integracoes.md) | Vector stores (incl. HNSW e leitura de índices Annoy/FAISS), bancos, busca na web, loaders, permissões, sandboxes |
| 14 | [Produção, segurança e troubleshooting](14-producao-seguranca-troubleshooting.md) | Checklist, riscos, testes, erros comuns, limitações |
| 15 | [Exemplos e receitas](15-exemplos-e-receitas.md) | Catálogo dos exemplos, receitas, equivalência com Python, glossário |
| 16 | [Memória de longo prazo (LangMem)](16-memoria-langmem.md) | Memória semântica, episódica e procedural, tools, background, otimização de prompts, resumo |
| 17 | [Agent Protocol](17-agent-protocol.md) | Servir agents por uma API aberta: agents, threads, runs, store, streaming tipado, aprovação humana, WebSocket |
| 18 | [ciam: contexto de projeto](18-ciam-contexto.md) | Índice local com busca híbrida, ADR/PRD/planos, memória, servidor MCP para editores |
| 19 | [Open SWE](19-open-swe.md) | Fábrica de software: engenheiro com sandbox por thread, revisor de PRs, vigia de CI, webhooks e automações |
| 20 | [RAG, CAG e KAG](20-rag-cag-kag.md) | Quando usar cada um, `rag` (busca híbrida, indexação incremental), `cag` (corpus em prefixo cacheável), `kag` (grafo, planejamento multi-hop) e o roteador |

## Documentos relacionados

- [README do projeto](../../../README.pt-BR.md) — visão geral e início rápido
- [Índice de `docs/`](../README.pt-BR.md) — todos os documentos de referência, com uma linha cada
- [ARCHITECTURE.md](../ARCHITECTURE.md) — decisões de design
- [AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md), [CIAM.md](../CIAM.md) e [OPENSWE.md](../OPENSWE.md) — referência dos pacotes `agentprotocol`, `ciam` e `openswe`
- [KNOWLEDGE.md](../KNOWLEDGE.md) — referência dos pacotes `knowledge`, `rag`, `cag` e `kag`
- [LANGMEM.md](../LANGMEM.md) — referência do pacote `langmem` e diferenças em relação ao LangMem Python
- [EVALS.md](../EVALS.md) — avaliação offline de agents e chains (`evals`)
- [DEV.md](../DEV.md) — servidor de desenvolvimento (`langgraph/dev`, `cmd/langgraph`)
- [INTEGRATIONS.md](../INTEGRATIONS.md) — integrações e nível de verificação de cada uma
- [PROVIDERS.md](../PROVIDERS.md) e [PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md) — cobertura dos 504 provedores da LangChain
- [examples/README.pt-BR.md](../../../examples/README.pt-BR.md) — índice dos exemplos executáveis

## Convenções do manual

- Os trechos usam os imports reais do módulo `github.com/fsvxavier/golang-chain-eco/...`. Para encurtar, imports repetidos podem ser omitidos.
- `ctx` é um `context.Context` e `m` é um `models.ChatModel`, salvo indicação contrária.
- Erros são tratados com `log.Fatal` nos exemplos de `main`; em código real, trate-os.
- Avisos de **segurança** e de **comportamento surpreendente** aparecem em blocos "Atenção".
