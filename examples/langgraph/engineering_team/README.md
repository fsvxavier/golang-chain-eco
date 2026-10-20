# engineering_team

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

An engineering organisation as one graph: a manager routes a request, a product owner specifies it, an architect assesses risk, a tech leader splits it into tasks, four engineers build in parallel, and every task passes code review and QA before delivery. All agents run in one process.

## What it does

The cast, each with its persona in `skills/<name>/SKILL.md`:

| Role | Count | Job |
|---|---|---|
| Software Engineer Manager | 1 | Receives the request, routes it, writes the final report. Never writes code (the node fails if the reply contains a code fence). |
| Product Owner | 1 | Turns the request into a spec with numbered acceptance criteria. |
| Architect | 1 | Rates risk and sets constraints and security rules that travel with every task. |
| Tech Leader | 1 | Splits the spec into tasks and rates each `low`, `medium` or `high` complexity. |
| Software Engineers | 4 | 2 junior, 1 mid, 1 senior; implement in parallel. |
| Specialist reviewers | 2 | Approve a task or return it with feedback. |
| QA engineers | 2 | Approve a task or return it with a reproducible defect. |

Flow: `manager -> po -> architect -> leader -> (dev -> review -> qa -> done) x N -> deliver`. A task returned by review or QA goes back to the same engineer with the feedback; after 3 returns it is reported as `blocked`.

Three mechanisms make it work:

- **Atomic reservation.** The registry reserves people under a single lock, so two tasks never get the same engineer, reviewer or QA. The leader's assignment prefers a fit by complexity (`high` never goes to a junior); when nobody suitable is free the task waits in a backlog and is assigned when someone is released. The demo plan has five tasks for four engineers.
- **Context travels with the work.** Every task carries the whole dossier (request, spec, architect assessment) and the feedback it has received, because a `Send` argument is all a node sees. The shared state keeps the handoff history on a checkpointed thread (`graph.Thread("coupons")`).
- **A deferred `deliver` node** runs once every task has left the pipeline, then the manager writes the report.

## How to apply it

- Edit the personas: each `skills/<name>/SKILL.md` is the system prompt of its agent (the Go code embeds the folder with `go:embed` and strips the frontmatter). Every skill defines the output format the code parses (`TASK|id|title|complexity|description`, `VERDICT: APPROVE|RETURN`, `RISK:`, ...), so change formats in the skill and in `main.go` together.
- Change the team: add members in `newRegistry` and a skill for any new role.
- Change the pipeline: nodes are plain functions in `main.go`; routing is `Command{Send}`.
- The registry is a process-local mutex. For agents in separate processes you would need a store with compare-and-swap or a lease, plus a durable queue (see `remote/` and `agentprotocol/`).

## Run it

```bash
go run ./examples/langgraph/engineering_team
go test -race ./examples/langgraph/engineering_team   # registry atomicity and backlog tests
```

Offline, a scripted model (`brain.go`) answers by the `ROLE:` line of each prompt and makes review return T3 and QA return T2 once, so the loop back to the engineer is visible. With `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` set, the same graph runs with a real model (one model for the whole team; the persona comes from each skill). Expect parallel lines to interleave differently from run to run.

## Test it in production mode

1. Set a provider key and run again. The structure should hold (all five tasks reach `approved` or `blocked`), but wording and the number of review rounds will vary. If the run fails with a parse problem, a model ignored a skill's output format: tighten that skill.
2. Make `check` stricter than the model: the code returned by an engineer is text. In production, have the engineer node write it to a sandbox and run the real build and tests before review (see `examples/integrations/remote_sandbox` and `kubernetes.Sandbox`).
3. Replace `checkpoint.NewMemory()` with a durable saver (`sqlsaver`, Redis, ...) and derive `thread_id` from your work-item id; kill the process mid-run and resume.
4. Bound cost: the loop has `maxRounds`; also set `ModelCallLimit`/timeouts per agent and a `RecursionLimit` on the graph.
5. If agents must live in separate services, move each behind `agentprotocol`/`remote.Graph` and replace the in-process registry with a shared reservation store.
6. Evaluate the personas like code: run a set of requests (`examples/evals/basic`) and compare plans, review catch rate and QA defects before and after editing a skill.

## See also

- [Manual, chapter 7: LangGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
- `examples/langgraph/multi_agent_supervisor` and `hierarchical_teams`: simpler supervisor shapes
