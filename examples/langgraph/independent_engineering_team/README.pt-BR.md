# independent_engineering_team

O fluxo do `engineering_team` com cada agente como **serviço próprio**: um processo (ou container, ou máquina) por agente, conversando por HTTP. Os workers sobem e caem sozinhos e pegam jobs de uma fila.

## O que faz

| Serviço | Qtd | Papel |
|---|---|---|
| `hub` | 1 | Registro com reserva atômica e leases, fila de jobs e um servidor MCP com RAG, CAG, KAG e a memória do time. |
| `manager` | 1 | Software Engineer Manager. Recebe o pedido (API de Agent Server, `remote/server`), roteia, conduz o fluxo e escreve o relatório. Nunca escreve código (a execução falha se a resposta tiver bloco de código). |
| `po`, `architect`, `leader` | 1 cada | Especificação, risco e segurança, plano de tarefas com complexidade. |
| `junior-1`, `junior-2`, `mid-1`, `senior-1` | 4 | Implementam em paralelo. |
| `specialist-1`, `specialist-2` | 2 | Revisão: aprova ou devolve. |
| `qa-1`, `qa-2` | 2 | QA: aprova ou devolve. |

Cada worker é o `cmd/worker` com `WORKER_ROLE`; a persona é `skills/<nome>/SKILL.md`.

Como as peças conversam:

- **Pedido de entrada**: `remote.Graph` (cliente) -> `remote/server` do manager.
- **Trabalho de saída**: o manager envia um job ao hub (`Prefer` = ids de workers ou papéis, em ordem). O hub reserva um worker livre **atomicamente** (um lock; dois jobs nunca pegam o mesmo worker; trabalho de alta complexidade nunca vai para júnior) ou deixa o job na fila. O worker faz long-poll em `claim`, executa e devolve o resultado.
- **Agent Protocol**: cada worker serve seu agente por Agent Protocol (`agentprotocol.Server`). O job roda como run numa thread com o nome da tarefa (`<pedido>-T3-dev`), então a tarefa devolvida volta ao mesmo engenheiro, que lembra do que escreveu. Qualquer cliente Agent Protocol também pode chamar um worker direto.
- **Leases**: os workers enviam heartbeat. Worker mudo por `HUB_LEASE_TTL` é descartado e seu job volta ao início da fila (o job é abandonado após 3 entregas). O resultado atrasado do worker descartado é rejeitado (409): um job nunca é concluído duas vezes.
- **Contexto**: cada job leva o dossiê completo (pedido, spec, regras do arquiteto) e o feedback recebido; o grafo do manager tem checkpoint por pedido; a **memória do time** (MCP `memory_put` / `memory_search`, por id do pedido) guarda a saída de cada agente para os seguintes.
- **Conhecimento via MCP**: antes de responder, o worker consulta o hub com a técnica adequada ao papel e acrescenta ao prompt em `## Knowledge`:

  | Papel | Ferramenta | Por quê |
  |---|---|---|
  | architect | `kag_query` | relações: o que protege o quê, o que um pagamento toca |
  | engenheiros | `rag_answer` | recuperar padrões de código por similaridade (vetor + BM25) |
  | specialist, QA | `cag_answer` | checklists pequenos e estáveis, inteiros no contexto |
  | PO, leader | `knowledge_answer` | o roteador escolhe RAG, CAG ou KAG |

  As mesmas ferramentas também são dadas aos agentes, então um modelo real pode chamá-las.

## Escolha o modelo de cada agente

Defina `AGENT_MODEL_<PAPEL>=provedor:modelo` (papéis: `MANAGER PO ARCHITECT LEADER JUNIOR MID SENIOR SPECIALIST QA KNOWLEDGE`). Nomes Anthropic podem ser curtos: `anthropic:opus_5_5` vira `anthropic:claude-opus-5-5`. Ordem: `AGENT_MODEL_<PAPEL>`, depois `AGENT_MODEL`, depois padrões por papel para a chave definida (Anthropic: Opus para PO e arquiteto, Sonnet para manager, leader, senior e specialist, Haiku para o resto; OpenAI: `gpt-4o-mini`), senão o modelo offline roteirizado. Chaves: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, ... veja `.env.example`. `KNOWLEDGE_EMBEDDINGS=provedor:modelo` troca os embeddings offline.

