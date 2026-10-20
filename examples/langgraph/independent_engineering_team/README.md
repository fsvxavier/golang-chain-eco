# independent_engineering_team

The `engineering_team` flow with every agent as **its own service**: a process (or container, or machine) per agent, talking HTTP. Workers start and die on their own and take jobs from a queue.

## What it does

| Service | Count | Job |
|---|---|---|
| `hub` | 1 | Registry with atomic reservation and leases, the job queue, and one MCP server with RAG, CAG, KAG and the team memory. |
| `manager` | 1 | Software Engineer Manager. Receives the request (Agent Server API, `remote/server`), routes it, drives the flow, writes the report. Never writes code (the run fails if its reply has a code fence). |
| `po`, `architect`, `leader` | 1 each | Specification, risk and security, task plan with complexity. |
| `junior-1`, `junior-2`, `mid-1`, `senior-1` | 4 | Implement in parallel. |
| `specialist-1`, `specialist-2` | 2 | Review: approve or return. |
| `qa-1`, `qa-2` | 2 | QA: approve or return. |

Each worker is `cmd/worker` with `WORKER_ROLE`; its persona is `skills/<name>/SKILL.md`.

How the pieces talk:

- **Request in**: `remote.Graph` (client) -> manager's `remote/server`.
- **Work out**: the manager submits a job to the hub (`Prefer` = worker ids or roles, in order). The hub reserves a free worker **atomically** (one lock; two jobs never get the same worker; high-complexity work never goes to a junior) or leaves the job queued. A worker long-polls `claim`, runs the job, and posts the result.
- **Agent Protocol**: every worker serves its agent over Agent Protocol (`agentprotocol.Server`). The job runs as a run on a thread named after the task (`<request>-T3-dev`), so a returned task goes back to the same engineer, who remembers what it wrote. Any Agent Protocol client can call a worker directly too.
- **Leases**: workers heartbeat. A worker silent for `HUB_LEASE_TTL` is dropped and its job returns to the front of the queue (a job is abandoned after 3 handouts). A late result from the dropped worker is rejected (409), so a job is never completed twice.
- **Context**: each job carries the whole dossier (request, spec, architect rules) and the feedback it received; the manager's graph is checkpointed per request; the **team memory** (MCP `memory_put` / `memory_search`, scoped by request id) holds each agent's output for the next ones.
- **Knowledge over MCP**: before answering, a worker queries the hub with the technique that fits its role and adds it to the prompt under `## Knowledge`:

  | Role | Tool | Why |
  |---|---|---|
  | architect | `kag_query` | relations: what guards what, what a payment touches |
  | engineers | `rag_answer` | retrieve coding standards by similarity (vector + BM25) |
  | specialist, QA | `cag_answer` | small stable checklists kept whole in context |
  | PO, leader | `knowledge_answer` | the router picks RAG, CAG or KAG |

  The same tools are also given to the agents, so a real model can call them itself.

## Choose the model of each agent

Set `AGENT_MODEL_<ROLE>=provider:model` (roles: `MANAGER PO ARCHITECT LEADER JUNIOR MID SENIOR SPECIALIST QA KNOWLEDGE`). Anthropic names may be short: `anthropic:opus_5_5` is `anthropic:claude-opus-5-5`. Resolution order: `AGENT_MODEL_<ROLE>`, then `AGENT_MODEL`, then per-role defaults for the key you set (Anthropic: Opus for PO and architect, Sonnet for manager, leader, senior and specialist, Haiku for the rest; OpenAI: `gpt-4o-mini`), else the scripted offline model. Keys: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, ... see `.env.example`. `KNOWLEDGE_EMBEDDINGS=provider:model` replaces the offline hashing embeddings.

```bash
AGENT_MODEL_PO=anthropic:opus_5_5 AGENT_MODEL_JUNIOR=anthropic:haiku_5_5 AGENT_MODEL_SENIOR=openai:gpt-4o \
  ANTHROPIC_API_KEY=... OPENAI_API_KEY=... go run ./examples/langgraph/independent_engineering_team
```

## How to apply it

