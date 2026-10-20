# Open SWE in Go (`openswe/`)

**English** | [Português](../pt-BR/OPENSWE.md) | [Español](../es-ES/OPENSWE.md)

A port of the ideas behind [Open SWE](https://github.com/langchain-ai/open-swe) (MIT): a "software factory" on top of a deep agent. It **investigates** a repository, **implements** the change in a sandbox isolated per thread, **validates** it and **delivers a pull request**; **reviews** PRs with findings anchored to the diff and reconciles them on every push; **watches the CI** until it turns green; and is triggered by **GitHub mentions, new PRs, schedules and automations**.

Tutorial: [manual/19-open-swe.md](manual/19-open-swe.md). Examples: [`examples/openswe/`](../../examples/openswe/). Prompts adapted from the original project (MIT); the code is a Go rewrite on top of this ecosystem (`deepagents`, `agents`, `langgraph`, `mcp`, `langmem`, `ciam`).

## Packages

| Package | Does | Origin (open-swe) |
|---|---|---|
| `openswe` | `NewAgent` (engineer), `ReviewerRunner`, `AgentRunner`, `PRTools`, `PRLinks` | `server.py`, `reviewer.py`, `tools/open_pull_request.py` |
| `openswe/prompts` | prompts as embedded files (system, reviewer, subagent) | `resources/prompts/**`, `prompt.py` |
| `openswe/github` | REST/GraphQL client, GitHub App auth, webhook parsing and signing | `github/**` |
| `openswe/sandbox` | one persistent sandbox per thread; `ErrUnreachable`; repository preparation | `sandboxes/**` |
| `openswe/guards` | rule middleware (PRs only through the tool, workflow push with approval, read-only reviewer, orphan tool-call repair, sandbox circuit breaker, follow-up queue) | `middleware/**` |
| `openswe/review` | findings anchored to the diff, publication, reconciliation with threads, per-repository style | `review/**`, `tools/add_finding.py`, `publish_review.py` |
| `openswe/babysit` | durable CI watcher, without spending tokens while idle; flaky re-run with a budget | `baby_sit.py`, `bundled_skills/baby-sit` |
| `openswe/triggers` | webhook, "one run per thread" dispatch, cron, automations | `webhooks/**`, `schedules/**`, `dispatch.py` |
| `openswe/slack` | Web API client (`chat.postMessage`, `chat.update`, `reactions.add`), Events API with v0 signing, `Notifier` that returns the reply to the thread | Slack integration |
| `openswe/linear` | GraphQL client (issue, `commentCreate`), webhook with `Linear-Signature`, `Notifier` that comments on the issue | Linear integration |

## The engineer

```go
agent, _ := openswe.NewAgent(openswe.Config{
    Model: model, GitHub: ghClient, Sandbox: sandboxManager,
    Links: &openswe.PRLinks{S: st}, DraftPRs: true, AllowedOrgs: []string{"acme"},
    Babysit: watcher, Dispatcher: dispatcher, Queue: queue, Approver: approver,
    Context: &ciam.Toolkit{M: ciamManager, Project: "/workspace/api"}, // optional
})
```

It is a `deepagents.Create` with: a backend resolved **per thread** (`sandbox.Manager.Backend()`), delivery tools (`open_pull_request`, `link_pull_request`, `github_comment`, `request_pr_review`), `fetch_url` (with SSRF protection), baby-sit tools, a read-only `researcher` subagent, and the stack of *guards*. The **system prompt is rendered on every call** from the run's origin (`github`, `schedule`, `cli`), the repository and the `AGENTS.md`/`CLAUDE.md` read from the sandbox (5-minute cache), the workspace rules and your instructions.

### The delivery rules (in the prompt **and** in the code)

1. A PR is the default delivery for any code change; `open_pull_request` only after `git push`.
2. `open_pull_request` is **idempotent**: if an open PR already exists for the branch, it returns that one (no duplicates); it refuses an unpublished branch with the instruction to push; it appends *References* (thread link) and the attribution; it applies the draft policy (`DraftPRs`).
3. **Never** `gh pr create`, `gh api .../pulls` (POST), `curl` to the pulls endpoint or `hub pull-request`: `PRCreationGuard` blocks them (including inside `bash -c`, `eval`, `env X=1 ...`).
4. Access failure (403/404/"App not installed") → the tool returns "do not try another path; report and stop".
5. **Never** mark a draft as ready (`request_pr_review` refuses a draft PR and asks the author to do it), **never** approve a PR as yourself, and no force-push without authorization.
6. A `git push` that changes `.github/workflows/` needs **human approval** tied to the exact diff (`WorkflowPushGuard`); without a configured `Approver`, it is denied.

## The reviewer

`ReviewerRunner` (a `triggers.Runner`): reads the PR (head/base), loads the repository's style, builds a `review.Session`, **reconciles** what happened on GitHub since the last review, renders the prompt (first review, re-review or reassessment of a reply) and runs a **read-only** agent (writes only to `/review/**`, `ReadOnlyGuard` on the shell).

Tools: `fetch_review_diff` (writes the diff to a file in the sandbox), `add_finding`, `list_findings`, `update_finding`, `resolve_finding_thread`, `publish_review`.

- **Anchoring**: `add_finding` rejects lines outside the PR diff (always the **full** diff base..head, even in an incremental re-review). Title ≤ 120, suggestion ≤ 4 lines (longer ones are discarded, the description stays), duplicates (same *fingerprint*: file + category + normalized title) are merged.
- **`publish_review`** (once, alone): `ranking` must list **exactly** the new findings (otherwise it returns `expected_finding_ids`); an anchor that left the diff becomes `unresolvable_findings`; a re-review with nothing new, or an empty review on an already reviewed head, becomes `skipped_empty_re_review` (no spam); `dry_run` validates without posting. It posts **one** review with inline comments (`line`/`start_line`/`side`) and a hidden marker per finding, and links each comment to the finding and to the GitHub *thread*.
- **Approval**: a per-repository mode (`off`, `dry_run` default, `approve`). It only approves if the mode is `approve`, the assessment is positive, **there is no open finding of severity ≥ medium**, and the head has not changed during the review.
- **Reconciliation** (`Session.Reconcile`): a thread resolved by a human closes the finding; human replies become interactions and trigger a **focused reassessment** (`resolve_finding_thread` posts the note as a reply and resolves the thread).
- **Learned style**: `review.LearnStyle` distills, with your model, the guidelines from **human** comments on recently merged PRs and stores them in `Style.CustomPrompt` (automatic review is **opt-in** per repository: `Style.Enabled`).

## CI: baby-sit

`babysit.Watcher.Start` opens a watch (it does not open if the PR is closed or CI has already passed). The watch reacts to the `check_run/check_suite` webhook and to a deterministic *poll* (`Tick`/`Run`, 10 min in the original) — **neither of them calls a model**. The agent is only woken (`Dispatcher.Wake`) when: there is a new failure (once per `head + set of failing checks`), everything turned green (ends), the PR was closed/merged, or GitHub access failed 3 times. If delivering the wake fails, the key is forgotten and the next evaluation tries again.

`rerun_flaky_check` re-runs **only the failed jobs** of a **GitHub Actions** check (never other providers), requires evidence, only on the watched head, at most **3 per head**; a denied permission does not consume the budget. The `babysit.Skill` (embedded SKILL.md) teaches the agent to diagnose and to decide between deterministic, flaky, ambiguous and external.

## Triggers

`triggers.Webhook` validates `X-Hub-Signature-256`, responds 202 and processes in the background (events for the same issue/PR in arrival order, in the process); a repeated delivery (`X-GitHub-Delivery`) runs once. Routing: a mention of the bot in an issue/PR/comment → the item's code thread (deterministic `ThreadID`: the same PR always lands in the same thread and reuses context and sandbox); a new/ready PR in an enabled repository → review (and `Watch` for re-reviews on pushes); a human reply to a reviewer comment → reassessment; `check_run`/`check_suite` → baby-sit; everything also fires automations. Protections: events from the bot itself are ignored; in a **public** repository only organizations in `AllowedOrgs` can trigger the agent; all external text reaches the agent inside `<untrusted_...>` (a closing tag in the text is neutralized).

`triggers.Dispatcher`: **one run per thread**. A message for a busy thread goes to the queue (`guards.Queue`); `guards.QueueMiddleware` injects it before the next model call of the running run and, whatever is left at the end, starts a new run — nothing is lost.

Automations: `Automation{Prompt, Repo, Triggers: [{schedule, cron}|{github, issues.opened|pull_request.opened|closed|merged}]}` in an `AutomationStore`; `Scheduler.Tick/Run` fires each cron minute **once** (claim per automation+minute, safe with several replicas; limited recovery after a pause).

## Slack and Linear

Both come in through the same `triggers.Dispatcher` as GitHub: each event becomes a `triggers.Task` with `Kind: KindAgent`, and **an external thread is an openswe thread** (`triggers.ThreadKey`, keyed by `slack:<channel>:<thread_ts>` or `linear:issue:<id>`), so context and sandbox are reused just as on GitHub.

```go
d := &triggers.Dispatcher{
    Runners: map[string]triggers.Runner{triggers.KindAgent: // chained notifiers; each one passes on what is not its own
        slack.Notifier{Client: slack.NewClient(os.Getenv("SLACK_BOT_TOKEN")),
            Inner: linear.Notifier{Client: linear.NewClient(os.Getenv("LINEAR_API_KEY")),
                Inner: openswe.AgentRunner{Graph: agent}}}},
    Queue: &guards.MemoryQueue{}, // no queue: a message for a busy thread is an error
}
slackH := &slack.Handler{SigningSecret: os.Getenv("SLACK_SIGNING_SECRET"), BotUserID: "U0BOT",
    Channels: []string{"C0CODE"}, Dispatcher: d} // Channels is required: empty ignores everything
linearH := &linear.Webhook{Secret: os.Getenv("LINEAR_WEBHOOK_SECRET"), Dispatcher: d,
    BotUserID: "linear-user-id", BotName: "openswe", TriggerLabel: "openswe"}
mux.Handle("/slack/events", slackH)
mux.Handle("/linear/webhook", linearH)
```

**Slack** (`openswe/slack`)

- `Handler` verifies `X-Slack-Signature` (`v0=` + HMAC-SHA256 of `v0:<timestamp>:<body>`, constant-time comparison) and refuses timestamps more than 5 minutes off. `url_verification` receives the `challenge`. The signature vector and the `event_callback` envelope were checked against the Slack documentation; no test against a real workspace.
- `app_mention` in a configured channel starts the run; the thread is `thread_ts`, or the message's own `ts` when the mention starts the thread.
- Messages with a `subtype` are ignored (bots, edits, channel joins), except `thread_broadcast` and `file_share`, which are from users.
- A reply in a thread only becomes a follow-up if it mentions the bot **or** if the thread was already triggered by this process. That knowledge lives in memory: after a restart, the bot must be mentioned again.
- Repeated deliveries (same `event_id`, including with `X-Slack-Retry-Num`) and the `app_mention` + `message` pair with the same `ts` run once. The handler always answers `200`; dispatch failures go to `OnError` (Slack does not resend).
- `Notifier` posts the **agent's final answer** in the thread at the end of the run (or "The run failed: …"). Pinging mentions (`<!channel>`, `<@U…>`) are neutralized before sending. `Describe` adds, for example, the PR link (`PRLinks.ForThread`).
- The text from whoever wrote in Slack goes inside `<untrusted_slack author="…">`.

**Linear** (`openswe/linear`)

- `Webhook` verifies `Linear-Signature` (hex HMAC-SHA256 of the raw body) and refuses a `webhookTimestamp` more than 60 s off.
- An issue created or updated **assigned to the bot** (`BotUserID`) or with the `TriggerLabel` label starts the run. On `update`, it only counts when `updatedFrom` carries `labelIds` or `assigneeId`. Known limit: if another label changes in the same event, the label already present also triggers a new run (which goes to the thread's queue, so nothing is lost).
- A `create` comment that mentions `@BotName` (or the `mention://user/<BotUserID>` link), written by another account, starts the run in the issue's thread. The author comes from `data.user.id`, `data.userId` or `actor.id` (the official example only carries `userId`).
- A repeated delivery (`Linear-Delivery`) runs once.
- `Notifier` comments the final answer on the issue (`commentCreate`).
- Fields were checked against the Linear documentation's payload example and against the `IssueWebhookPayload`, `CommentWebhookPayload` and `EntityWebhookPayload` types of the public GraphQL schema (`linear/linear`, `packages/sdk/src/schema.graphql`); the client queries use fields from that schema. There was no test against a real workspace.
- `linear.Client` sends the personal key directly in the `Authorization` header; for an OAuth token, set `AuthPrefix: "Bearer "`. GraphQL errors (`errors`) become `*linear.APIError`, even with HTTP 200.

Offline example (fake Slack, fake GitHub, scripted model): [`examples/openswe/slack_trigger`](../../examples/openswe/slack_trigger/).

## Sandbox

`sandbox.Manager`: one sandbox per thread, stored (provider + id) in the `store.Store`. Providers: `Local` (a directory on the host, `backends.LocalShell`: **no isolation**; logs a warning on the first command, no `AllowedCommands`) and `Kubernetes` (a pod, via `integrations/kubernetes`); for any other environment that runs commands (SSH, Docker, cloud), implement `sandbox.Provider` on top of `backends.Remote`. Rule inherited from the original: a sandbox that does not respond is **not silently replaced** (`ErrUnreachable`, "no replacement was started"): a new one would be empty and would throw away unpublished work; only `Replace` (a deliberate act) creates another. `PrepareRepo` clones/updates, sets the commit identity, enters the work branch **without** resetting or cleaning what a previous run left, and stores the token in a `0600` credential (never in the remote URL).

## Configuration and production

- **GitHub token**: `github.App{AppID, Key}` (RS256 JWT → installation token with cache) or `StaticToken`. Give the sandbox a token **without** permission to create PRs if you want `PRCreationGuard` to be more than a guardrail.
- **Persistence**: `store.Store` for findings, watches, automations, PR↔thread links, styles and sandbox ids (`sqlstore`/Redis/Mongo in production); `checkpoint.Saver` for the agents' state.
- **Limits**: `ModelTimeout` (5 min), `RunLimit` (300 model calls) and `RecursionLimit` (2000 super-steps — each model call costs several because of the middleware hooks) per run.
- **Serving as an API**: the engineer's and reviewer's graphs can be published with [`agentprotocol`](AGENT-PROTOCOL.md) (`config.configurable` carries `source`, `repo`, `owner`).
- **Project context**: [`ciam`](CIAM.md) as `Config.Context`.

## What was **not** ported

Declared, not hidden:

- **Slack**: interactive buttons, human review cards, expedited review through cards and dedicated code channels (the bot answers in any channel in the `Channels` list, with no channel of its own per repository).
- **Linear**: projects, cycles, syncing issue states and the agent creating issues. Only issue (creation/update with label or assignment) and comments with a mention are triggers.
- **Dashboard/UI**, desktop app, CLI and local *bridge*.
- **Accounts and authorization**: OAuth, profiles, "act as", workspace administration, feature flags, audit trail, analytics and cost per run. The packages receive identity (`Requester`, `Approver`) from whoever integrates them.
- **PostgreSQL persistence** of the product's tables (it uses the generic `store.Store`).
- **Managed Deep Agents runtime** / hosting on the LangSmith Platform; LangSmith sandbox provider (Daytona/E2B/Modal/Runloop/snapshots): use your own `sandbox.Provider`.
- `review-scout`, `review-guide`, PR *walkthrough*, *incidents*, skill store, automatic model selection, thread summary/title, provider-specific middleware (message sanitization per vendor).
- **Guard guarantees** (guardrails, not a security boundary): `PRCreationGuard` and `ReadOnlyGuard` inspect every command, including the text of `bash -c`/`eval`, and track per run the files the agent writes (`write_file`, `edit_file`, `> f`, `>> f`, `tee f`, heredoc and redirected `echo`/`printf`). When a command executes that file (`sh f`, `bash f`, `./f`, `source f`, `. f`, `python[3] f`, `node f`, `chmod +x f && ./f`), the last content written is inspected with the same rules, up to 3 levels of script. Download or decode-to-shell pipelines are also blocked directly (`base64 … | sh`, `curl … | bash`, `wget … | sudo sh`, `sh <(curl …)`, `bash -c "$(curl …)"`). A file written by one run only affects that run.
  - **Not covered**: content produced by another command (`cp`, `sed -i`, `python -c`, `git checkout`, `cat origem > f`), `xargs`/`find -exec`, files changed outside the tools, and payloads assembled at runtime. These cases depend on the boundary below.
  - **Real boundary**: (1) the **token** used by the sandbox has no permission to create PRs or write to GitHub (reviewer: read-only); (2) the **sandbox filesystem is read-only** for the reviewer, except `/review/**`.
  - **How to configure (2)**: `openswe/sandbox` currently has no read-only mount option; the pod of the `Kubernetes` provider is created by `integrations/kubernetes.NewSandbox` with the default spec. For a reviewer, implement your own `sandbox.Provider` that creates the pod with `securityContext.readOnlyRootFilesystem: true` and a writable `emptyDir` only at `/review`, and wrap it with `backends.Remote`.

## Tests

`go test -race ./openswe/...`, 100% offline (GitHub and CI simulated with `httptest`, real git in temporary directories): Slack (v0 signature with a fixed vector, 5-minute window, `url_verification`, mention and thread follow-up, retries and duplicates, final answer and mention neutralization) and Linear (`Linear-Signature` with a fixed vector, 60-second window, assignment and label, comment mention, repeated delivery, comment with a GraphQL error, comment payload copied from the official documentation), `event_callback` envelope and `thread_broadcast` of Slack per the documentation; GitHub client (PR idempotency, App JWT with signature verification, token cache, webhooks), prompts, shell parsing and all the guards (including a real `git push` with workflows), sandbox (persistence, non-replacement, routing by thread, clone/continuation without losing work, token outside the URL), reviewer (anchors, dedupe, ranking, approval, reconciliation, style), baby-sit (wake dedupe, re-run budget, repeated webhooks, API failures), triggers (cron, claims, queue, webhook security, per-PR event order, automations) and end-to-end: an engineer delivering a PR, a reviewer publishing once and unable to write, mention → run with a follow-up in the same run.
