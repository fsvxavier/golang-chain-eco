# Documentação

[English](../en-US/README.md) | **Português** | [Español](../es-ES/README.md)

Ponto de partida para tudo que está em `docs/`. O guia de uso passo a passo é o [manual](manual/README.md); os demais arquivos são referência de um pacote ou de uma decisão.

## Por onde começar

| Quero... | Leia |
|---|---|
| Instalar e rodar o primeiro agent | [Manual, capítulo 1](manual/01-introducao-e-instalacao.md) |
| Entender as camadas e os tipos centrais | [Manual, capítulo 2](manual/02-conceitos.md) e [ARCHITECTURE.md](ARCHITECTURE.md) |
| Dar conhecimento a um modelo (RAG, cache de prompt, grafo) | [Manual, capítulo 20](manual/20-rag-cag-kag.md) e [KNOWLEDGE.md](KNOWLEDGE.md) |
| Levar para produção | [Manual, capítulo 14](manual/14-producao-seguranca-troubleshooting.md) |
| Achar um exemplo pronto | [Manual, capítulo 15](manual/15-exemplos-e-receitas.md) e [examples/README.md](../../examples/README.md) |
| Saber se um provedor ou banco é suportado | [INTEGRATIONS.md](INTEGRATIONS.md) e [PROVIDERS.md](PROVIDERS.md) |

## Manual (guia de uso)

O [índice do manual](manual/README.md) tem os 20 capítulos, de instalação a RAG/CAG/KAG, com o caminho de leitura sugerido para cada objetivo.

## Referência por pacote

| Documento | Pacotes | Assunto |
|---|---|---|
| [KNOWLEDGE.md](KNOWLEDGE.md) | `knowledge`, `rag`, `cag`, `kag`, `integrations/graphstore`, `cmd/knowledge` | RAG, CAG e KAG sobre um contrato comum, roteador, tools e MCP, grafo em Neo4j e Memgraph |
| [LANGMEM.md](LANGMEM.md) | `langmem` | Memória de longo prazo (semântica, episódica, procedural) e diferenças em relação ao LangMem Python |
| [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md) | `agentprotocol` | Servidor e cliente do Agent Protocol, streaming tipado |
| [CIAM.md](CIAM.md) | `ciam`, `cmd/ciam` | Contexto de projeto para assistentes de IA: índice, busca híbrida, ADR/PRD/planos, MCP |
| [OPENSWE.md](OPENSWE.md) | `openswe` | Agent engenheiro, revisor de PRs, vigia de CI e gatilhos |
| [EVALS.md](EVALS.md) | `evals` | Avaliação offline de agents e chains |
| [DEV.md](DEV.md) | `cmd/langgraph` | Servidor de desenvolvimento (`langgraph dev`) |

## Arquitetura e cobertura

| Documento | Conteúdo |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Camadas, decisões de design e como os pacotes se apoiam (em inglês) |
| [INTEGRATIONS.md](INTEGRATIONS.md) | O que cada integração cobre, como foi verificada e como testar contra serviços reais |
| [PROVIDERS.md](PROVIDERS.md) | Matriz dos 504 provedores da LangChain: o que existe e o que não |
| [PROVIDERS-ANALISE.md](PROVIDERS-ANALISE.md) | Análise dos provedores que faltam e por que |

## Desenho e planos

- superpowers/plans/: planos de implementação, um arquivo por funcionalidade.

## Convenções

- Texto em português do Brasil; identificadores e comentários de código em inglês. `ARCHITECTURE.md` é a exceção e está em inglês.
- Trechos de código dos manuais são mínimos e seguem a API real; para o uso exato, os `*_test.go` de cada pacote e os programas em `examples/` são a fonte.
- Contagens (pacotes, exemplos, provedores) mudam com o código: confira `examples/README.md` e `PROVIDERS.md` para os números atuais.