- Edit a persona in `skills/<name>/SKILL.md`. Each defines the output format the manager parses (`TASK|id|title|complexity|desc`, `VERDICT:`, `RISK:`); change both together.
- Change the knowledge: `kb/*.md` feeds RAG (all), CAG (the two checklists); the KAG graph is `triples` in `internal/team/knowledge.go`.
- Change the team: add a worker to `docker-compose.yml` (`WORKER_ID`, `WORKER_ROLE`) and to `Cast` in `internal/team/local.go`. Nothing else knows the headcount.
- Change the flow: `internal/team/manager.go`.

## Run it

Single process, offline (every service on its own loopback port, real HTTP, scripted model):

```bash
go run ./examples/langgraph/independent_engineering_team
go test -race ./examples/langgraph/independent_engineering_team/...   # atomic reservation, leases, MCP knowledge, full delivery
```

Separate services, locally or in Docker:

```bash
cp examples/langgraph/independent_engineering_team/.env.example examples/langgraph/independent_engineering_team/.env
F=examples/langgraph/independent_engineering_team/docker-compose.yml
docker compose -f $F up --build -d          # hub, manager and 11 workers; order does not matter
docker compose -f $F run --rm client        # sends the sample request, prints report and history
curl localhost:8000/state                   # workers, reservations, queue
docker compose -f $F stop senior-1          # kill a worker mid-run: its job moves to another
docker compose -f $F down
```

Without Docker: `go run ./examples/langgraph/independent_engineering_team/cmd/hub`, then `cmd/manager` and one `cmd/worker` per role (`WORKER_ID`, `WORKER_ROLE`, `HUB_URL`, `LISTEN`), then `cmd/client`.

Host ports taken? Set `HUB_PORT` and `MANAGER_PORT`.

## Task status and notifications

Every task (and the request) always has one of 13 statuses: `received`, `in_specification`, `risk_sec_review`, `task_assignment`, `in_development`, `in_code_review`, `in_qa_testing`, `qa_approved`, `qa_rejected`, `review_approved`, `review_rejected`, `committed`, `completed`. The agent that holds the work is recorded with each move.

```
request: received -> in_specification -> risk_sec_review -> task_assignment -> completed
task:    task_assignment -> in_development -> in_code_review -> review_approved -> in_qa_testing -> qa_approved -> committed -> completed
         in_code_review -> review_rejected -> in_development      in_qa_testing -> qa_rejected -> in_development
```

`examples/internal/tasktrack` checks every move against this table (a task cannot reach `committed` without review and QA; an illegal move is logged and not recorded). A task that exhausts its 3 rounds stays at the rejection that stopped it. Here `committed` means the approved code is recorded as the task's deliverable; a real run commits it in the sandbox first.

Each move is announced by `NOTIFY` (default `console`): `console`, `slack` (`SLACK_WEBHOOK_URL`), `email` (`SMTP_ADDR`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`, `MAIL_TO`) and `chat` (`CHAT_WEBHOOK_URL`, any webhook that accepts `{"text": "...", "event": {...}}`), comma separated. `NOTIFY_STATUSES=review_rejected,qa_rejected,completed` limits slack, email and chat to those statuses; the console always shows all. A failing sink is logged and never blocks the work.

```bash
NOTIFY=console,slack SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... NOTIFY_STATUSES=qa_rejected,completed go run ./examples/langgraph/independent_engineering_team
```

## Test it in production mode

1. Put real keys in `.env` and run the client. Structure holds (all tasks `approved` or `blocked`); wording and review rounds vary. A parse error means a model ignored a skill's output format: tighten that skill.
2. Pin the models per agent as above and compare cost and quality per role.
3. Kill a worker during a run and watch `GET /state` (`curl localhost:8000/state`): after `HUB_LEASE_TTL` the job moves and the run still finishes.
4. The hub is one instance with in-memory state (`ponytail:` note in `hub.go`). To run it replicated, keep the same transitions as compare-and-swap in a shared store. Likewise replace `checkpoint.NewMemory()` with a durable saver and the in-memory memory/vector stores with `integrations/` backends.
5. Engineers return code as text. Run it in a sandbox before review (`kubernetes.Sandbox`, `examples/integrations/remote_sandbox`).
6. Add `MANAGER_API_KEY` and put the hub on a private network: the hub and workers have no auth.

## See also

- `examples/langgraph/engineering_team`: the same flow in one process
- [Manual, chapter 7: LangGraph](../../../docs/en-US/manual/07-langgraph.md), [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
