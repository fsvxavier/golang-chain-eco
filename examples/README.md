# Exemplos

Cada exemplo é um programa independente: `go run ./examples/<grupo>/<nome>`.
Sem chave de API eles usam um modelo fake com respostas calculadas (saída determinística);
com `ANTHROPIC_API_KEY` ou `OPENAI_API_KEY` usam o provedor real.
Os exemplos foram portados dos repositórios `langchain`, `langgraph` e `deepagents`.

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

## Não portados

`web-navigation` (precisa de navegador), `lats` (busca em árvore com avaliação por rollouts), `reflexion`, os exemplos `deploy-*`
(configuração da plataforma de deploy), `llm-wiki`, `nvidia_deep_agent`, `better-harness`, `talon`, `downloading_agents` e
`delta-channel-dump` (específicos do ecossistema Python/plataforma). Os blocos de construção usados por eles já existem aqui.
