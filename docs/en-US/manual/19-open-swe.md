# 19. Open SWE: a software factory with agents

**English** | [Português](../../pt-BR/manual/19-open-swe.md) | [Español](../../es-ES/manual/19-open-swe.md)

The `openswe` package builds, on top of the previous chapters, a system that turns engineering work into a repeatable process: **investigate → implement in a sandbox → validate → deliver a PR → review → watch CI → respond to feedback**. Reference, map to the original project and what was not ported: [docs/en-US/OPENSWE.md](../OPENSWE.md).

Prerequisites: chapters 5, 9, 10 (tools, agents, deep agents), 8 (store and checkpoint) and 11 (MCP).

## 19.1 Overview

```
GitHub (issue, PR, comment, CI) ─┐
schedule (cron) / automations ───┼─► triggers.Webhook / Scheduler ─► Dispatcher (1 run per thread)
terminal / API (agentprotocol) ──┘                                        │
                                              ┌───────────────────────────┤
                                              ▼                           ▼
                                   openswe.NewAgent (engineer)    openswe.ReviewerRunner
                                   per-thread sandbox + guards    anchored diff, findings, publish
                                   open_pull_request / babysit    reconciliation on every push
```

Each piece is a package you can use on its own: `github`, `sandbox`, `guards`, `review`, `babysit`, `triggers`, `prompts`.

## 19.2 Run the examples first

Everything offline (simulated GitHub, scripted model, real git):

```bash
go run ./examples/openswe/engineer   # issue → sandbox → blocked shortcut → draft PR → comment
go run ./examples/openswe/reviewer   # single review, refused push, re-review with reconciliation
go run ./examples/openswe/factory    # webhooks, queue, cron, claims and CI watch
```

## 19.3 Step by step to build yours

### 1. GitHub

```go
// GitHub App (recommended): RS256 JWT → installation token with cache
gh := github.NewClient(&github.App{AppID: 12345, Key: pemBytes})
// or a fixed token to start:
gh = github.NewClient(github.StaticToken(os.Getenv("GITHUB_TOKEN")))
```

Typical App permissions: *Contents* (rw), *Pull requests* (rw), *Issues* (rw), *Checks* (r), *Actions* (rw, only if you want CI re-runs), *Metadata* (r); events: `issues`, `issue_comment`, `pull_request`, `pull_request_review_comment`, `check_run`, `check_suite`.

### 2. Per-thread sandbox

```go
sb := &sandbox.Manager{
	Providers: map[string]sandbox.Provider{"k8s": sandbox.Kubernetes{Client: k8s, Namespace: "agents", Image: "ghcr.io/acme/dev:latest"}},
	Default: "k8s", Store: st, // the store keeps thread→sandbox
}
```

`sandbox.Local{Base: "/var/lib/openswe"}` is fine for development (no isolation!). A sandbox that stops responding raises `sandbox.ErrUnreachable` and is **not swapped automatically** — a new one would be empty and would lose unpublished work; swapping is an explicit decision (`sb.Replace`).

To prepare the repository inside it: `sandbox.PrepareRepo(ctx, executor, sandbox.RepoSpec{Owner, Name, Branch: "openswe/<thread>"}, identity, token)` — clones or updates, sets the commit identity and **preserves** the work from previous runs.

### 3. The engineer

```go
agent, _ := openswe.NewAgent(openswe.Config{
	Model: model, GitHub: gh, Sandbox: sb, Checkpointer: saver, Store: st,
	Links: &openswe.PRLinks{S: st}, DraftPRs: true, AllowedOrgs: []string{"acme"},
	Queue: queue, Approver: approver, Babysit: watcher, Dispatcher: dispatcher,
})
```

Per run, the system prompt is rendered with the origin (`github`/`schedule`/`cli`), the repository's `AGENTS.md` and your rules (`Workspace`, `CustomInstructions`). The prompts are files in `openswe/prompts/templates/` — read them: they are the agent's policy.

### 4. The reviewer

```go
reviews := review.NewStore(st)
styles := &review.StyleStore{S: st}
styles.Put(ctx, review.Style{FullName: "acme/api", Enabled: true, Approval: review.ApprovalDryRun})
rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: styles, Announce: true}
```

