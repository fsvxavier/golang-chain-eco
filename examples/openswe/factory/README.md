# factory

How work reaches the agents: a GitHub webhook server, a cron scheduler with stored automations and a CI watcher.

## What it does

- A signed `issue_comment` that @mentions the bot starts the coding thread of that issue; a second comment while it runs is queued and joins the same run.
- Redelivered webhooks run once; strangers on other repos and the bot's own comments are ignored; a new PR in a repo with automatic review enabled starts a review.
- A cron automation (`0 9 * * mon`) fires once per matching minute (claims prevent doubles); a failing CI check wakes the baby-sit watcher's thread exactly once, and a flaky Actions job is re-run within a budget.
- The GitHub (and Slack) services are in-process fakes from `examples/internal` (`fakegithub`, `fakeslack`), so nothing leaves your machine.

## How to apply it

```go
d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: coder, triggers.KindReviewer: reviewer}, Queue: queue}
wh := &triggers.Webhook{Secret: secret, Dispatcher: d, Bot: "openswe", AllowedOrgs: []string{"acme"}, Claims: claims, Reviews: reviews, Styles: styles, Babysit: watcher, Scheduler: sched}
http.Handle("/github/webhook", wh)
```

The webhook verifies the signature, deduplicates deliveries (`Claims`), drops bot and out-of-org events, and routes to the right runner. `Dispatcher` queues messages for a busy thread.

## Run it

```bash
go run ./examples/openswe/factory
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Deploy the webhook behind TLS and configure the GitHub App's webhook URL and secret; check that an unsigned request is rejected.
2. Replace `MemoryClaims` and `guards.MemoryQueue` with durable ones so redeliveries and queued messages survive restarts and work across replicas.
3. Run two replicas and send the same delivery to both: only one run must start.
4. Enable automatic review per repository (`review.Style.Enabled`) gradually; start with a single repo.
5. Set the baby-sit retry budget (`babysit.MaxRetriesPerHead`) and alert when it is exhausted.

## See also

- [Manual, chapter 19: Open SWE](../../../docs/en-US/manual/19-open-swe.md)
- [OPENSWE.md](../../../docs/en-US/OPENSWE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
