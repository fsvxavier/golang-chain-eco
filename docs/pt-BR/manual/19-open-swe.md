# 19. Open SWE: uma fábrica de software com agents

[English](../../en-US/manual/19-open-swe.md) | **Português** | [Español](../../es-ES/manual/19-open-swe.md)

O pacote `openswe` monta, sobre os capítulos anteriores, um sistema que transforma trabalho de engenharia em um processo repetível: **investigar → implementar num sandbox → validar → entregar um PR → revisar → vigiar o CI → responder ao feedback**. Referência, mapa para o projeto original e o que não foi portado: [docs/pt-BR/OPENSWE.md](../OPENSWE.md).

Pré-requisitos: capítulos 5, 9, 10 (tools, agents, deep agents), 8 (store e checkpoint) e 11 (MCP).

## 19.1 Visão geral

```
GitHub (issue, PR, comentário, CI) ─┐
agenda (cron) / automações ──────────┼─► triggers.Webhook / Scheduler ─► Dispatcher (1 run por thread)
terminal / API (agentprotocol) ──────┘                                        │
                                              ┌───────────────────────────────┤
                                              ▼                               ▼
                                   openswe.NewAgent (engenheiro)    openswe.ReviewerRunner
                                   sandbox por thread + guards      diff ancorado, findings, publish
                                   open_pull_request / babysit      reconciliação a cada push
```

Cada peça é um pacote que você pode usar sozinho: `github`, `sandbox`, `guards`, `review`, `babysit`, `triggers`, `prompts`.

## 19.2 Rode os exemplos primeiro

Tudo offline (GitHub simulado, modelo roteirizado, git real):

```bash
go run ./examples/openswe/engineer   # issue → sandbox → atalho bloqueado → PR rascunho → comentário
go run ./examples/openswe/reviewer   # review única, push recusado, re-revisão com reconciliação
go run ./examples/openswe/factory    # webhooks, fila, cron, claims e vigia de CI
```

## 19.3 Passo a passo para montar o seu

### 1. GitHub

```go
// GitHub App (recomendado): JWT RS256 → token de instalação com cache
gh := github.NewClient(&github.App{AppID: 12345, Key: pemBytes})
// ou um token fixo para começar:
gh = github.NewClient(github.StaticToken(os.Getenv("GITHUB_TOKEN")))
```

Permissões típicas do App: *Contents* (rw), *Pull requests* (rw), *Issues* (rw), *Checks* (r), *Actions* (rw, só se quiser re-run de CI), *Metadata* (r); eventos: `issues`, `issue_comment`, `pull_request`, `pull_request_review_comment`, `check_run`, `check_suite`.

### 2. Sandbox por thread

```go
sb := &sandbox.Manager{
	Providers: map[string]sandbox.Provider{"k8s": sandbox.Kubernetes{Client: k8s, Namespace: "agents", Image: "ghcr.io/acme/dev:latest"}},
	Default: "k8s", Store: st, // o store guarda thread→sandbox
}
```

`sandbox.Local{Base: "/var/lib/openswe"}` serve para desenvolvimento (sem isolamento!). Um sandbox que para de responder gera `sandbox.ErrUnreachable` e **não é trocado sozinho** — um novo estaria vazio e perderia trabalho não publicado; trocar é uma decisão explícita (`sb.Replace`).

Para preparar o repositório dentro dele: `sandbox.PrepareRepo(ctx, executor, sandbox.RepoSpec{Owner, Name, Branch: "openswe/<thread>"}, identidade, token)` — clona ou atualiza, define a identidade de commit e **preserva** o trabalho de runs anteriores.

### 3. O engenheiro

```go
agent, _ := openswe.NewAgent(openswe.Config{
	Model: model, GitHub: gh, Sandbox: sb, Checkpointer: saver, Store: st,
	Links: &openswe.PRLinks{S: st}, DraftPRs: true, AllowedOrgs: []string{"acme"},
	Queue: queue, Approver: approver, Babysit: watcher, Dispatcher: dispatcher,
})
```

Por run, o prompt de sistema é renderizado com a origem (`github`/`schedule`/`cli`), o `AGENTS.md` do repositório e as suas regras (`Workspace`, `CustomInstructions`). Os prompts são arquivos em `openswe/prompts/templates/` — leia-os: são a política do agent.

### 4. O revisor

```go
reviews := review.NewStore(st)
styles := &review.StyleStore{S: st}
styles.Put(ctx, review.Style{FullName: "acme/api", Enabled: true, Approval: review.ApprovalDryRun})
rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: styles, Announce: true}
```

