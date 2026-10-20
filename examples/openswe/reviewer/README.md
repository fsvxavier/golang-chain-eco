# reviewer

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

The Open SWE pull-request reviewer: read-only, one review per round, and a re-review that reconciles earlier findings instead of starting over.

## What it does

- Round 1: the reviewer fetches the diff, records a high-severity finding anchored to the line (a token written to the log), tries `git push` (refused: reviews are read-only) and publishes **one** review with an inline comment.
- The author pushes a fix and replies on the thread. Round 2 (a re-review) sees the old finding and the human reply (fenced as untrusted), resolves the finding with a note and does not post an empty review.
- The GitHub (and Slack) services are in-process fakes from `examples/internal` (`fakegithub`, `fakeslack`), so nothing leaves your machine.

## How to apply it

```go
rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: styles}
rr.Run(ctx, triggers.Task{Kind: triggers.KindReviewer, Owner: "acme", Repo: "api", Number: 5})
styles.Put(ctx, review.Style{FullName: "acme/api", Enabled: true, CustomPrompt: "Secrets must never reach logs.", Approval: review.ApprovalDryRun})
```

Per-repository `Style` sets enablement, a custom prompt and the approval mode (`ApprovalDryRun` records the verdict without approving on GitHub).

## Run it

```bash
go run ./examples/openswe/reviewer
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Start with `ApprovalDryRun` on a real repository: compare the reviewer's verdicts with human reviews for a few weeks before allowing approvals.
2. Use a real model and a GitHub App; keep the sandbox read-only (the Kubernetes provider needs a custom pod with `readOnlyRootFilesystem`; see OPENSWE.md).
3. Persist findings and styles in a durable `store.Store`; findings link to GitHub comment ids so a re-review can resolve them.
4. Track precision: how many findings humans mark useful; tune `CustomPrompt` per repo.
5. Treat PR content and replies as untrusted input (the reviewer already fences them) and never give the review sandbox write credentials.

## See also

- [Manual, chapter 19: Open SWE](../../../docs/en-US/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/en-US/OPENSWE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
