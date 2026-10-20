# Documentación

[English](../en-US/README.md) | [Português](../pt-BR/README.md) | **Español**

Punto de partida para todo lo que está en `docs/`. La guía de uso paso a paso es el [manual](manual/README.md); los demás archivos son referencia de un paquete o de una decisión.

## Por dónde empezar

| Quiero... | Lee |
|---|---|
| Instalar y ejecutar el primer agent | [Manual, capítulo 1](manual/01-introducao-e-instalacao.md) |
| Entender las capas y los tipos centrales | [Manual, capítulo 2](manual/02-conceitos.md) y [ARCHITECTURE.md](ARCHITECTURE.md) |
| Dar conocimiento a un modelo (RAG, caché de prompt, grafo) | [Manual, capítulo 20](manual/20-rag-cag-kag.md) y [KNOWLEDGE.md](KNOWLEDGE.md) |
| Llevarlo a producción | [Manual, capítulo 14](manual/14-producao-seguranca-troubleshooting.md) |
| Encontrar un ejemplo listo | [Manual, capítulo 15](manual/15-exemplos-e-receitas.md) y [examples/README.md](../../examples/README.md) |
| Saber si un proveedor o base de datos es compatible | [INTEGRATIONS.md](INTEGRATIONS.md) y [PROVIDERS.md](PROVIDERS.md) |

## Manual (guía de uso)

El [índice del manual](manual/README.md) tiene los 20 capítulos, desde la instalación hasta RAG/CAG/KAG, con la ruta de lectura sugerida para cada objetivo.

## Referencia por paquete

| Documento | Paquetes | Asunto |
|---|---|---|
| [KNOWLEDGE.md](KNOWLEDGE.md) | `knowledge`, `rag`, `cag`, `kag`, `integrations/graphstore`, `cmd/knowledge` | RAG, CAG y KAG sobre un contrato común, router, tools y MCP, grafo en Neo4j y Memgraph |
| [LANGMEM.md](LANGMEM.md) | `langmem` | Memoria a largo plazo (semántica, episódica, procedural) y diferencias respecto a LangMem de Python |
| [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md) | `agentprotocol` | Servidor y cliente del Agent Protocol, streaming tipado |
| [CIAM.md](CIAM.md) | `ciam`, `cmd/ciam` | Contexto de proyecto para asistentes de IA: índice, búsqueda híbrida, ADR/PRD/planes, MCP |
| [OPENSWE.md](OPENSWE.md) | `openswe` | Agent ingeniero, revisor de PRs, vigilante de CI y disparadores |
| [EVALS.md](EVALS.md) | `evals` | Evaluación offline de agents y chains |
| [DEV.md](DEV.md) | `cmd/langgraph` | Servidor de desarrollo (`langgraph dev`) |

## Arquitectura y cobertura

| Documento | Contenido |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Capas, decisiones de diseño y cómo se apoyan los paquetes |
| [INTEGRATIONS.md](INTEGRATIONS.md) | Qué cubre cada integración, cómo se verificó y cómo probar contra servicios reales |
| [PROVIDERS.md](PROVIDERS.md) | Matriz de los 504 proveedores de LangChain: qué existe y qué no |
| [PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md) | Análisis de los proveedores que faltan y por qué |

## Diseño y planes

- superpowers/plans/: planes de implementación, un archivo por funcionalidad.

## Convenciones

- Los documentos se escriben en inglés, idioma canónico; las traducciones al portugués de Brasil (`docs/pt-BR/`) y al español (`docs/es-ES/`) replican esta estructura. Los identificadores y comentarios de código están en inglés.
- Los fragmentos de código de los manuales son mínimos y siguen la API real; para el uso exacto, los `*_test.go` de cada paquete y los programas en `examples/` son la fuente.
- Los conteos (paquetes, ejemplos, proveedores) cambian con el código: consulta `examples/README.md` y `PROVIDERS.md` para los números actuales.
