// Package openswe is a Go port of the ideas of Open SWE
// (github.com/langchain-ai/open-swe): an open-source "software factory" built
// on a deep agent. It investigates a codebase, implements a change in an
// isolated per-thread sandbox, validates it and delivers a pull request; it
// reviews pull requests with diff-anchored findings that it re-reconciles on
// every push; it watches CI until green; and it is started from GitHub
// mentions, new PRs, schedules and stored automations.
//
// The pieces are separate packages that compose:
//
//   - openswe/prompts: the engineer's and the reviewer's prompts, as embedded files.
//   - openswe/github: typed GitHub REST/GraphQL client, App auth, webhook parsing.
//   - openswe/sandbox: a persistent sandbox per thread (local, Kubernetes, any runner),
//     never silently replaced, plus repository preparation.
//   - openswe/guards: middleware for the rules (PRs only via the tool, workflow pushes
//     need approval, read-only reviewers, interrupted-tool repair, sandbox circuit breaker,
//     follow-up message queue).
//   - openswe/review: findings anchored to the diff, publish/reconcile, per-repo style.
//   - openswe/babysit: durable CI watches that spend no tokens while nothing changes.
//   - openswe/triggers: webhooks, one-run-per-thread dispatch, cron and automations.
//   - openswe/slack, openswe/linear: Slack Events API and Linear webhooks into the dispatcher,
//     and the replies posted back into the thread or issue.
//
// This package wires them into agents: NewAgent builds the engineer, and
// ReviewerRunner runs a review. Slack and Linear are integrated through
// openswe/slack and openswe/linear. What is not ported (Slack buttons and
// review cards, Linear projects and cycles, the dashboard, user accounts and
// Postgres, the Managed Deep Agents runtime) is listed in docs/OPENSWE.md.
package openswe