A revisão automática é **opt-in por repositório** (`Style.Enabled`). Comece com `ApprovalDryRun` (padrão: a avaliação positiva vira só um comentário) e só mude para `ApprovalOn` depois de confiar nos resultados. Para o revisor "falar como os mantenedores": `style.CustomPrompt, _ = review.LearnStyle(ctx, model, gh.Repo("acme","api"), 30)` e salve.

### 5. Gatilhos

```go
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{
	triggers.KindAgent:    openswe.AgentRunner{Graph: agent},
	triggers.KindReviewer: rr,
}, Queue: queue}
wh := &triggers.Webhook{Secret: os.Getenv("GH_WEBHOOK_SECRET"), Dispatcher: d, Bot: "openswe", BotLogins: []string{"openswe[bot]"},
	AllowedOrgs: []string{"acme"}, Claims: claims, Reviews: reviews, Styles: styles, Babysit: watcher, Scheduler: sched}
http.Handle("/webhooks/github", wh)
go sched.Run(ctx, logErr)        // cron e automações
go watcher.Run(ctx, 10*time.Minute, logErr) // fallback do vigia de CI
```

`@openswe corrija isso` num comentário de issue → thread de código (sempre a mesma para o mesmo item: contexto e sandbox reaproveitados). Outro comentário enquanto a run está ocupada entra na **fila** e é visto pela run em andamento antes da próxima chamada de modelo.

### 6. Vigiar o CI

Peça ao agent (ele tem `manage_baby_sit`/`rerun_flaky_check` e a skill `babysit.Skill`): "`/baby-sit <URL do PR>`". A vigia **não gasta tokens** enquanto nada muda; só acorda o agent em falha nova, CI verde ou PR encerrado. Re-run só para falha **flaky com evidência**, só jobs falhos do GitHub Actions, no máximo 3 por commit.

### 7. Automações

```go
automations.Put(ctx, triggers.Automation{Name: "triagem semanal", Repo: "acme/api", Enabled: true,
	Prompt:   "Triague as issues abertas e aplique labels.",
	Triggers: []triggers.Trigger{{Kind: "schedule", Cron: "0 9 * * mon"}, {Kind: "github", Event: "issues.opened"}}})
```

Cron em UTC, 5 campos, cada minuto dispara **uma vez** mesmo com várias réplicas (claims).

## 19.4 As regras e como são impostas

| Regra | Onde |
|---|---|
| PR só por `open_pull_request` (idempotente, rascunho, atribuição) | `PRCreationGuard` + tool |
| Nunca desmarcar rascunho; nunca aprovar o próprio PR; sem force-push sem autorização | prompt + `request_pr_review` recusa rascunho |
| Push que altera `.github/workflows/` exige aprovação humana **ligada ao diff exato** | `WorkflowPushGuard` + `Approver` |
| Revisor não escreve, não faz commit nem push, não publica fora das tools | permissões de arquivo + `ReadOnlyGuard` |
| Texto de issue/PR/comentário/log é **dado**, nunca instrução | `<untrusted_...>` + prompt |
| Repositório público só aciona o agent para organizações permitidas | `Webhook.AllowedOrgs` |
| Tool call interrompida não trava a thread para sempre | `RepairOrphanedToolCalls` |
| Sandbox morto não consome o orçamento de passos nem some em silêncio | `SandboxCircuitBreaker` |

Tenha claro o alcance: os guards **leem o comando**; não inspecionam um script gravado em arquivo. Complemente com credenciais mínimas (um token de sandbox sem permissão de criar PR, por exemplo), isolamento real (Kubernetes) e rede restrita.

## 19.5 Servir tudo por API

Publique o engenheiro e o revisor com o capítulo 17 (`agentprotocol`): `Options.Agents = [{ID:"engineer", Graph: agent}]`. Passe `source`, `repo` e `owner` em `config.configurable` para o prompt saber de onde vem o pedido.

## 19.6 Limites e próximos passos

- Slack e Linear existem como gatilhos (`openswe/slack`, `openswe/linear`; ver [OPENSWE.md](../OPENSWE.md#slack-e-linear)), sem botões/cartões de revisão nem projetos/ciclos. Dashboard, contas/OAuth, auditoria e o runtime gerenciado do projeto original **não** foram portados; o `Approver`, o `Requester` e a atribuição de PR são os pontos onde você pluga a sua identidade e os seus canais.
- `Local` não isola. Para produção use `Kubernetes` (ou implemente `sandbox.Provider` para o seu ambiente).
- Teste o seu fluxo com modelo roteirizado antes de ligar um modelo real: os testes de `openswe/openswe_test.go` são bons moldes.

---

Anterior: [18. ciam](18-ciam-contexto.md) · Próximo: [20. RAG, CAG e KAG](20-rag-cag-kag.md) · [Índice do manual](README.md)
