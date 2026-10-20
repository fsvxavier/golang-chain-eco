# Open SWE em Go (`openswe/`)

[English](../en-US/OPENSWE.md) | **Português** | [Español](../es-ES/OPENSWE.md)

Port das ideias do [Open SWE](https://github.com/langchain-ai/open-swe) (MIT): uma "fábrica de software" sobre um deep agent. Ela **investiga** um repositório, **implementa** a mudança num sandbox isolado por thread, **valida** e **entrega um pull request**; **revisa** PRs com findings ancorados no diff e os reconcilia a cada push; **vigia o CI** até ficar verde; e é acionada por **menções no GitHub, PRs novos, agendas e automações**.

Tutorial: [manual/19-open-swe.md](manual/19-open-swe.md). Exemplos: [`examples/openswe/`](../../examples/openswe/). Prompts adaptados do projeto original (MIT); o código é uma reescrita em Go sobre este ecossistema (`deepagents`, `agents`, `langgraph`, `mcp`, `langmem`, `ciam`).

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
| `openswe/slack` | cliente Web API (`chat.postMessage`, `chat.update`, `reactions.add`), Events API com assinatura v0, `Notifier` que devolve a resposta à thread | integração Slack |
| `openswe/linear` | cliente GraphQL (issue, `commentCreate`), webhook com `Linear-Signature`, `Notifier` que comenta no issue | integração Linear |

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

`triggers.Webhook` valida `X-Hub-Signature-256`, responde 202 e processa em segundo plano (eventos do mesmo issue/PR na ordem de chegada, no processo); entrega repetida (`X-GitHub-Delivery`) roda uma vez. Roteamento: menção ao bot em issue/PR/comentário → thread de código do item (`ThreadID` determinístico: o mesmo PR cai sempre na mesma thread e reusa contexto e sandbox); PR novo/pronto em repositório habilitado → revisão (e `Watch` para re-revisões em pushes); resposta humana a um comentário do revisor → reavaliação; `check_run`/`check_suite` → baby-sit; tudo também dispara automações. Proteções: eventos do próprio bot são ignorados; em repositório **público** só organizações em `AllowedOrgs` podem acionar o agent; todo texto externo chega ao agent dentro de `<untrusted_...>` (tag de fechamento no texto é neutralizada).

`triggers.Dispatcher`: **uma run por thread**. Mensagem para thread ocupada vai para a fila (`guards.Queue`); `guards.QueueMiddleware` a injeta antes da próxima chamada de modelo da run em andamento e, o que sobrar no fim, inicia uma nova run — nada se perde.

Automações: `Automation{Prompt, Repo, Triggers: [{schedule, cron}|{github, issues.opened|pull_request.opened|closed|merged}]}` em `AutomationStore`; `Scheduler.Tick/Run` dispara cada minuto de cron **uma vez** (claim por automação+minuto, seguro com várias réplicas; recuperação limitada após pausa).

## Slack e Linear

Os dois entram pelo mesmo `triggers.Dispatcher` que o GitHub: cada evento vira um `triggers.Task` com `Kind: KindAgent`, e **uma thread externa é uma thread do openswe** (`triggers.ThreadKey`, chaveado por `slack:<canal>:<thread_ts>` ou `linear:issue:<id>`), então contexto e sandbox são reaproveitados como no GitHub.

```go
d := &triggers.Dispatcher{
    Runners: map[string]triggers.Runner{triggers.KindAgent: // notifiers encadeados; cada um passa adiante o que não é seu
        slack.Notifier{Client: slack.NewClient(os.Getenv("SLACK_BOT_TOKEN")),
            Inner: linear.Notifier{Client: linear.NewClient(os.Getenv("LINEAR_API_KEY")),
                Inner: openswe.AgentRunner{Graph: agent}}}},
    Queue: &guards.MemoryQueue{}, // sem fila, mensagem para thread ocupada é erro
}
slackH := &slack.Handler{SigningSecret: os.Getenv("SLACK_SIGNING_SECRET"), BotUserID: "U0BOT",
    Channels: []string{"C0CODE"}, Dispatcher: d} // Channels é obrigatório: vazio ignora tudo
linearH := &linear.Webhook{Secret: os.Getenv("LINEAR_WEBHOOK_SECRET"), Dispatcher: d,
    BotUserID: "linear-user-id", BotName: "openswe", TriggerLabel: "openswe"}
mux.Handle("/slack/events", slackH)
mux.Handle("/linear/webhook", linearH)
```

**Slack** (`openswe/slack`)

- `Handler` verifica `X-Slack-Signature` (`v0=` + HMAC-SHA256 de `v0:<timestamp>:<corpo>`, comparação em tempo constante) e recusa timestamps com mais de 5 minutos de diferença. `url_verification` recebe o `challenge`. Vetor de assinatura e envelope `event_callback` conferidos com a documentação do Slack; sem teste contra um workspace real.
- `app_mention` num canal configurado inicia a run; a thread é `thread_ts`, ou o próprio `ts` quando a menção começa a thread.
- Mensagens com `subtype` são ignoradas (bots, edições, entradas no canal), exceto `thread_broadcast` e `file_share`, que são de usuário.
- Resposta em thread só vira follow-up se mencionar o bot **ou** se a thread já foi acionada por este processo. Esse conhecimento fica em memória: após reinício, é preciso mencionar o bot de novo.
- Entregas repetidas (mesmo `event_id`, inclusive com `X-Slack-Retry-Num`) e o par `app_mention` + `message` do mesmo `ts` rodam uma vez. O handler responde `200` sempre; falhas de despacho vão para `OnError` (o Slack não reenvia).
- `Notifier` posta a **resposta final do agente** na thread ao fim da run (ou "The run failed: …"). Menções que pingam (`<!channel>`, `<@U…>`) são neutralizadas antes do envio. `Describe` acrescenta, por exemplo, o link do PR (`PRLinks.ForThread`).
- O texto de quem escreveu no Slack vai dentro de `<untrusted_slack author="…">`.

**Linear** (`openswe/linear`)

- `Webhook` verifica `Linear-Signature` (HMAC-SHA256 hex do corpo cru) e recusa `webhookTimestamp` com mais de 60 s de diferença.
- Um issue criado ou atualizado **atribuído ao bot** (`BotUserID`) ou com o label `TriggerLabel` inicia a run. Em `update`, só conta quando `updatedFrom` traz `labelIds` ou `assigneeId`. Limite conhecido: se outro label mudar no mesmo evento, o label já presente também dispara uma nova run (que vai para a fila da thread, sem se perder).
- Comentário `create` que menciona `@BotName` (ou o link `mention://user/<BotUserID>`), escrito por outra conta, inicia a run na thread do issue. O autor vem de `data.user.id`, `data.userId` ou `actor.id` (o exemplo oficial traz só `userId`).
- Entrega repetida (`Linear-Delivery`) roda uma vez.
- `Notifier` comenta a resposta final no issue (`commentCreate`).
- Campos conferidos com o exemplo de payload da documentação do Linear e com os tipos `IssueWebhookPayload`, `CommentWebhookPayload` e `EntityWebhookPayload` do schema GraphQL público (`linear/linear`, `packages/sdk/src/schema.graphql`); as queries do cliente usam campos desse schema. Não houve teste contra um workspace real.
- `linear.Client` envia a chave pessoal direto no header `Authorization`; para um token OAuth, defina `AuthPrefix: "Bearer "`. Erros GraphQL (`errors`) viram `*linear.APIError`, inclusive com HTTP 200.

Exemplo offline (Slack falso, GitHub falso, modelo roteirizado): [`examples/openswe/slack_trigger`](../../examples/openswe/slack_trigger/).

## Sandbox

`sandbox.Manager`: um sandbox por thread, guardado (provedor + id) no `store.Store`. Provedores: `Local` (diretório no host, `backends.LocalShell`: **sem isolamento**; registra um aviso no primeiro comando, sem `AllowedCommands`) e `Kubernetes` (pod, via `integrations/kubernetes`); para qualquer outro ambiente que execute comandos (SSH, Docker, nuvem), implemente `sandbox.Provider` sobre `backends.Remote`. Regra herdada do original: sandbox que não responde **não é substituído em silêncio** (`ErrUnreachable`, "nenhuma substituição foi iniciada"): um novo estaria vazio e jogaria fora trabalho não publicado; só `Replace` (ato deliberado) cria outro. `PrepareRepo` clona/atualiza, define a identidade de commit, entra na branch de trabalho **sem** resetar nem limpar o que uma run anterior deixou, e guarda o token numa credencial `0600` (nunca na URL do remote).

## Configuração e produção

- **Token do GitHub**: `github.App{AppID, Key}` (JWT RS256 → token de instalação com cache) ou `StaticToken`. Dê ao sandbox um token **sem** permissão de criar PR se quiser que `PRCreationGuard` seja mais que um guarda-corpo.
- **Persistência**: `store.Store` para findings, vigias, automações, links PR↔thread, estilos e ids de sandbox (`sqlstore`/Redis/Mongo em produção); `checkpoint.Saver` para o estado dos agents.
- **Limites**: `ModelTimeout` (5 min), `RunLimit` (300 chamadas de modelo) e `RecursionLimit` (2000 superpassos — cada chamada de modelo custa vários por causa dos hooks de middleware) por run.
- **Servir como API**: os grafos do engenheiro e do revisor podem ser publicados com [`agentprotocol`](AGENT-PROTOCOL.md) (`config.configurable` carrega `source`, `repo`, `owner`).
- **Contexto do projeto**: [`ciam`](CIAM.md) como `Config.Context`.

## O que **não** foi portado

Declarado, não escondido:

- **Slack**: botões interativos, cartões de revisão humana, revisão expedita por cartões e canais de código dedicados (o bot responde em qualquer canal da lista `Channels`, sem canal próprio por repositório).
- **Linear**: projetos, ciclos, sincronização de estados do issue e criação de issues pelo agente. Apenas issue (criação/atualização com label ou atribuição) e comentários com menção são acionadores.
- **Dashboard/UI**, app desktop, CLI e *bridge* local.
- **Contas e autorização**: OAuth, perfis, "act as", administração de workspaces, feature flags, trilha de auditoria, analytics e custo por run. Os pacotes recebem identidade (`Requester`, `Approver`) de quem os integra.
- **Persistência em PostgreSQL** das tabelas do produto (usa `store.Store` genérico).
- **Runtime Managed Deep Agents** / hospedagem na LangSmith Platform; provedor de sandbox LangSmith (Daytona/E2B/Modal/Runloop/snapshots): use `sandbox.Provider` próprio.
- `review-scout`, `review-guide`, *walkthrough* de PR, *incidents*, skill store, seleção automática de modelo, resumo/título de thread, middleware específicos de provedor (sanitização de mensagens por vendor).
- **Garantias dos guards** (guarda-corpos, não fronteira de segurança): `PRCreationGuard` e `ReadOnlyGuard` inspecionam cada comando, inclusive o texto de `bash -c`/`eval`, e acompanham por run os arquivos que o agent escreve (`write_file`, `edit_file`, `> f`, `>> f`, `tee f`, heredoc e `echo`/`printf` redirecionados). Quando um comando executa esse arquivo (`sh f`, `bash f`, `./f`, `source f`, `. f`, `python[3] f`, `node f`, `chmod +x f && ./f`), o último conteúdo gravado é inspecionado com as mesmas regras, até 3 níveis de script. Também são bloqueados, de forma direta, pipelines de download ou decodificação para shell (`base64 … | sh`, `curl … | bash`, `wget … | sudo sh`, `sh <(curl …)`, `bash -c "$(curl …)"`). Um arquivo gravado por uma run só afeta essa run.
  - **Não cobertos**: conteúdo produzido por outro comando (`cp`, `sed -i`, `python -c`, `git checkout`, `cat origem > f`), `xargs`/`find -exec`, arquivos alterados fora das tools, e payloads montados em tempo de execução. Esses casos dependem da fronteira abaixo.
  - **Fronteira real**: (1) o **token** usado pelo sandbox não tem permissão de criar PR nem de escrever no GitHub (revisor: somente leitura); (2) o **sistema de arquivos do sandbox é somente-leitura** para o revisor, exceto `/review/**`.
  - **Como configurar (2)**: `openswe/sandbox` hoje não tem opção de montagem somente-leitura; o pod do provedor `Kubernetes` é criado por `integrations/kubernetes.NewSandbox` com a spec padrão. Para um revisor, implemente um `sandbox.Provider` próprio que crie o pod com `securityContext.readOnlyRootFilesystem: true` e um `emptyDir` gravável apenas em `/review`, e envolva-o com `backends.Remote`.

## Testes

`go test -race ./openswe/...`, 100% offline (GitHub e CI simulados com `httptest`, git real em diretórios temporários): Slack (assinatura v0 com vetor fixo, janela de 5 min, `url_verification`, menção e follow-up por thread, retentativas e duplicatas, resposta final e neutralização de menções) e Linear (`Linear-Signature` com vetor fixo, janela de 60 s, atribuição e label, menção de comentário, entrega repetida, comentário com erro GraphQL, payload de comentário copiado da documentação oficial), envelope `event_callback` e `thread_broadcast` do Slack conforme a documentação; cliente GitHub (idempotência de PR, JWT de App com verificação de assinatura, cache de token, webhooks), prompts, parsing de shell e todos os guards (incluindo `git push` real com workflows), sandbox (persistência, não-substituição, roteamento por thread, clone/continuação sem perder trabalho, token fora da URL), revisor (âncoras, dedupe, ranking, aprovação, reconciliação, estilo), baby-sit (dedupe de wakes, orçamento de re-runs, webhooks repetidos, falhas de API), gatilhos (cron, claims, fila, segurança de webhooks, ordem de eventos por PR, automações) e ponta a ponta: engenheiro entregando PR, revisor publicando uma vez e sem poder escrever, menção → run com follow-up na mesma run.
