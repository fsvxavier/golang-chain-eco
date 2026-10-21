# Manual de uso — golang-chain-eco

[English](../../en-US/manual/README.md) | [Português](../../pt-BR/manual/README.pt-BR.md) | **Español**

Manual completo para usar `golang-chain-eco`: un port en Go del ecosistema LangChain (langchain-core, LangGraph, `create_agent` con middleware, Deep Agents, cliente MCP, RemoteGraph y tracing).

Público: desarrolladores Go que quieren construir chains, agents y workflows con LLMs. No hace falta conocer la versión de Python.

## Cómo leer

- Primera vez: lee los capítulos 1 y 2 y ejecuta el ejemplo del capítulo 1.
- Solo quieres un agent funcionando: capítulos 3, 5 y 9.
- Quieres RAG: capítulos 3, 4 y 6.
- Quieres flujos duraderos con aprobación humana: capítulos 7 y 8.
- Vas a producción: capítulos 12 y 14.
- Buscas código listo: capítulo 15.
- Agent que recuerda entre conversaciones y aprende con feedback: capítulo 16.
- Exponer tus agents mediante una API abierta (UI, otros servicios): capítulo 17.
- Dar contexto de proyecto barato y preciso a asistentes de IA / editores: capítulo 18.
- Agent que abre PRs, revisa código, vigila el CI y responde en GitHub: capítulo 19.
- Dar conocimiento al modelo con RAG, CAG (caché de prompt) o KAG (grafo de conocimiento), y dejar que un router elija: capítulo 20.

## Índice

| # | Capítulo | Contenido |
|---|---|---|
| 1 | [Introducción e instalación](01-introducao-e-instalacao.md) | Requisitos, instalación, primer agent, variables de entorno, ejecución offline |
| 2 | [Conceptos fundamentales](02-conceitos.md) | Capas, `Runnable`, mensajes, estado, canales, contexto y configuración |
| 3 | [Modelos y proveedores](03-modelos-e-provedores.md) | `chatmodels.Init`, proveedores nativos y compatibles, router entre gateways, embeddings, caché, rate limit, modelo fake |
| 4 | [Prompts, chains y parsers](04-chains-prompts-parsers.md) | Plantillas, `Pipe`, combinadores, retry, fallbacks, salida estructurada, historial de chat |
| 5 | [Tools](05-tools.md) | Crear tools tipadas, schema, errores, tools terminales, artefactos, `Command` |
| 6 | [RAG](06-rag.md) | Loaders, splitters, embeddings, vector stores, retrievers, RAG agéntico |
| 7 | [LangGraph: StateGraph](07-langgraph.md) | Schema, estado tipado (`NewTyped`), nodos, aristas, paralelismo, `Send`, `Command`, subgrafos, API funcional |
| 8 | [Persistencia, human-in-the-loop y streaming](08-persistencia-hitl-streaming.md) | Checkpointers, durabilidad, threads, `Interrupt`, subgrafos con namespace, viaje en el tiempo, store, streaming |
| 9 | [Agents y middleware](09-agents-e-middleware.md) | `agents.Create`, middlewares listos, middleware propio, salida estructurada |
| 10 | [Deep Agents](10-deep-agents.md) | Planificación, filesystem virtual, subagents, backends, permisos, rúbrica, async |
| 11 | [MCP, servidor y grafos remotos](11-mcp-remote-servidor.md) | Cliente MCP, `remote.Client`, `remote.Graph`, `remote/server`, AG-UI |
| 12 | [Observabilidad](12-observabilidade.md) | Callbacks, LangSmith, Langfuse, OpenTelemetry, logs |
| 13 | [Integraciones](13-integracoes.md) | Vector stores (incl. HNSW y lectura de índices Annoy/FAISS), bases de datos, búsqueda web, loaders, permisos, sandboxes |
| 14 | [Producción, seguridad y troubleshooting](14-producao-seguranca-troubleshooting.md) | Checklist, riesgos, pruebas, errores comunes, limitaciones |
| 15 | [Ejemplos y recetas](15-exemplos-e-receitas.md) | Catálogo de los ejemplos, recetas, equivalencia con Python, glosario |
| 16 | [Memoria a largo plazo (LangMem)](16-memoria-langmem.md) | Memoria semántica, episódica y procedural, tools, background, optimización de prompts, resumen |
| 17 | [Agent Protocol](17-agent-protocol.md) | Servir agents mediante una API abierta: agents, threads, runs, store, streaming tipado, aprobación humana, WebSocket |
| 18 | [ciam: contexto de proyecto](18-ciam-contexto.md) | Índice local con búsqueda híbrida, ADR/PRD/planes, memoria, servidor MCP para editores |
| 19 | [Open SWE](19-open-swe.md) | Fábrica de software: ingeniero con sandbox por thread, revisor de PRs, vigilante de CI, webhooks y automatizaciones |
| 20 | [RAG, CAG y KAG](20-rag-cag-kag.md) | Cuándo usar cada uno, `rag` (búsqueda híbrida, indexación incremental), `cag` (corpus en prefijo cacheable), `kag` (grafo, planificación multi-salto) y el router |

## Documentos relacionados

- [README del proyecto](../../../README.es-ES.md) — visión general e inicio rápido
- [Índice de `docs/`](../README.es-ES.md) — todos los documentos de referencia, con una línea cada uno
- [ARCHITECTURE.md](../ARCHITECTURE.md) — decisiones de diseño
- [AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md), [CIAM.md](../CIAM.md) y [OPENSWE.md](../OPENSWE.md) — referencia de los paquetes `agentprotocol`, `ciam` y `openswe`
- [KNOWLEDGE.md](../KNOWLEDGE.md) — referencia de los paquetes `knowledge`, `rag`, `cag` y `kag`
- [LANGMEM.md](../LANGMEM.md) — referencia del paquete `langmem` y diferencias respecto a LangMem de Python
- [EVALS.md](../EVALS.md) — evaluación offline de agents y chains (`evals`)
- [DEV.md](../DEV.md) — servidor de desarrollo (`langgraph/dev`, `cmd/langgraph`)
- [INTEGRATIONS.md](../INTEGRATIONS.md) — integraciones y nivel de verificación de cada una
- [Manuales de integración](../../integrations/es-ES/README.md) — router con varios gateways y clientes MCP
- [PROVIDERS.md](../PROVIDERS.md) y [PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md) — cobertura de los 504 proveedores de LangChain
- [examples/README.es-ES.md](../../../examples/README.es-ES.md) — índice de los ejemplos ejecutables

## Convenciones del manual

- Los fragmentos usan los imports reales del módulo `github.com/fsvxavier/golang-chain-eco/...`. Para acortar, los imports repetidos pueden omitirse.
- `ctx` es un `context.Context` y `m` es un `models.ChatModel`, salvo indicación contraria.
- Los errores se tratan con `log.Fatal` en los ejemplos de `main`; en código real, manéjalos.
- Las advertencias de **seguridad** y de **comportamiento sorprendente** aparecen en bloques "Atención".