```bash
AGENT_MODEL_PO=anthropic:opus_5_5 AGENT_MODEL_JUNIOR=anthropic:haiku_5_5 AGENT_MODEL_SENIOR=openai:gpt-4o \
  ANTHROPIC_API_KEY=... OPENAI_API_KEY=... go run ./examples/langgraph/independent_engineering_team
```

## Como aplicar

- Edite uma persona em `skills/<nome>/SKILL.md`. Cada uma define o formato de saída que o manager lê (`TASK|id|título|complexidade|desc`, `VERDICT:`, `RISK:`); mude os dois juntos.
- Mude o conhecimento: `kb/*.md` alimenta o RAG (todos) e o CAG (os dois checklists); o grafo KAG é `triples` em `internal/team/knowledge.go`.
- Mude o time: adicione um worker no `docker-compose.yml` (`WORKER_ID`, `WORKER_ROLE`) e em `Cast` (`internal/team/local.go`). Nada mais conhece o tamanho do time.
- Mude o fluxo: `internal/team/manager.go`.

## Execute

Um só processo, offline (cada serviço em sua porta loopback, HTTP real, modelo roteirizado):

```bash
go run ./examples/langgraph/independent_engineering_team
go test -race ./examples/langgraph/independent_engineering_team/...   # reserva atômica, leases, conhecimento via MCP, entrega completa
```

Serviços separados, local ou em Docker:

```bash
cp examples/langgraph/independent_engineering_team/.env.example examples/langgraph/independent_engineering_team/.env
F=examples/langgraph/independent_engineering_team/docker-compose.yml
docker compose -f $F up --build -d          # hub, manager e 11 workers; a ordem não importa
docker compose -f $F run --rm client        # envia o pedido de exemplo, imprime relatório e histórico
curl localhost:8000/state                   # workers, reservas, fila
docker compose -f $F stop senior-1          # derrube um worker no meio: o job vai para outro
docker compose -f $F down
```

Portas do host ocupadas? Use `HUB_PORT` e `MANAGER_PORT`. Sem Docker: `go run ./examples/langgraph/independent_engineering_team/cmd/hub`, depois `cmd/manager` e um `cmd/worker` por papel (`WORKER_ID`, `WORKER_ROLE`, `HUB_URL`, `LISTEN`), depois `cmd/client`.

## Teste em modo produção

1. Ponha chaves reais no `.env` e rode o cliente. A estrutura se mantém (todas as tarefas `approved` ou `blocked`); texto e rodadas de revisão variam. Erro de parse indica um modelo que ignorou o formato de uma skill: reforce essa skill.
2. Fixe os modelos por agente como acima e compare custo e qualidade por papel.
3. Derrube um worker durante a execução e acompanhe `curl localhost:8000/state`: após `HUB_LEASE_TTL` o job muda de mãos e a execução termina.
4. O hub é uma instância com estado em memória (nota `ponytail:` em `hub.go`). Para replicá-lo, mantenha as mesmas transições como compare-and-swap num store compartilhado. Troque também `checkpoint.NewMemory()` por um saver durável e os stores em memória por backends de `integrations/`.
5. Os engenheiros devolvem código como texto. Execute-o em sandbox antes da revisão (`kubernetes.Sandbox`, `examples/integrations/remote_sandbox`).
6. Defina `MANAGER_API_KEY` e ponha o hub em rede privada: hub e workers não têm autenticação.

## Veja também

- `examples/langgraph/engineering_team`: o mesmo fluxo em um processo
- [Manual, capítulo 7: LangGraph](../../../docs/pt-BR/manual/07-langgraph.md), [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
