# engineer

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

The Open SWE engineer end to end: it edits files in its own sandbox, is stopped when it tries to bypass the PR tool, and delivers an audited draft pull request.

## What it does

- A scripted engineer works GitHub issue `acme/api#12`: it writes `/api/auth/login.go` in the thread's own sandbox, tries the forbidden shortcut `gh pr create`, which a guard refuses, then opens the PR with `open_pull_request` and comments on the issue.
- Shows what the guards and prompt do: the repository's `AGENTS.md` reaches the system prompt, new PRs are drafts, only allowed orgs are accepted, and the PR is linked to the thread (`PRLinks`).
- The GitHub (and Slack) services are in-process fakes from `examples/internal` (`fakegithub`, `fakeslack`), so nothing leaves your machine.

## How to apply it

```go
agent, _ := openswe.NewAgent(openswe.Config{
    Model: model, GitHub: ghClient, Sandbox: sandboxManager, Links: links, Checkpointer: saver,
    DraftPRs: true, AllowedOrgs: []string{"acme"}, DefaultRepo: "acme/api",
})
err := openswe.AgentRunner{Graph: agent}.Run(ctx, triggers.Task{Kind: triggers.KindAgent, ThreadID: thread, Owner: "acme", Repo: "api", Number: 12, Messages: msgs})
```

One sandbox per thread, never silently replaced: a replacement would be empty and lose unpublished work. Pass a `*ciam.Toolkit` as `Config.Context` to give the engineer project context.

## Run it

```bash
go run ./examples/openswe/engineer
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Use a real model and a real GitHub App: `github.App{AppID, Key}` (RS256 JWT to an installation token, cached) instead of the fake. Give the sandbox a token **without** permission to create PRs, so `PRCreationGuard` is more than a guardrail.
2. Replace `sandbox.Local` (a host directory, **no isolation**) with the `Kubernetes` provider (a pod) or your own `sandbox.Provider`; keep `AllowedOrgs` tight.
3. Use durable stores (`sqlstore`, Redis or Mongo for `store.Store`; a durable `checkpoint.Saver`) so threads, PR links and sandbox ids survive restarts.
4. Keep `RunLimit`, `ModelTimeout` and `RecursionLimit` (the engineer needs a high recursion limit because of middleware hooks).
5. Try it on a throwaway repository first: open an issue, mention the bot, review the draft PR it opens, and confirm a forbidden command (`gh pr create`, force push) is refused.

## See also

- [Manual, chapter 19: Open SWE](../../../docs/en-US/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/en-US/OPENSWE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
