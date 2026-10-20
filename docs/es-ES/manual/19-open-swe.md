# 19. Open SWE: una fábrica de software con agents

[English](../../en-US/manual/19-open-swe.md) | [Português](../../pt-BR/manual/19-open-swe.md) | **Español**

El paquete `openswe` monta, sobre los capítulos anteriores, un sistema que convierte el trabajo de ingeniería en un proceso repetible: **investigar → implementar en un sandbox → validar → entregar un PR → revisar → vigilar el CI → responder al feedback**. Referencia, mapa al proyecto original y lo que no se portó: [docs/es-ES/OPENSWE.md](../OPENSWE.md).

Requisitos previos: capítulos 5, 9, 10 (tools, agents, deep agents), 8 (store y checkpoint) y 11 (MCP).

## 19.1 Visión general

```
GitHub (issue, PR, comentario, CI) ─┐
agenda (cron) / automatizaciones ────┼─► triggers.Webhook / Scheduler ─► Dispatcher (1 run por thread)
terminal / API (agentprotocol) ──────┘                                        │
                                              ┌───────────────────────────────┤
                                              ▼                               ▼
                                   openswe.NewAgent (ingeniero)    openswe.ReviewerRunner
                                   sandbox por thread + guards     diff anclado, findings, publish
                                   open_pull_request / babysit     reconciliación en cada push
```

Cada pieza es un paquete que puedes usar por separado: `github`, `sandbox`, `guards`, `review`, `babysit`, `triggers`, `prompts`.

## 19.2 Ejecuta primero los ejemplos

Todo sin conexión (GitHub simulado, modelo guionado, git real):

```bash
go run ./examples/openswe/engineer   # issue → sandbox → atajo bloqueado → PR en borrador → comentario
go run ./examples/openswe/reviewer   # revisión única, push rechazado, re-revisión con reconciliación
go run ./examples/openswe/factory    # webhooks, cola, cron, claims y vigilancia de CI
```

## 19.3 Paso a paso para montar el tuyo

### 1. GitHub

```go
// GitHub App (recomendado): JWT RS256 → token de instalación con caché
gh := github.NewClient(&github.App{AppID: 12345, Key: pemBytes})
// o un token fijo para empezar:
gh = github.NewClient(github.StaticToken(os.Getenv("GITHUB_TOKEN")))
```

Permisos típicos de la App: *Contents* (rw), *Pull requests* (rw), *Issues* (rw), *Checks* (r), *Actions* (rw, solo si quieres re-runs de CI), *Metadata* (r); eventos: `issues`, `issue_comment`, `pull_request`, `pull_request_review_comment`, `check_run`, `check_suite`.

### 2. Sandbox por thread

```go
sb := &sandbox.Manager{
	Providers: map[string]sandbox.Provider{"k8s": sandbox.Kubernetes{Client: k8s, Namespace: "agents", Image: "ghcr.io/acme/dev:latest"}},
	Default: "k8s", Store: st, // el store guarda thread→sandbox
}
```

`sandbox.Local{Base: "/var/lib/openswe"}` sirve para desarrollo (¡sin aislamiento!). Un sandbox que deja de responder genera `sandbox.ErrUnreachable` y **no se reemplaza solo** — uno nuevo estaría vacío y perdería trabajo no publicado; reemplazarlo es una decisión explícita (`sb.Replace`).

Para preparar el repositorio dentro: `sandbox.PrepareRepo(ctx, executor, sandbox.RepoSpec{Owner, Name, Branch: "openswe/<thread>"}, identidad, token)` — clona o actualiza, define la identidad de commit y **conserva** el trabajo de runs anteriores.

### 3. El ingeniero

```go
agent, _ := openswe.NewAgent(openswe.Config{
	Model: model, GitHub: gh, Sandbox: sb, Checkpointer: saver, Store: st,
	Links: &openswe.PRLinks{S: st}, DraftPRs: true, AllowedOrgs: []string{"acme"},
	Queue: queue, Approver: approver, Babysit: watcher, Dispatcher: dispatcher,
})
```

Por run, el prompt de sistema se renderiza con el origen (`github`/`schedule`/`cli`), el `AGENTS.md` del repositorio y tus reglas (`Workspace`, `CustomInstructions`). Los prompts son archivos en `openswe/prompts/templates/` — léelos: son la política del agent.

### 4. El revisor

```go
reviews := review.NewStore(st)
styles := &review.StyleStore{S: st}
styles.Put(ctx, review.Style{FullName: "acme/api", Enabled: true, Approval: review.ApprovalDryRun})
rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: styles, Announce: true}
```

La revisión automática es **opt-in por repositorio** (`Style.Enabled`). Empieza con `ApprovalDryRun` (predeterminado: una evaluación positiva se convierte solo en un comentario) y cambia a `ApprovalOn` solo después de confiar en los resultados. Para que el revisor "hable como los mantenedores": `style.CustomPrompt, _ = review.LearnStyle(ctx, model, gh.Repo("acme","api"), 30)` y guárdalo.