Automatic review is **opt-in per repository** (`Style.Enabled`). Start with `ApprovalDryRun` (default: a positive assessment becomes only a comment) and switch to `ApprovalOn` only after you trust the results. For the reviewer to "talk like the maintainers": `style.CustomPrompt, _ = review.LearnStyle(ctx, model, gh.Repo("acme","api"), 30)` and save it.

### 5. Triggers

```go
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{
	triggers.KindAgent:    openswe.AgentRunner{Graph: agent},
	triggers.KindReviewer: rr,
}, Queue: queue}
wh := &triggers.Webhook{Secret: os.Getenv("GH_WEBHOOK_SECRET"), Dispatcher: d, Bot: "openswe", BotLogins: []string{"openswe[bot]"},
	AllowedOrgs: []string{"acme"}, Claims: claims, Reviews: reviews, Styles: styles, Babysit: watcher, Scheduler: sched}
http.Handle("/webhooks/github", wh)
go sched.Run(ctx, logErr)        // cron and automations
go watcher.Run(ctx, 10*time.Minute, logErr) // CI watcher fallback
```

`@openswe fix this` in an issue comment → code thread (always the same one for the same item: context and sandbox are reused). Another comment while the run is busy goes into the **queue** and is seen by the running run before its next model call.

### 6. Watching CI

Ask the agent (it has `manage_baby_sit`/`rerun_flaky_check` and the `babysit.Skill`): "`/baby-sit <PR URL>`". The watch **spends no tokens** while nothing changes; it only wakes the agent on a new failure, a green CI or a closed PR. Re-runs are only for **flaky failures with evidence**, only the failed GitHub Actions jobs, at most 3 per commit.

### 7. Automations

```go
automations.Put(ctx, triggers.Automation{Name: "weekly triage", Repo: "acme/api", Enabled: true,
	Prompt:   "Triage the open issues and apply labels.",
	Triggers: []triggers.Trigger{{Kind: "schedule", Cron: "0 9 * * mon"}, {Kind: "github", Event: "issues.opened"}}})
```

Cron in UTC, 5 fields; each minute fires **once** even with several replicas (claims).

## 19.4 The rules and how they are enforced

| Rule | Where |
|---|---|
| PRs only through `open_pull_request` (idempotent, draft, attribution) | `PRCreationGuard` + tool |
| Never unmark a draft; never approve your own PR; no force-push without authorization | prompt + `request_pr_review` refuses drafts |
| A push that changes `.github/workflows/` requires human approval **tied to the exact diff** | `WorkflowPushGuard` + `Approver` |
| The reviewer does not write, commit or push, and publishes only through the tools | file permissions + `ReadOnlyGuard` |
| Text from issues/PRs/comments/logs is **data**, never instruction | `<untrusted_...>` + prompt |
| A public repository only triggers the agent for allowed organizations | `Webhook.AllowedOrgs` |
| An interrupted tool call does not lock the thread forever | `RepairOrphanedToolCalls` |
| A dead sandbox does not consume the step budget or disappear silently | `SandboxCircuitBreaker` |

Be clear about the scope: the guards **read the command**; they do not inspect a script written to a file. Complement them with minimal credentials (a sandbox token without permission to create PRs, for example), real isolation (Kubernetes) and restricted network.

## 19.5 Serving everything through an API

Publish the engineer and the reviewer with chapter 17 (`agentprotocol`): `Options.Agents = [{ID:"engineer", Graph: agent}]`. Pass `source`, `repo` and `owner` in `config.configurable` so the prompt knows where the request came from.

## 19.6 Limits and next steps

- Slack and Linear exist as triggers (`openswe/slack`, `openswe/linear`; see [OPENSWE.md](../OPENSWE.md#slack-and-linear)), without buttons/review cards or projects/cycles. The dashboard, accounts/OAuth, audit and the managed runtime of the original project were **not** ported; `Approver`, `Requester` and PR attribution are the points where you plug in your own identity and channels.
- `Local` does not isolate. For production use `Kubernetes` (or implement `sandbox.Provider` for your environment).
- Test your flow with a scripted model before connecting a real one: the tests in `openswe/openswe_test.go` are good templates.

---

Previous: [18. ciam](18-ciam-contexto.md) · Next: [20. RAG, CAG and KAG](20-rag-cag-kag.md) · [Manual index](README.md)
