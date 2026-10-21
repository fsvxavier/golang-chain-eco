# Exemplos

[English](README.md) | **Português** | [Español](README.es-ES.md)

Cada exemplo é um programa independente: `go run ./examples/<grupo>/<nome>`.
Sem chave de API eles usam um modelo fake com respostas calculadas (saída determinística);
com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` usam o provedor real.
Cada pasta tem `README.md` (inglês), `README.pt-BR.md` e `README.es-ES.md` com o que o exemplo faz, como aplicar o padrão, como rodar e como testar em modo produtivo.
Os grupos `langchain`, `langgraph` e `deepagents` foram portados dos repositórios de mesmo nome; `langmem`, `agentprotocol`, `ciam` e `openswe` foram portados dos projetos de mesmo nome (LangMem, Agent Protocol, context-ia-manager e Open SWE) e `integrations` são exemplos próprios deste projeto.

## langchain

| Exemplo | Mostra | Origem |
|---|---|---|
| `langchain/chain` | `prompt \| modelo \| parser` tipado | LCEL |
| `langchain/rag_chain` | splitter, embeddings, retriever e chain RAG | LCEL / RAG |
| `langchain/middleware_stack` | PII + retry + fallback + limites compostos | `langchain_v1/agents/middleware` |

## langgraph

| Exemplo | Mostra | Origem |
|---|---|---|
| `langgraph/react_from_scratch` | loop ReAct montado à mão com `StateGraph` | `react-agent-from-scratch.ipynb` |
| `langgraph/react_agent_middleware` | `agents.Create` + streaming de updates | `create_agent` |
| `langgraph/structured_output` | agent que termina com resposta tipada | `react-agent-structured-output.ipynb` |
| `langgraph/tool_calling` | tools em paralelo, erro de tool e tool inexistente | `tool-calling.ipynb` |
| `langgraph/wait_user_input` | `Interrupt` + `Command{Resume}` + checkpoint | `human_in_the_loop/wait-user-input.ipynb` |
| `langgraph/hitl_agent` | aprovação humana de uma tool (middleware) | HITL do `create_agent` |
| `langgraph/customer_support` | tools livres + tool sensível com aprovação/rejeição | `customer-support.ipynb` |
| `langgraph/subgraph` | grafo compilado usado como nó | `subgraph.ipynb` |
| `langgraph/plan_and_execute` | planner, executor e replanner | `plan-and-execute.ipynb` |
| `langgraph/rewoo` | plano completo com placeholders `#E1`, execução e solver | `rewoo/rewoo.ipynb` |
| `langgraph/llm_compiler` | DAG de tarefas executado em paralelo com `Send` | `llm-compiler/LLMCompiler.ipynb` |
| `langgraph/reflection` | gerador e crítico em loop | `reflection.ipynb` |
| `langgraph/code_assistant` | gerar, checar, refletir e tentar de novo | `code_assistant/*.ipynb` |
| `langgraph/extraction_retries` | extração validada com retry guiado pelo erro | `extraction/retries.ipynb` |
| `langgraph/info_gather` | chatbot que coleta requisitos e muda de fase | `chatbots/information-gather-prompting.ipynb` |
| `langgraph/self_discover` | pipeline de 4 prompts (selecionar, adaptar, estruturar, resolver) | `self-discover/self-discover.ipynb` |
| `langgraph/multi_agent_supervisor` | supervisor roteando para especialistas | `multi_agent/multi-agent-collaboration.ipynb` |
| `langgraph/hierarchical_teams` | supervisor de supervisores (subgrafos por time) | `multi_agent/hierarchical_agent_teams.ipynb` |
| `langgraph/chatbot_simulation` | usuário simulado + avaliador | `chatbot-simulation-evaluation/*.ipynb` |
| `langgraph/corrective_rag` | RAG com avaliação de documentos e fallback web | `rag/langgraph_crag.ipynb` |
| `langgraph/self_rag` | RAG que checa se a resposta é fundamentada e regenera | `rag/langgraph_self_rag.ipynb` |
| `langgraph/adaptive_rag` | roteador escolhe vector store ou web | `rag/langgraph_adaptive_rag.ipynb` |
| `langgraph/sql_agent` | agent de SQL com guarda somente-leitura | `tutorials/sql-agent.ipynb` |
| `langgraph/map_reduce` | fan-out com `Send` e redução | Send API |
| `langgraph/typed_state` | estado tipado com struct (`NewTyped`, tags de reducer, `Validate`) | próprio |
| `langgraph/engineering_team` | time de engenharia (gerente, PO, arquiteto, líder, 4 devs, 2 revisores, 2 QA) com handoff por `Send`/`Command`, registro de disponibilidade com reserva atômica e skills por agente em `skills/` | próprio |
| `langgraph/independent_engineering_team` | o mesmo time como serviços separados: hub (reserva atômica com leases, fila de jobs, RAG/CAG/KAG e memória do time via MCP), gerente em `remote/server`, workers em Agent Protocol; modelo por agente via env; Docker compose | próprio |
| `langgraph/decider_fallback` | `Decider`, `RuleDecider` e o fallback `Guarded`, sem modelo | próprio |
| `langgraph/compress_history_summary` | `Summary` de um histórico acima de 8k tokens, cabendo no orçamento de 700 tokens | próprio |
| `langgraph/compress_shell_rtk` | saída de shell pelo RTK quando o decider decide | próprio |
| `langgraph/compress_prompt_caveman` | compressão de prompt e de resposta (Caveman e a diretiva de resposta concisa) | próprio |
| `langgraph/compress_toon` | resultado JSON de ferramenta em TOON, tamanhos antes e depois, fallback `TOONShorter` | próprio |
| `langgraph/compress_ponytail` | nota Ponytail por nível como o modelo a recebe; system prompt intacto | próprio |
| `langgraph/decider_laya_http` | `HTTPDecider` contra o `laya-serve` (servidor falso offline) | próprio |
| `langgraph/decider_laya_native` | decider laya-go nativo, módulo separado, tag `laya_native` | próprio |
| `langgraph/full_team` | o time independente com compressão de chamadas; decider e modelo falsos por padrão | próprio |

