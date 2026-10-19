# Open SWE em Go (`openswe/`)

Port das ideias do [Open SWE](https://github.com/langchain-ai/open-swe) (MIT): uma "fábrica de software" sobre um deep agent. Ela **investiga** um repositório, **implementa** a mudança num sandbox isolado por thread, **valida** e **entrega um pull request**; **revisa** PRs com findings ancorados no diff e os reconcilia a cada push; **vigia o CI** até ficar verde; e é acionada por **menções no GitHub, PRs novos, agendas e automações**.

Tutorial: [manual/19-open-swe.md](manual/19-open-swe.md). Exemplos: [`examples/openswe/`](../examples/openswe/). Prompts adaptados do projeto original (MIT); o código é uma reescrita em Go sobre este ecossistema (`deepagents`, `agents`, `langgraph`, `mcp`, `langmem`, `ciam`).

## Pacotes

| Pacote | Faz | Origem (open-swe) |
|---|---|---|
| `openswe` | `NewAgent` (engenheiro), `ReviewerRunner`, `AgentRunner`, `PRTools`, `PRLinks` | `server.py`, `reviewer.py`, `tools/open_pull_request.py` |
| `openswe/prompts` | prompts como arquivos embutidos (sistema, revisor, subagent) | `resources/prompts/**`, `prompt.py` |
| `openswe/github` | cliente REST/GraphQL, auth de GitHub App, parse e assinatura de webhooks | `github/**` |
| `openswe/sandbox` | um sandbox persistente por thread; `ErrUnreachable`; preparo do repositório | `sandboxes/**` |
| `openswe/guards` | middleware das regras (PRs só pela tool, push de workflow com aprovação, revisor somente-leitura, reparo de tool call órfã, disjuntor de sandbox, fila de follow-ups) | `middleware/**` |
| `openswe/review` | findings ancorados no diff, publicação, reconciliação com threads, estilo por repositório | `review/**`, `tools/add_finding.py`, `publish_review.py` |
| `openswe/babysit` | vigia de CI durável, sem gastar tokens parado; re-run de flaky com orçamento | `baby_sit.py`, `bundled_skills/baby-sit` |
| `openswe/triggers` | webhook, despacho "uma run por thread", cron, automações | `webhooks/**`, `schedules/**`, `dispatch.py` |

## O engenheiro

```go
agent, _ := openswe.NewAgent(openswe.Config{
    Model: model, GitHub: ghClient, Sandbox: sandboxManager,
    Links: &openswe.PRLinks{S: st}, DraftPRs: true, AllowedOrgs: []string{"acme"},
    Babysit: watcher, Dispatcher: dispatcher, Queue: queue, Approver: approver,
    Context: &ciam.Toolkit{M: ciamManager, Project: "/workspace/api"}, // opcional
})
```

É um `deepagents.Create` com: backend resolvido **por thread** (`sandbox.Manager.Backend()`), tools de entrega (`open_pull_request`, `link_pull_request`, `github_comment`, `request_pr_review`), `fetch_url` (com proteção de SSRF), tools de baby-sit, subagent `researcher` somente-leitura, e a pilha de *guards*. O **prompt de sistema é renderizado a cada chamada** a partir da origem da run (`github`, `schedule`, `cli`), do repositório e do `AGENTS.md`/`CLAUDE.md` lido do sandbox (cache de 5 min), das regras da workspace e das suas instruções.

### As regras de entrega (no prompt **e** no código)

1. PR é a entrega padrão de qualquer mudança de código; `open_pull_request` só depois do `git push`.
2. `open_pull_request` é **idempotente**: se já existe PR aberto para a branch, devolve esse (não duplica); recusa branch não publicada com a instrução de dar push; acrescenta *References* (link da thread) e a atribuição; aplica a política de rascunho (`DraftPRs`).
3. **Nunca** `gh pr create`, `gh api .../pulls` (POST), `curl` ao endpoint de pulls ou `hub pull-request`: `PRCreationGuard` bloqueia (inclusive dentro de `bash -c`, `eval`, `env X=1 ...`).
4. Falha de acesso (403/404/"App não instalado") → a tool devolve "não tente outro caminho; reporte e encerre".
5. **Nunca** marcar rascunho como pronto (`request_pr_review` recusa PR em rascunho e manda pedir ao autor), **nunca** aprovar PR como si mesmo, sem force-push sem autorização.
6. `git push` que muda `.github/workflows/` precisa de **aprovação humana** presa ao diff exato (`WorkflowPushGuard`); sem `Approver` configurado, é negado.

## O revisor

`ReviewerRunner` (um `triggers.Runner`): lê o PR (head/base), carrega o estilo do repositório, monta uma `review.Session`, **reconcilia** o que aconteceu no GitHub desde a última revisão, renderiza o prompt (primeira revisão, re-revisão ou reavaliação de resposta) e roda um agent **somente-leitura** (escrita só em `/review/**`, `ReadOnlyGuard` no shell).

Tools: `fetch_review_diff` (escreve o diff num arquivo do sandbox), `add_finding`, `list_findings`, `update_finding`, `resolve_finding_thread`, `publish_review`.

- **Ancoragem**: `add_finding` rejeita linhas fora do diff do PR (sempre o diff **completo** base..head, mesmo numa re-revisão incremental). Título ≤ 120, sugestão ≤ 4 linhas (maior é descartada, a descrição fica), duplicata (mesma *fingerprint*: arquivo + categoria + título normalizado) é fundida.
- **`publish_review`** (uma vez, sozinha): `ranking` tem de listar **exatamente** os findings novos (senão devolve `expected_finding_ids`); âncora que saiu do diff vira `unresolvable_findings`; re-revisão sem nada novo, ou revisão vazia sobre um head já revisado, vira `skipped_empty_re_review` (sem spam); `dry_run` valida sem postar. Posta **uma** review com comentários inline (`line`/`start_line`/`side`) e um marcador oculto por finding, e liga cada comentário ao finding e à *thread* do GitHub.
- **Aprovação**: modo por repositório (`off`, `dry_run` padrão, `approve`). Só aprova se o modo for `approve`, a avaliação for positiva, **não houver finding aberto de severidade ≥ medium** e o head não tiver mudado durante a revisão.
- **Reconciliação** (`Session.Reconcile`): thread resolvida por humano fecha o finding; respostas humanas viram interações e disparam uma **reavaliação focada** (`resolve_finding_thread` posta a nota como resposta e resolve a thread).
- **Estilo aprendido**: `review.LearnStyle` destila, com o seu modelo, as diretrizes dos comentários **humanos** de PRs recentes mergeados e as guarda em `Style.CustomPrompt` (revisão automática é **opt-in** por repositório: `Style.Enabled`).

## CI: baby-sit

`babysit.Watcher.Start` abre uma vigia (não abre se o PR está fechado ou o CI já passou). A vigia reage ao webhook `check_run/check_suite` e a um *poll* determinístico (`Tick`/`Run`, 10 min no original) — **nenhum dos dois chama um modelo**. O agent só é acordado (`Dispatcher.Wake`) quando: há falha nova (uma vez por `head + conjunto de checks falhando`), tudo ficou verde (encerra), o PR fechou/mergeou, ou o acesso ao GitHub falhou 3 vezes. Se a entrega do wake falha, a chave é esquecida e a próxima avaliação tenta de novo.

`rerun_flaky_check` re-executa **só os jobs falhos** de um check do **GitHub Actions** (nunca os demais provedores), exige evidência, só no head vigiado, no máximo **3 por head**; permissão negada não consome orçamento. A skill `babysit.Skill` (SKILL.md embutido) ensina o agent a diagnosticar e a decidir entre determinístico, flaky, ambíguo e externo.

## Gatilhos

`triggers.Webhook` valida `X-Hub-Signature-256`, responde 202 e processa em segundo plano; entrega repetida (`X-GitHub-Delivery`) roda uma vez. Roteamento: menção ao bot em issue/PR/comentário → thread de código do item (`ThreadID` determinístico: o mesmo PR cai sempre na mesma thread e reusa contexto e sandbox); PR novo/pronto em repositório habilitado → revisão (e `Watch` para re-revisões em pushes); resposta humana a um comentário do revisor → reavaliação; `check_run`/`check_suite` → baby-sit; tudo também dispara automações. Proteções: eventos do próprio bot são ignorados; em repositório **público** só organizações em `AllowedOrgs` podem acionar o agent; todo texto externo chega ao agent dentro de `<untrusted_...>` (tag de fechamento no texto é neutralizada).

`triggers.Dispatcher`: **uma run por thread**. Mensagem para thread ocupada vai para a fila (`guards.Queue`); `guards.QueueMiddleware` a injeta antes da próxima chamada de modelo da run em andamento e, o que sobrar no fim, inicia uma nova run — nada se perde.

Automações: `Automation{Prompt, Repo, Triggers: [{schedule, cron}|{github, issues.opened|pull_request.opened|closed|merged}]}` em `AutomationStore`; `Scheduler.Tick/Run` dispara cada minuto de cron **uma vez** (claim por automação+minuto, seguro com várias réplicas; recuperação limitada após pausa).

## Sandbox

`sandbox.Manager`: um sandbox por thread, guardado (provedor + id) no `store.Store`. Provedores: `Local` (diretório no host: **sem isolamento**) e `Kubernetes` (pod, via `integrations/kubernetes`); para qualquer outro ambiente que execute comandos (SSH, Docker, nuvem), implemente `sandbox.Provider` sobre `backends.Remote`. Regra herdada do original: sandbox que não responde **não é substituído em silêncio** (`ErrUnreachable`, "nenhuma substituição foi iniciada"): um novo estaria vazio e jogaria fora trabalho não publicado; só `Replace` (ato deliberado) cria outro. `PrepareRepo` clona/atualiza, define a identidade de commit, entra na branch de trabalho **sem** resetar nem limpar o que uma run anterior deixou, e guarda o token numa credencial `0600` (nunca na URL do remote).

## Configuração e produção

- **Token do GitHub**: `github.App{AppID, Key}` (JWT RS256 → token de instalação com cache) ou `StaticToken`. Dê ao sandbox um token **sem** permissão de criar PR se quiser que `PRCreationGuard` seja mais que um guarda-corpo.
- **Persistência**: `store.Store` para findings, vigias, automações, links PR↔thread, estilos e ids de sandbox (`sqlstore`/Redis/Mongo em produção); `checkpoint.Saver` para o estado dos agents.
- **Limites**: `ModelTimeout` (5 min), `RunLimit` (300 chamadas de modelo) e `RecursionLimit` (2000 superpassos — cada chamada de modelo custa vários por causa dos hooks de middleware) por run.
- **Servir como API**: os grafos do engenheiro e do revisor podem ser publicados com [`agentprotocol`](AGENT-PROTOCOL.md) (`config.configurable` carrega `source`, `repo`, `owner`).
- **Contexto do projeto**: [`ciam`](CIAM.md) como `Config.Context`.

## O que **não** foi portado

Declarado, não escondido:

- Integrações **Slack** (conversas, botões, canais de código, revisão humana e revisão expedita por cartões) e **Linear**.
- **Dashboard/UI**, app desktop, CLI e *bridge* local.
- **Contas e autorização**: OAuth, perfis, "act as", administração de workspaces, feature flags, trilha de auditoria, analytics e custo por run. Os pacotes recebem identidade (`Requester`, `Approver`) de quem os integra.
- **Persistência em PostgreSQL** das tabelas do produto (usa `store.Store` genérico).
- **Runtime Managed Deep Agents** / hospedagem na LangSmith Platform; provedor de sandbox LangSmith (Daytona/E2B/Modal/Runloop/snapshots): use `sandbox.Provider` próprio.
- `review-scout`, `review-guide`, *walkthrough* de PR, *incidents*, skill store, seleção automática de modelo, resumo/título de thread, middleware específicos de provedor (sanitização de mensagens por vendor).
- **Garantias dos guards**: `PRCreationGuard`/`ReadOnlyGuard` leem a linha de comando; um script gravado em arquivo e executado depois não é inspecionado. São guarda-corpos, não fronteira de segurança.

## Testes

`go test -race ./openswe/...`, 100% offline (GitHub e CI simulados com `httptest`, git real em diretórios temporários): cliente GitHub (idempotência de PR, JWT de App com verificação de assinatura, cache de token, webhooks), prompts, parsing de shell e todos os guards (incluindo `git push` real com workflows), sandbox (persistência, não-substituição, roteamento por thread, clone/continuação sem perder trabalho, token fora da URL), revisor (âncoras, dedupe, ranking, aprovação, reconciliação, estilo), baby-sit (dedupe de wakes, orçamento de re-runs, webhooks repetidos, falhas de API), gatilhos (cron, claims, fila, segurança de webhooks, automações) e ponta a ponta: engenheiro entregando PR, revisor publicando uma vez e sem poder escrever, menção → run com follow-up na mesma run.
