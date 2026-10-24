# independent_engineering_team

The `engineering_team` flow with every agent as **its own service**: a process (or container, or machine) per agent, talking HTTP. Workers start and die on their own and take jobs from a queue. The agents run through `middleware.Compaction()`: old turns are caveman-compressed, a Ponytail note is added when the conversation has a real share of code, JSON tool results become TOON, all decided by rules with no model needed.

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

## Chat bridge (MCP)

`cmd/bridge` is an MCP server on stdio. Chats and consoles start it themselves and talk to it over stdin/stdout, so nothing needs to listen on a port. It reaches the running stack through three variables.

### 1. Build the server once

```bash
go build -o bin/bridge ./cmd/bridge
```

Use a binary instead of `go run`: it starts instantly and prints nothing but protocol on stdout. Below, `/abs/path/to/bridge` is the file you just built.

### 2. Variables

| Variable | Default | Use |
|---|---|---|
| `MANAGER_URL` | `http://localhost:8080` | manager (Agent Server API) |
| `MANAGER_API_KEY` | empty | key, when the manager sets one |
| `HUB_URL` | `http://localhost:8000` | hub (queue, `GET /state`, team memory) |
| `BRIDGE_CIAM_CMD` | empty | command of the project-context server, e.g. `/abs/path/ciam mcp` |
| `BRIDGE_KNOWLEDGE_CMD` | empty | command of the knowledge server, e.g. `/abs/path/knowledge mcp --dir /abs/.knowledge` |

### 3. Tools

`team_submit` sends a demand and returns `thread_id` at once; `team_status` shows task statuses, report and history; `team_queue` lists workers and the queue; `team_ask_worker` asks one worker over Agent Protocol; `team_memory` reads the team memory.

Beyond the team, the same server offers:

- `graph_run`, `graph_state`, `graph_resume`: run any graph on any Agent Server (`assistant_id`, optional `url`, `wait`), read a thread's state and interrupts, and answer an interrupt.
- the `ciam_*` tools (project search, ADR/PRD/plans, memory), when `BRIDGE_CIAM_CMD` is set.
- the `knowledge_*` tools (RAG, CAG, KAG), when `BRIDGE_KNOWLEDGE_CMD` is set. Run `knowledge index <path> --kind both` first; without an index that server fails to start and the bridge logs it and goes on without it.

Put these two variables in the `env` of the registration below, with absolute paths (`go build -o bin/ciam ./cmd/ciam`, `go build -o bin/knowledge ./cmd/knowledge`).

### 4. Register it in each tool

Put secrets (`MANAGER_API_KEY`) in the tool's own secret store or in your shell environment, not in a file you commit. Config formats change between releases; check the tool's MCP docs if a key is rejected.

#### GitHub Copilot (VS Code)

Requires VS Code with Copilot Chat in Agent mode. Create `.vscode/mcp.json` in the workspace (or run **MCP: Open User Configuration** for all workspaces):

```json
{
  "inputs": [
    { "type": "promptString", "id": "manager-key", "description": "MANAGER_API_KEY", "password": true }
  ],
  "servers": {
    "team-bridge": {
      "type": "stdio",
      "command": "/abs/path/to/bridge",
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/abs/path/to/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/abs/path/to/knowledge mcp --dir /abs/path/to/.knowledge",
        "MANAGER_API_KEY": "${input:manager-key}"
      }
    }
  }
}
```

Then open the file and click **Start** above the server (or run **MCP: List Servers**). In Copilot Chat choose **Agent**, open the tools picker and enable `team-bridge`. The `inputs` entry makes VS Code ask for the key once and keep it out of the file.

#### Claude Code

Command line (`--scope local` is the default; `project` writes `.mcp.json` to share with the team; `user` applies to every project):

```bash
claude mcp add --transport stdio --scope project \
  --env MANAGER_URL=http://localhost:8080 --env HUB_URL=http://localhost:8000 \
  --env BRIDGE_CIAM_CMD="/abs/path/to/ciam mcp" \
  --env BRIDGE_KNOWLEDGE_CMD="/abs/path/to/knowledge mcp --dir /abs/path/to/.knowledge" \
  team-bridge -- /abs/path/to/bridge
```

Or write `.mcp.json` at the project root by hand:

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/abs/path/to/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/abs/path/to/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/abs/path/to/knowledge mcp --dir /abs/path/to/.knowledge"
      }
    }
  }
}
```

Check with `claude mcp list` or `/mcp` inside a session. Project-scoped servers ask for approval the first time.

#### Cursor

Create `.cursor/mcp.json` in the project (or `~/.cursor/mcp.json` for all projects):

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/abs/path/to/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/abs/path/to/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/abs/path/to/knowledge mcp --dir /abs/path/to/.knowledge"
      }
    }
  }
}
```

Open **Settings → Cursor Settings → MCP**, turn the server on and wait for the green dot. Cursor lists the tools there; the agent uses them in Agent mode.

#### Kilo Code

Kilo Code reads `mcpServers` from `.kilocode/mcp.json` in the project (global settings: **MCP Servers → Edit Global MCP** in the Kilo panel). Note: newer Kilo releases built on Opencode read `kilo.json` with the same shape as Opencode below; this was not checked here, so use whichever your version shows in its MCP screen.

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/abs/path/to/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/abs/path/to/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/abs/path/to/knowledge mcp --dir /abs/path/to/.knowledge"
      },
      "alwaysAllow": ["team_status", "team_queue", "team_memory"],
      "disabled": false
    }
  }
}
```

Open the **MCP Servers** icon in the Kilo panel to see the status and tools. `alwaysAllow` lets the listed tools run without a prompt; leave it out to approve each call.

#### Opencode

Create `opencode.json` in the project (or `~/.config/opencode/opencode.json` for all projects):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "team-bridge": {
      "type": "local",
      "command": ["/abs/path/to/bridge"],
      "environment": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/abs/path/to/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/abs/path/to/knowledge mcp --dir /abs/path/to/.knowledge"
      },
      "enabled": true
    }
  }
}
```

Run `opencode mcp list` to check. In the TUI the tools appear as `team-bridge_team_submit` and so on.

### 5. Use it

Ask in the chat, for example: *"Use team_submit to send: let shoppers apply a coupon at checkout"*, then *"show team_status for that thread"* or *"who is in team_queue?"*. Start the stack first (`docker compose up`, see above).