## deepagents

| Exemplo | Mostra | Origem |
|---|---|---|
| `deepagents/basic` | planejamento + filesystem virtual | `deepagents` |
| `deepagents/research` | subagents de pesquisa em paralelo, relatório em arquivo | `examples/deep_research` |
| `deepagents/text_to_sql` | plano, schema em arquivo, subagent validador | `examples/text-to-sql-agent` |
| `deepagents/content_builder` | memória (`AGENTS.md`), skills e arquivos reais em disco | `examples/content-builder-agent` |
| `deepagents/ralph_mode` | loop com contexto novo por iteração, estado só em arquivos | `examples/ralph_mode` |
| `deepagents/rubric` | grader com critérios e revisão iterativa | `examples/rubric_middleware` |
| `deepagents/async_subagents` | tarefa em background (runner local) | `examples/async-subagent-server` |
| `deepagents/async_subagent_server` | servidor de agents HTTP + `RemoteRunner` | `examples/async-subagent-server` |
| `deepagents/mcp_docs_agent` | tools vindas de um servidor MCP | `examples/deploy-mcp-docs-agent` |

## langmem

Memória de longo prazo do [LangMem](https://github.com/langchain-ai/langmem) integrada ao store e ao `agents.Create` (pacote [`langmem`](../langmem), referência em [docs/en-US/LANGMEM.md](../docs/pt-BR/LANGMEM.md)). Todos rodam offline com um modelo fake que fala o protocolo de tool calls do extrator.

| Exemplo | Mostra | Origem |
|---|---|---|
| `langmem/hot_path` | agent com `manage_memory` e `search_memory`, memória isolada por usuário, busca semântica | `hot_path_quickstart` |
| `langmem/background` | `StoreManager` + `ReflectionExecutor`: 3 turnos rápidos geram 1 extração; busca nas memórias | `background_quickstart`, `delayed_processing` |
| `langmem/profile_episodic` | coleção (inserir/atualizar/apagar), perfil de um documento com semente, episódios | `extract_semantic_memories`, `manage_user_profile`, `extract_episodic_memories` |
| `langmem/prompt_optimizer` | prompt que aprende com feedback (metaprompt) e otimizador de vários prompts | `optimize_memory_prompt`, `optimize_compound_system` |
| `langmem/summarization` | resumo incremental em um grafo com checkpointer; histórico completo, modelo vê o resumido | `summarization` |

## agentprotocol

Servir agents pelo [Agent Protocol](https://github.com/langchain-ai/agent-protocol) (pacote [`agentprotocol`](../agentprotocol), referência em [docs/en-US/AGENT-PROTOCOL.md](../docs/pt-BR/AGENT-PROTOCOL.md)). Rodam offline (servidor `httptest` em processo).

| Exemplo | Mostra | Origem |
|---|---|---|
| `agentprotocol/serve_and_call` | descoberta de agents, threads multi-turno, runs (wait/stream), histórico, fork, cópia, store | OpenAPI do protocolo |
| `agentprotocol/human_in_the_loop` | streaming por thread: canais `lifecycle`/`messages`/`tools`/`input`, `run.start`, `input.respond`, `state.*` | Agent Streaming Protocol |

## ciam

Contexto cirúrgico para assistentes de IA (pacote [`ciam`](../ciam), referência em [docs/en-US/CIAM.md](../docs/pt-BR/CIAM.md)). Rodam offline com embeddings por hashing; com Ollama, use `providers/ollama.Embeddings`.

| Exemplo | Mostra | Origem |
|---|---|---|
| `ciam/project_context` | índice incremental, busca híbrida por tipo, compressão, ADR/PRD/plano (supersede), contexto de decisão, memória, servidor MCP | context-ia-manager |
| `ciam/agent_with_context` | agent que descobre uma decisão (ADR) pelo contexto antes de implementar | context-ia-manager |

## openswe

Fábrica de software com agents (pacote [`openswe`](../openswe), referência em [docs/en-US/OPENSWE.md](../docs/pt-BR/OPENSWE.md)). Usam GitHub, Slack e Linear simulados ([`internal/fakegithub`](internal/fakegithub), [`internal/fakeslack`](internal/fakeslack), [`internal/fakelinear`](internal/fakelinear)), modelo roteirizado e git/sandbox reais em diretórios temporários.

| Exemplo | Mostra | Origem |
|---|---|---|
| `openswe/engineer` | issue → sandbox por thread → atalho `gh pr create` bloqueado → PR em rascunho → comentário e vínculo PR↔thread | open-swe |
| `openswe/reviewer` | review única com finding ancorado no diff, push recusado (somente-leitura), re-revisão que reconcilia a resposta humana | open-swe |
| `openswe/factory` | webhook (menção, fila de follow-ups, duplicata, estranho em repo público), PR novo → revisão, cron, vigia de CI sem desperdício | open-swe |
| `openswe/slack_trigger` | Slack: `url_verification`, menção numa thread, retry ignorado, resposta final postada na thread | open-swe |

## evals

| Exemplo | Mostra | Origem |
|---|---|---|
| `evals/basic` | `evals.Evaluate` com exato, contains, embedding e LLM-as-judge; relatório texto/JSON | próprio (evals offline do LangSmith) |

## integrations

Exemplos das integrações (todos rodam offline; o último usa S3 se `S3_ENDPOINT` estiver definido).

| Exemplo | Mostra |
|---|---|
| `integrations/graph_rag` | retriever que segue links de metadados: similaridade pura vs travessia em 2 saltos |
| `integrations/agui_server` | grafo servido pelo protocolo AG-UI (SSE) e chamado em processo, com os eventos impressos |
| `integrations/permission_rag` | retriever filtrado por permissão (Permit; SpiceDB tem o mesmo formato), cada usuário vê o que pode |
| `integrations/remote_sandbox` | `backends.Remote`: arquivos e shell sobre um runner qualquer (aqui bash local; em produção um pod) |
| `integrations/vector_indexes` | índices `Flat` (exato) e `Annoy` (aproximado): recall vs `SearchK`, salvar e carregar |
| `integrations/durable_threads` | estado de uma thread num checkpointer (S3 se configurado, memória se não) |

## knowledge

RAG, CAG e KAG sobre o contrato comum `knowledge.Answerer` (todos rodam offline com modelo fake).

| Exemplo | Mostra |
|---|---|
| `knowledge/rag` | indexação incremental (`Added`/`Skipped`/`Removed`), busca vetorial vs híbrida (termo exato), resposta com citações |
| `knowledge/cag` | corpus em prefixo estável, hash e tokens, `cache_hit` na segunda pergunta, fallback quando o corpus não cabe |
| `knowledge/kag` | grafo extraído de trechos, pergunta de 3 saltos que a busca vetorial não responde, verificação, grafo com ciclo |
| `knowledge/agent_tools` | agent (`agents.Create`) usando as tools do `knowledge.Toolkit`; o modelo chama `rag_answer` e cita as fontes |
| `knowledge/mcp_server` | as mesmas tools como servidor MCP; com `-demo` mostra `tools/list` e `tools/call` em processo, sem `-demo` serve stdio |
| `knowledge/router` | roteador escolhendo KAG, CAG ou RAG pela pergunta e pelo perfil, `Force` e fallback para RAG |

## Não portados

`web-navigation` (precisa de navegador), `lats` (busca em árvore com avaliação por rollouts), `reflexion`, os exemplos `deploy-*`
(configuração da plataforma de deploy), `llm-wiki`, `nvidia_deep_agent`, `better-harness`, `talon`, `downloading_agents` e
`delta-channel-dump` (específicos do ecossistema Python/plataforma). Os blocos de construção usados por eles já existem aqui.
