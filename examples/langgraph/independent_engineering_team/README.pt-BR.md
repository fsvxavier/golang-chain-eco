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

## Status das tarefas e notificações

Toda tarefa (e o pedido) tem sempre um de 13 status: `received`, `in_specification`, `risk_sec_review`, `task_assignment`, `in_development`, `in_code_review`, `in_qa_testing`, `qa_approved`, `qa_rejected`, `review_approved`, `review_rejected`, `committed`, `completed`. O agente que está com o trabalho é registrado em cada movimentação.

```
request: received -> in_specification -> risk_sec_review -> task_assignment -> completed
task:    task_assignment -> in_development -> in_code_review -> review_approved -> in_qa_testing -> qa_approved -> committed -> completed
         in_code_review -> review_rejected -> in_development      in_qa_testing -> qa_rejected -> in_development
```

`examples/internal/tasktrack` valida cada movimentação contra esta tabela (uma tarefa não chega a `committed` sem revisão e QA; movimentação ilegal é logada e não registrada). Uma tarefa que esgota as 3 rodadas fica na rejeição que a parou. Aqui `committed` significa que o código aprovado foi registrado como entrega da tarefa; uma execução real commita antes no sandbox.

Cada movimentação é anunciada por `NOTIFY` (padrão `console`): `console`, `slack` (`SLACK_WEBHOOK_URL`), `email` (`SMTP_ADDR`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`, `MAIL_TO`) e `chat` (`CHAT_WEBHOOK_URL`, qualquer webhook que aceite `{"text": "...", "event": {...}}`), separados por vírgula. `NOTIFY_STATUSES=review_rejected,qa_rejected,completed` limita slack, email e chat a esses status; o console mostra todos. Um canal que falha é logado e nunca trava o trabalho.

```bash
NOTIFY=console,slack SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... NOTIFY_STATUSES=qa_rejected,completed go run ./examples/langgraph/independent_engineering_team
```

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

## Ponte para chats (MCP)

`cmd/bridge` é um servidor MCP em stdio. Os chats e consoles o iniciam sozinhos e falam com ele por stdin/stdout, então nada precisa escutar numa porta. Ele alcança a stack em execução por três variáveis.

### 1. Compile o servidor uma vez

```bash
go build -o bin/bridge ./cmd/bridge
```

Use um binário em vez de `go run`: inicia na hora e não imprime nada além do protocolo no stdout. Abaixo, `/caminho/abs/para/bridge` é o arquivo que você acabou de compilar.

### 2. Variáveis

| Variável | Padrão | Uso |
|---|---|---|
| `MANAGER_URL` | `http://localhost:8080` | manager (Agent Server API) |
| `MANAGER_API_KEY` | vazio | chave, quando o manager define uma |
| `HUB_URL` | `http://localhost:8000` | hub (fila, `GET /state`, memória do time) |
| `BRIDGE_CIAM_CMD` | vazio | comando do servidor de contexto do projeto, ex.: `/abs/caminho/ciam mcp` |
| `BRIDGE_KNOWLEDGE_CMD` | vazio | comando do servidor de conhecimento, ex.: `/abs/caminho/knowledge mcp --dir /abs/.knowledge` |

### 3. Ferramentas

`team_submit` envia uma demanda e devolve o `thread_id` na hora; `team_status` mostra status das tarefas, relatório e histórico; `team_queue` lista workers e fila; `team_ask_worker` pergunta a um worker via Agent Protocol; `team_memory` lê a memória do time.

Além do time, o mesmo servidor oferece:

- `graph_run`, `graph_state`, `graph_resume`: rodar qualquer grafo em qualquer Agent Server (`assistant_id`, `url` opcional, `wait`), ler estado e interrupções de uma thread e responder a uma interrupção.
- as ferramentas `ciam_*` (busca no projeto, ADR/PRD/planos, memória), quando `BRIDGE_CIAM_CMD` está definido.
- as ferramentas `knowledge_*` (RAG, CAG, KAG), quando `BRIDGE_KNOWLEDGE_CMD` está definido. Rode antes `knowledge index <path> --kind both`; sem índice esse servidor não sobe e a ponte registra o erro e segue sem ele.

Coloque essas duas variáveis no `env` do registro abaixo, com caminhos absolutos (`go build -o bin/ciam ./cmd/ciam`, `go build -o bin/knowledge ./cmd/knowledge`).

### 4. Registre em cada aplicação

Coloque segredos (`MANAGER_API_KEY`) no cofre da própria ferramenta ou no ambiente do shell, não num arquivo versionado. Os formatos de configuração mudam entre versões; se uma chave for rejeitada, confira a documentação MCP da ferramenta.

#### GitHub Copilot (VS Code)

Requer VS Code com Copilot Chat em modo Agent. Crie `.vscode/mcp.json` no workspace (ou rode **MCP: Open User Configuration** para valer em todos):

```json
{
  "inputs": [
    { "type": "promptString", "id": "manager-key", "description": "MANAGER_API_KEY", "password": true }
  ],
  "servers": {
    "team-bridge": {
      "type": "stdio",
      "command": "/caminho/abs/para/bridge",
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/caminho/abs/para/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/caminho/abs/para/knowledge mcp --dir /caminho/abs/para/.knowledge",
        "MANAGER_API_KEY": "${input:manager-key}"
      }
    }
  }
}
```

Abra o arquivo e clique em **Start** acima do servidor (ou rode **MCP: List Servers**). No Copilot Chat escolha **Agent**, abra o seletor de ferramentas e habilite `team-bridge`. A entrada `inputs` faz o VS Code pedir a chave uma vez e mantê-la fora do arquivo.

#### Claude Code

Linha de comando (`--scope local` é o padrão; `project` grava `.mcp.json` para compartilhar com o time; `user` vale para todos os projetos):

```bash
claude mcp add --transport stdio --scope project \
  --env MANAGER_URL=http://localhost:8080 --env HUB_URL=http://localhost:8000 \
  --env BRIDGE_CIAM_CMD="/caminho/abs/para/ciam mcp" \
  --env BRIDGE_KNOWLEDGE_CMD="/caminho/abs/para/knowledge mcp --dir /caminho/abs/para/.knowledge" \
  team-bridge -- /caminho/abs/para/bridge
```

Ou escreva `.mcp.json` na raiz do projeto à mão:

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/caminho/abs/para/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/caminho/abs/para/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/caminho/abs/para/knowledge mcp --dir /caminho/abs/para/.knowledge"
      }
    }
  }
}
```

Confira com `claude mcp list` ou `/mcp` dentro da sessão. Servidores de escopo projeto pedem aprovação na primeira vez.

#### Cursor

Crie `.cursor/mcp.json` no projeto (ou `~/.cursor/mcp.json` para todos os projetos):

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/caminho/abs/para/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/caminho/abs/para/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/caminho/abs/para/knowledge mcp --dir /caminho/abs/para/.knowledge"
      }
    }
  }
}
```

