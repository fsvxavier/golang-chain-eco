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
| `langgraph/subgraph` | grafo compilado usado como nó | `subgraph.ipynb` |
| `langgraph/plan_and_execute` | planner, executor e replanner | `plan-and-execute.ipynb` |
| `langgraph/reflection` | gerador e crítico em loop | `reflection.ipynb` |
| `langgraph/multi_agent_supervisor` | supervisor roteando para especialistas | `multi_agent/*.ipynb` |
| `langgraph/corrective_rag` | RAG com avaliação de documentos e fallback web | `rag/langgraph_crag.ipynb` |
| `langgraph/map_reduce` | fan-out com `Send` e redução | Send API / LLMCompiler |

## deepagents

| Exemplo | Mostra | Origem |
|---|---|---|
| `deepagents/basic` | planejamento + filesystem virtual | `deepagents` |
| `deepagents/research` | subagents de pesquisa em paralelo, relatório em arquivo | `examples/deep_research` |
| `deepagents/rubric` | grader com critérios e revisão iterativa | `examples/rubric_middleware` |
| `deepagents/async_subagents` | tarefa em background e consulta posterior | `examples/async-subagent-server` |

## Não portados (ainda)

`web-navigation`, `lats`, `self-discover`, `rewoo`, `llm-compiler` (completo), `customer-support`, `sql-agent`,
`ralph_mode`, `llm-wiki` e os exemplos `deploy-*` dependem de integrações externas (navegador, banco, plataforma de deploy).
Os blocos de construção (grafos, `Send`, HITL, backends, MCP) já estão disponíveis para reproduzi-los.
