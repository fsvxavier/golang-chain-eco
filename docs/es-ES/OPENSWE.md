# Open SWE en Go (`openswe/`)

[English](../en-US/OPENSWE.md) | [Português](../pt-BR/OPENSWE.md) | **Español**

Port de las ideas de [Open SWE](https://github.com/langchain-ai/open-swe) (MIT): una "fábrica de software" sobre un deep agent. Ella **investiga** un repositorio, **implementa** el cambio en un sandbox aislado por thread, **valida** y **entrega un pull request**; **revisa** PRs con findings anclados al diff y los reconcilia en cada push; **vigila el CI** hasta que queda en verde; y se activa con **menciones en GitHub, PRs nuevos, agendas y automatizaciones**.

Tutorial: [manual/19-open-swe.md](manual/19-open-swe.md). Ejemplos: [`examples/openswe/`](../../examples/openswe/). Prompts adaptados del proyecto original (MIT); el código es una reescritura en Go sobre este ecosistema (`deepagents`, `agents`, `langgraph`, `mcp`, `langmem`, `ciam`).

## Paquetes

| Paquete | Hace | Origen (open-swe) |
|---|---|---|
| `openswe` | `NewAgent` (ingeniero), `ReviewerRunner`, `AgentRunner`, `PRTools`, `PRLinks` | `server.py`, `reviewer.py`, `tools/open_pull_request.py` |
| `openswe/prompts` | prompts como archivos embebidos (sistema, revisor, subagent) | `resources/prompts/**`, `prompt.py` |
| `openswe/github` | cliente REST/GraphQL, autenticación de GitHub App, parseo y firma de webhooks | `github/**` |
| `openswe/sandbox` | un sandbox persistente por thread; `ErrUnreachable`; preparación del repositorio | `sandboxes/**` |
| `openswe/guards` | middleware de las reglas (PRs solo por la tool, push de workflow con aprobación, revisor de solo lectura, reparación de tool call huérfana, cortacircuitos de sandbox, cola de follow-ups) | `middleware/**` |
| `openswe/review` | findings anclados al diff, publicación, reconciliación con threads, estilo por repositorio | `review/**`, `tools/add_finding.py`, `publish_review.py` |
| `openswe/babysit` | vigilante de CI duradero, sin gastar tokens en espera; re-run de flaky con presupuesto | `baby_sit.py`, `bundled_skills/baby-sit` |
| `openswe/triggers` | webhook, despacho "una run por thread", cron, automatizaciones | `webhooks/**`, `schedules/**`, `dispatch.py` |
| `openswe/slack` | cliente Web API (`chat.postMessage`, `chat.update`, `reactions.add`), Events API con firma v0, `Notifier` que devuelve la respuesta al thread | integración de Slack |
| `openswe/linear` | cliente GraphQL (issue, `commentCreate`), webhook con `Linear-Signature`, `Notifier` que comenta en el issue | integración de Linear |

## El ingeniero

```go
agent, _ := openswe.NewAgent(openswe.Config{
    Model: model, GitHub: ghClient, Sandbox: sandboxManager,
    Links: &openswe.PRLinks{S: st}, DraftPRs: true, AllowedOrgs: []string{"acme"},
    Babysit: watcher, Dispatcher: dispatcher, Queue: queue, Approver: approver,
    Context: &ciam.Toolkit{M: ciamManager, Project: "/workspace/api"}, // opcional
})
```

Es un `deepagents.Create` con: backend resuelto **por thread** (`sandbox.Manager.Backend()`), tools de entrega (`open_pull_request`, `link_pull_request`, `github_comment`, `request_pr_review`), `fetch_url` (con protección SSRF), tools de baby-sit, subagent `researcher` de solo lectura y la pila de *guards*. El **prompt de sistema se renderiza en cada llamada** a partir del origen de la run (`github`, `schedule`, `cli`), del repositorio y del `AGENTS.md`/`CLAUDE.md` leído del sandbox (caché de 5 min), de las reglas del workspace y de tus instrucciones.

### Las reglas de entrega (en el prompt **y** en el código)

1. Un PR es la entrega predeterminada de cualquier cambio de código; `open_pull_request` solo después del `git push`.
2. `open_pull_request` es **idempotente**: si ya existe un PR abierto para la rama, devuelve ese (no duplica); rechaza una rama no publicada con la instrucción de hacer push; agrega *References* (enlace del thread) y la atribución; aplica la política de borrador (`DraftPRs`).
3. **Nunca** `gh pr create`, `gh api .../pulls` (POST), `curl` al endpoint de pulls ni `hub pull-request`: `PRCreationGuard` los bloquea (incluso dentro de `bash -c`, `eval`, `env X=1 ...`).
4. Fallo de acceso (403/404/"App no instalada") → la tool devuelve "no intentes otro camino; informa y termina".
5. **Nunca** marcar un borrador como listo (`request_pr_review` rechaza un PR en borrador y pide al autor que lo haga), **nunca** aprobar un PR como si fueras tú, y sin force-push sin autorización.
6. Un `git push` que cambia `.github/workflows/` necesita **aprobación humana** atada al diff exacto (`WorkflowPushGuard`); sin `Approver` configurado, se niega.

## El revisor

`ReviewerRunner` (un `triggers.Runner`): lee el PR (head/base), carga el estilo del repositorio, arma una `review.Session`, **reconcilia** lo que pasó en GitHub desde la última revisión, renderiza el prompt (primera revisión, re-revisión o reevaluación de una respuesta) y ejecuta un agent **de solo lectura** (escritura solo en `/review/**`, `ReadOnlyGuard` en el shell).

Tools: `fetch_review_diff` (escribe el diff en un archivo del sandbox), `add_finding`, `list_findings`, `update_finding`, `resolve_finding_thread`, `publish_review`.

- **Anclaje**: `add_finding` rechaza líneas fuera del diff del PR (siempre el diff **completo** base..head, incluso en una re-revisión incremental). Título ≤ 120, sugerencia ≤ 4 líneas (una mayor se descarta, la descripción se mantiene), los duplicados (misma *fingerprint*: archivo + categoría + título normalizado) se fusionan.
- **`publish_review`** (una vez, sola): `ranking` tiene que listar **exactamente** los findings nuevos (si no, devuelve `expected_finding_ids`); un ancla que salió del diff pasa a `unresolvable_findings`; una re-revisión sin nada nuevo, o una revisión vacía sobre un head ya revisado, se convierte en `skipped_empty_re_review` (sin spam); `dry_run` valida sin publicar. Publica **una** review con comentarios inline (`line`/`start_line`/`side`) y un marcador oculto por finding, y enlaza cada comentario con el finding y con el *thread* de GitHub.
- **Aprobación**: modo por repositorio (`off`, `dry_run` predeterminado, `approve`). Solo aprueba si el modo es `approve`, la evaluación es positiva, **no hay ningún finding abierto de severidad ≥ medium** y el head no cambió durante la revisión.
- **Reconciliación** (`Session.Reconcile`): un thread resuelto por una persona cierra el finding; las respuestas humanas se convierten en interacciones y disparan una **reevaluación enfocada** (`resolve_finding_thread` publica la nota como respuesta y resuelve el thread).
- **Estilo aprendido**: `review.LearnStyle` destila, con tu modelo, las directrices de los comentarios **humanos** de PRs recientes fusionados y las guarda en `Style.CustomPrompt` (la revisión automática es **opt-in** por repositorio: `Style.Enabled`).

## CI: baby-sit

`babysit.Watcher.Start` abre una vigilancia (no se abre si el PR está cerrado o el CI ya pasó). La vigilancia reacciona al webhook `check_run/check_suite` y a un *poll* determinístico (`Tick`/`Run`, 10 min en el original) — **ninguno de los dos llama a un modelo**. El agent solo se despierta (`Dispatcher.Wake`) cuando: hay un fallo nuevo (una vez por `head + conjunto de checks fallidos`), todo quedó en verde (termina), el PR se cerró/fusionó, o el acceso a GitHub falló 3 veces. Si la entrega del wake falla, la clave se olvida y la siguiente evaluación vuelve a intentarlo.

`rerun_flaky_check` vuelve a ejecutar **solo los jobs fallidos** de un check de **GitHub Actions** (nunca los demás proveedores), exige evidencia, solo en el head vigilado, como máximo **3 por head**; un permiso denegado no consume presupuesto. La skill `babysit.Skill` (SKILL.md embebido) enseña al agent a diagnosticar y a decidir entre determinístico, flaky, ambiguo y externo.

## Disparadores

`triggers.Webhook` valida `X-Hub-Signature-256`, responde 202 y procesa en segundo plano (eventos del mismo issue/PR en orden de llegada, dentro del proceso); una entrega repetida (`X-GitHub-Delivery`) se ejecuta una vez. Enrutamiento: mención al bot en issue/PR/comentario → thread de código del ítem (`ThreadID` determinístico: el mismo PR siempre cae en el mismo thread y reutiliza contexto y sandbox); PR nuevo/listo en un repositorio habilitado → revisión (y `Watch` para re-revisiones en pushes); respuesta humana a un comentario del revisor → reevaluación; `check_run`/`check_suite` → baby-sit; todo también dispara automatizaciones. Protecciones: los eventos del propio bot se ignoran; en un repositorio **público** solo las organizaciones en `AllowedOrgs` pueden activar el agent; todo texto externo llega al agent dentro de `<untrusted_...>` (una etiqueta de cierre en el texto se neutraliza).

`triggers.Dispatcher`: **una run por thread**. Un mensaje para un thread ocupado va a la cola (`guards.Queue`); `guards.QueueMiddleware` lo inyecta antes de la siguiente llamada al modelo de la run en curso y, lo que sobre al final, inicia una nueva run — nada se pierde.

Automatizaciones: `Automation{Prompt, Repo, Triggers: [{schedule, cron}|{github, issues.opened|pull_request.opened|closed|merged}]}` en `AutomationStore`; `Scheduler.Tick/Run` dispara cada minuto de cron **una vez** (claim por automatización+minuto, seguro con varias réplicas; recuperación limitada tras una pausa).

## Slack y Linear

Los dos entran por el mismo `triggers.Dispatcher` que GitHub: cada evento se convierte en un `triggers.Task` con `Kind: KindAgent`, y **un thread externo es un thread de openswe** (`triggers.ThreadKey`, indexado por `slack:<canal>:<thread_ts>` o `linear:issue:<id>`), así que contexto y sandbox se reutilizan como en GitHub.

```go
d := &triggers.Dispatcher{
    Runners: map[string]triggers.Runner{triggers.KindAgent: // notifiers encadenados; cada uno pasa adelante lo que no es suyo
        slack.Notifier{Client: slack.NewClient(os.Getenv("SLACK_BOT_TOKEN")),
            Inner: linear.Notifier{Client: linear.NewClient(os.Getenv("LINEAR_API_KEY")),
                Inner: openswe.AgentRunner{Graph: agent}}}},
    Queue: &guards.MemoryQueue{}, // sin cola, un mensaje para un thread ocupado es un error
}
slackH := &slack.Handler{SigningSecret: os.Getenv("SLACK_SIGNING_SECRET"), BotUserID: "U0BOT",
    Channels: []string{"C0CODE"}, Dispatcher: d} // Channels es obligatorio: vacío ignora todo
linearH := &linear.Webhook{Secret: os.Getenv("LINEAR_WEBHOOK_SECRET"), Dispatcher: d,
    BotUserID: "linear-user-id", BotName: "openswe", TriggerLabel: "openswe"}
mux.Handle("/slack/events", slackH)
mux.Handle("/linear/webhook", linearH)
```

**Slack** (`openswe/slack`)

- `Handler` verifica `X-Slack-Signature` (`v0=` + HMAC-SHA256 de `v0:<timestamp>:<cuerpo>`, comparación en tiempo constante) y rechaza timestamps con más de 5 minutos de diferencia. `url_verification` recibe el `challenge`. El vector de firma y el envelope `event_callback` se comprobaron con la documentación de Slack; sin prueba contra un workspace real.
- `app_mention` en un canal configurado inicia la run; el thread es `thread_ts`, o el propio `ts` cuando la mención inicia el thread.
- Los mensajes con `subtype` se ignoran (bots, ediciones, entradas al canal), salvo `thread_broadcast` y `file_share`, que son de usuario.
- Una respuesta en un thread solo se vuelve follow-up si menciona al bot **o** si el thread ya fue activado por este proceso. Ese conocimiento vive en memoria: tras un reinicio, hay que volver a mencionar al bot.
- Las entregas repetidas (mismo `event_id`, incluso con `X-Slack-Retry-Num`) y el par `app_mention` + `message` con el mismo `ts` se ejecutan una vez. El handler siempre responde `200`; los fallos de despacho van a `OnError` (Slack no reenvía).
- `Notifier` publica la **respuesta final del agente** en el thread al terminar la run (o "The run failed: …"). Las menciones que notifican (`<!channel>`, `<@U…>`) se neutralizan antes de enviar. `Describe` agrega, por ejemplo, el enlace del PR (`PRLinks.ForThread`).
- El texto de quien escribió en Slack va dentro de `<untrusted_slack author="…">`.

**Linear** (`openswe/linear`)

- `Webhook` verifica `Linear-Signature` (HMAC-SHA256 en hex del cuerpo crudo) y rechaza un `webhookTimestamp` con más de 60 s de diferencia.
- Un issue creado o actualizado **asignado al bot** (`BotUserID`) o con la etiqueta `TriggerLabel` inicia la run. En `update`, solo cuenta cuando `updatedFrom` trae `labelIds` o `assigneeId`. Límite conocido: si otra etiqueta cambia en el mismo evento, la etiqueta ya presente también dispara una nueva run (que va a la cola del thread, así que no se pierde).
- Un comentario `create` que menciona `@BotName` (o el enlace `mention://user/<BotUserID>`), escrito por otra cuenta, inicia la run en el thread del issue. El autor viene de `data.user.id`, `data.userId` o `actor.id` (el ejemplo oficial solo trae `userId`).
- Una entrega repetida (`Linear-Delivery`) se ejecuta una vez.
- `Notifier` comenta la respuesta final en el issue (`commentCreate`).
- Los campos se comprobaron con el ejemplo de payload de la documentación de Linear y con los tipos `IssueWebhookPayload`, `CommentWebhookPayload` y `EntityWebhookPayload` del schema GraphQL público (`linear/linear`, `packages/sdk/src/schema.graphql`); las consultas del cliente usan campos de ese schema. No hubo prueba contra un workspace real.
- `linear.Client` envía la clave personal directamente en el header `Authorization`; para un token OAuth, define `AuthPrefix: "Bearer "`. Los errores GraphQL (`errors`) se convierten en `*linear.APIError`, incluso con HTTP 200.

Ejemplo offline (Slack falso, GitHub falso, modelo guionado): [`examples/openswe/slack_trigger`](../../examples/openswe/slack_trigger/).

## Sandbox

`sandbox.Manager`: un sandbox por thread, guardado (proveedor + id) en el `store.Store`. Proveedores: `Local` (un directorio en el host, `backends.LocalShell`: **sin aislamiento**; registra un aviso en el primer comando, sin `AllowedCommands`) y `Kubernetes` (un pod, vía `integrations/kubernetes`); para cualquier otro entorno que ejecute comandos (SSH, Docker, nube), implementa `sandbox.Provider` sobre `backends.Remote`. Regla heredada del original: un sandbox que no responde **no se reemplaza en silencio** (`ErrUnreachable`, "no se inició ningún reemplazo"): uno nuevo estaría vacío y tiraría trabajo no publicado; solo `Replace` (acto deliberado) crea otro. `PrepareRepo` clona/actualiza, define la identidad de commit, entra en la rama de trabajo **sin** resetear ni limpiar lo que dejó una run anterior, y guarda el token en una credencial `0600` (nunca en la URL del remote).

## Configuración y producción

- **Token de GitHub**: `github.App{AppID, Key}` (JWT RS256 → token de instalación con caché) o `StaticToken`. Dale al sandbox un token **sin** permiso de crear PR si quieres que `PRCreationGuard` sea más que una barrera de protección.
- **Persistencia**: `store.Store` para findings, vigilancias, automatizaciones, vínculos PR↔thread, estilos e ids de sandbox (`sqlstore`/Redis/Mongo en producción); `checkpoint.Saver` para el estado de los agents.
- **Límites**: `ModelTimeout` (5 min), `RunLimit` (300 llamadas al modelo) y `RecursionLimit` (2000 super-pasos — cada llamada al modelo cuesta varios por los hooks del middleware) por run.
- **Servir como API**: los grafos del ingeniero y del revisor pueden publicarse con [`agentprotocol`](AGENT-PROTOCOL.md) (`config.configurable` lleva `source`, `repo`, `owner`).
- **Contexto del proyecto**: [`ciam`](CIAM.md) como `Config.Context`.

## Lo que **no** se portó

Declarado, no escondido:

- **Slack**: botones interactivos, tarjetas de revisión humana, revisión expedita por tarjetas y canales de código dedicados (el bot responde en cualquier canal de la lista `Channels`, sin canal propio por repositorio).
- **Linear**: proyectos, ciclos, sincronización de estados del issue y creación de issues por el agente. Solo son disparadores el issue (creación/actualización con etiqueta o asignación) y los comentarios con mención.
- **Dashboard/UI**, app de escritorio, CLI y *bridge* local.
- **Cuentas y autorización**: OAuth, perfiles, "act as", administración de workspaces, feature flags, pista de auditoría, analíticas y costo por run. Los paquetes reciben la identidad (`Requester`, `Approver`) de quien los integra.
- **Persistencia en PostgreSQL** de las tablas del producto (usa el `store.Store` genérico).
- **Runtime de Managed Deep Agents** / hospedaje en LangSmith Platform; proveedor de sandbox de LangSmith (Daytona/E2B/Modal/Runloop/snapshots): usa tu propio `sandbox.Provider`.
- `review-scout`, `review-guide`, *walkthrough* de PR, *incidents*, skill store, selección automática de modelo, resumen/título de thread, middleware específicos de proveedor (saneamiento de mensajes por vendor).
- **Garantías de los guards** (barreras de protección, no frontera de seguridad): `PRCreationGuard` y `ReadOnlyGuard` inspeccionan cada comando, incluido el texto de `bash -c`/`eval`, y siguen por run los archivos que el agent escribe (`write_file`, `edit_file`, `> f`, `>> f`, `tee f`, heredoc y `echo`/`printf` redirigidos). Cuando un comando ejecuta ese archivo (`sh f`, `bash f`, `./f`, `source f`, `. f`, `python[3] f`, `node f`, `chmod +x f && ./f`), se inspecciona el último contenido escrito con las mismas reglas, hasta 3 niveles de script. También se bloquean directamente los pipelines de descarga o decodificación hacia shell (`base64 … | sh`, `curl … | bash`, `wget … | sudo sh`, `sh <(curl …)`, `bash -c "$(curl …)"`). Un archivo escrito por una run solo afecta a esa run.
  - **No cubiertos**: contenido producido por otro comando (`cp`, `sed -i`, `python -c`, `git checkout`, `cat origen > f`), `xargs`/`find -exec`, archivos alterados fuera de las tools, y payloads armados en tiempo de ejecución. Estos casos dependen de la frontera de abajo.
  - **Frontera real**: (1) el **token** usado por el sandbox no tiene permiso para crear PRs ni para escribir en GitHub (revisor: solo lectura); (2) el **sistema de archivos del sandbox es de solo lectura** para el revisor, salvo `/review/**`.
  - **Cómo configurar (2)**: `openswe/sandbox` hoy no tiene opción de montaje de solo lectura; el pod del proveedor `Kubernetes` lo crea `integrations/kubernetes.NewSandbox` con la spec predeterminada. Para un revisor, implementa un `sandbox.Provider` propio que cree el pod con `securityContext.readOnlyRootFilesystem: true` y un `emptyDir` escribible solo en `/review`, y envuélvelo con `backends.Remote`.

## Pruebas

`go test -race ./openswe/...`, 100% offline (GitHub y CI simulados con `httptest`, git real en directorios temporales): Slack (firma v0 con vector fijo, ventana de 5 min, `url_verification`, mención y follow-up por thread, reintentos y duplicados, respuesta final y neutralización de menciones) y Linear (`Linear-Signature` con vector fijo, ventana de 60 s, asignación y etiqueta, mención en comentario, entrega repetida, comentario con error GraphQL, payload de comentario copiado de la documentación oficial), envelope `event_callback` y `thread_broadcast` de Slack según la documentación; cliente de GitHub (idempotencia de PR, JWT de App con verificación de firma, caché de token, webhooks), prompts, parseo de shell y todos los guards (incluido un `git push` real con workflows), sandbox (persistencia, no reemplazo, enrutamiento por thread, clonado/continuación sin perder trabajo, token fuera de la URL), revisor (anclas, dedupe, ranking, aprobación, reconciliación, estilo), baby-sit (dedupe de wakes, presupuesto de re-runs, webhooks repetidos, fallos de API), disparadores (cron, claims, cola, seguridad de webhooks, orden de eventos por PR, automatizaciones) y de extremo a extremo: un ingeniero entregando un PR, un revisor publicando una vez y sin poder escribir, mención → run con follow-up en la misma run.