Abra **Settings → Cursor Settings → MCP**, ligue o servidor e espere o ponto verde. O Cursor lista as ferramentas ali; o agente as usa no modo Agent.

#### Kilo Code

O Kilo Code lê `mcpServers` de `.kilocode/mcp.json` no projeto (configuração global: **MCP Servers → Edit Global MCP** no painel do Kilo). Atenção: versões novas do Kilo, baseadas no Opencode, leem `kilo.json` com o mesmo formato do Opencode abaixo; isso não foi verificado aqui, então use o que a tela de MCP da sua versão mostrar.

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/caminho/abs/para/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/caminho/abs/para/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/caminho/abs/para/knowledge mcp --dir /caminho/abs/para/.knowledge"
      },
      "alwaysAllow": ["team_status", "team_queue", "team_memory"],
      "disabled": false
    }
  }
}
```

Abra o ícone **MCP Servers** no painel do Kilo para ver status e ferramentas. `alwaysAllow` deixa as ferramentas listadas rodarem sem confirmação; omita para aprovar cada chamada.

#### Opencode

Crie `opencode.json` no projeto (ou `~/.config/opencode/opencode.json` para todos os projetos):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "team-bridge": {
      "type": "local",
      "command": ["/caminho/abs/para/bridge"],
      "environment": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/caminho/abs/para/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/caminho/abs/para/knowledge mcp --dir /caminho/abs/para/.knowledge"
      },
      "enabled": true
    }
  }
}
```

Rode `opencode mcp list` para conferir. Na TUI as ferramentas aparecem como `team-bridge_team_submit` e assim por diante.

### 5. Use

Peça no chat, por exemplo: *"Use team_submit para enviar: permitir cupom de desconto no checkout"*, depois *"mostre o team_status dessa thread"* ou *"quem está no team_queue?"*. Suba a stack antes (`docker compose up`, veja acima).