### 5. Disparadores

```go
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{
	triggers.KindAgent:    openswe.AgentRunner{Graph: agent},
	triggers.KindReviewer: rr,
}, Queue: queue}
wh := &triggers.Webhook{Secret: os.Getenv("GH_WEBHOOK_SECRET"), Dispatcher: d, Bot: "openswe", BotLogins: []string{"openswe[bot]"},
	AllowedOrgs: []string{"acme"}, Claims: claims, Reviews: reviews, Styles: styles, Babysit: watcher, Scheduler: sched}
http.Handle("/webhooks/github", wh)
go sched.Run(ctx, logErr)        // cron y automatizaciones
go watcher.Run(ctx, 10*time.Minute, logErr) // fallback del vigilante de CI
```

`@openswe corrige esto` en un comentario de issue → thread de código (siempre el mismo para el mismo ítem: contexto y sandbox se reutilizan). Otro comentario mientras la run está ocupada entra en la **cola** y lo ve la run en curso antes de su siguiente llamada al modelo.

### 6. Vigilar el CI

Pídele al agent (tiene `manage_baby_sit`/`rerun_flaky_check` y la skill `babysit.Skill`): "`/baby-sit <URL del PR>`". La vigilancia **no gasta tokens** mientras nada cambia; solo despierta al agent ante una falla nueva, CI en verde o PR cerrado. Los re-runs solo se hacen para fallas **flaky con evidencia**, solo de los jobs fallidos de GitHub Actions, como máximo 3 por commit.

### 7. Automatizaciones

```go
automations.Put(ctx, triggers.Automation{Name: "triaje semanal", Repo: "acme/api", Enabled: true,
	Prompt:   "Triaja las issues abiertas y aplica etiquetas.",
	Triggers: []triggers.Trigger{{Kind: "schedule", Cron: "0 9 * * mon"}, {Kind: "github", Event: "issues.opened"}}})
```

Cron en UTC, 5 campos; cada minuto dispara **una vez** aunque haya varias réplicas (claims).

## 19.4 Las reglas y cómo se imponen

| Regla | Dónde |
|---|---|
| PR solo por `open_pull_request` (idempotente, borrador, atribución) | `PRCreationGuard` + tool |
| Nunca desmarcar un borrador; nunca aprobar tu propio PR; sin force-push sin autorización | prompt + `request_pr_review` rechaza borradores |
| Un push que cambia `.github/workflows/` exige aprobación humana **atada al diff exacto** | `WorkflowPushGuard` + `Approver` |
| El revisor no escribe, no hace commit ni push, y no publica fuera de las tools | permisos de archivo + `ReadOnlyGuard` |
| El texto de issues/PRs/comentarios/logs es **dato**, nunca instrucción | `<untrusted_...>` + prompt |
| Un repositorio público solo activa al agent para organizaciones permitidas | `Webhook.AllowedOrgs` |
| Una tool call interrumpida no bloquea el thread para siempre | `RepairOrphanedToolCalls` |
| Un sandbox muerto no consume el presupuesto de pasos ni desaparece en silencio | `SandboxCircuitBreaker` |

Ten claro el alcance: los guards **leen el comando**; no inspeccionan un script escrito en un archivo. Complementa con credenciales mínimas (un token de sandbox sin permiso para crear PR, por ejemplo), aislamiento real (Kubernetes) y red restringida.

## 19.5 Servir todo por API

Publica el ingeniero y el revisor con el capítulo 17 (`agentprotocol`): `Options.Agents = [{ID:"engineer", Graph: agent}]`. Pasa `source`, `repo` y `owner` en `config.configurable` para que el prompt sepa de dónde viene el pedido.

## 19.6 Límites y próximos pasos

- Slack y Linear existen como disparadores (`openswe/slack`, `openswe/linear`; ver [OPENSWE.md](../OPENSWE.md#slack-y-linear)), sin botones/tarjetas de revisión ni proyectos/ciclos. El dashboard, cuentas/OAuth, auditoría y el runtime gestionado del proyecto original **no** se portaron; `Approver`, `Requester` y la atribución del PR son los puntos donde conectas tu propia identidad y tus canales.
- `Local` no aísla. Para producción usa `Kubernetes` (o implementa `sandbox.Provider` para tu entorno).
- Prueba tu flujo con un modelo guionado antes de conectar uno real: las pruebas de `openswe/openswe_test.go` son buenas plantillas.

---

Anterior: [18. ciam](18-ciam-contexto.md) · Siguiente: [20. RAG, CAG y KAG](20-rag-cag-kag.md) · [Índice del manual](README.es-ES.md)
