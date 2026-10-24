# full_team_front

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A copy of [full_team](../full_team/README.md) in which the whole team works on front-end and user experience. It runs as an MCP server: VS Code (or another MCP chat) sends a demand, and the team turns it into reviewed screen code and tests. The demand travels product owner, engineering team, developer, joint review (specialist and tech lead), QA. Any problem found in review or QA goes back to the developer who wrote the code.

Offline it runs with fake models and a fake backend, so the whole flow runs without a key. This folder is self-contained: the team code was copied, not imported, because Go does not allow importing another tree's `internal/` package.

## What it does

- **Team and models.** Twelve workers, one process each when run as services. Each role has a model; the defaults apply when only `ANTHROPIC_API_KEY` is set.

  | Worker | Count | Model | Job |
  |---|---|---|---|
  | `product-owner` (`po`) | 1 | `claude-opus-5-5` | reads the backend, writes the spec |
  | `leader` (tech lead) and `manager` | 1 each | `claude-sonnet-5-5` | plans the tasks, assigns them, takes part in the review |
  | `software-engineer-junior` | 2 | `claude-haiku-5-5` | builds the screen code and its unit tests |
  | `software-engineer-mid` | 2 | `claude-haiku-5-5` | same |
  | `software-engineer-senior` | 2 | `claude-haiku-5-5` | same |
  | `specialist-reviewer` | 2 | `claude-sonnet-5-5` | reviews the code with the tech lead |
  | `qa-engineer` | 2 | `claude-sonnet-5-5` | writes the automated tests of the screens |

  Override one role with `AGENT_MODEL_<ROLE>`, for example `AGENT_MODEL_PO=anthropic:opus_5_5` or `AGENT_MODEL_QA=openai:gpt-4o-mini`. `AGENT_MODEL` sets one model for every role. Without any key the run is offline.

- **Flow.** The PO writes the spec from the demand and the backend it reads. The leader plans the tasks and names a preferred developer for each one; the assignment takes the current load of each worker into account. The developer builds the screen code and its unit tests. Then the **joint review** runs: the specialist and the leader see the same submission, and the task passes only when both approve. QA then writes the automated tests of the screens, traced to the PO spec, and approves when they cover it.

- **Rework goes to the same developer.** When the joint review or QA rejects a task, the feedback goes to the developer who wrote it, on the same thread. That rework job is pinned: it waits for that developer instead of going to another one. If the developer's lease has expired, the job falls back to a free developer of the same level. A task rejected in `MaxRounds` (3) rounds ends `blocked`.

- **The PO reads the backend named in the demand.** The PO has three read-only tools, and only the PO has them:
  - `backend_read`: reads one file. The path must be under a root named in the demand and under `WORKSPACE_ROOT`. `..`, absolute paths outside the roots and symlinks that leave the roots are refused. The size is capped.
  - `backend_grep`: searches under the same roots.
  - `backend_get`: an HTTP `GET` only; there is no method argument. The host must be the host of a URL named in the demand. http and https only. A redirect to another host is refused. Loopback and link-local targets are refused unless the demand names them. The body is capped and the request has a timeout.

  The roots and URLs come only from the structured fields of `team_submit` (`backend_roots`, `backend_urls`), never from free text in the request. `WORKSPACE_ROOT` is the workspace root; it defaults to the working directory. A demand with no roots and no URLs gives the PO no access at all.

- **The spec.** The PO lists the endpoints it read, the request and response payloads, the required fields, the errors and the UI states (loading, empty, error). Every payload it describes must come from something it read. The spec has an `EVIDENCE:` block (file and line, or URL, for each payload) and an `OPEN ITEMS:` block for what it could not confirm. The PO does not block on open items and does not invent a payload. If the demand names a backend and the PO did not call a backend tool, or the spec has no `EVIDENCE:` block, the manager adds `UNVERIFIED: no backend evidence` to the open items.

- **QA writes the tests.** The QA writes one test set per task for the screens the developers built, from the PO spec. Each test is named after the criterion or UI state it covers, and a `TRACE:` line maps tests to criteria. QA rejects to the developer when the screen code cannot satisfy a spec item (a wrong state, a missing field, a selector or accessibility problem).

- **Optional browser run (`browser_test`).** The QA can run its Playwright spec in Chrome. The command is fixed by the operator in `PLAYWRIGHT_CMD` (for example `npx playwright test --project=chromium`) and is run without a shell. It runs against the `app_url` named in the demand. If either one is missing, the tool reports `NOT RUN` with the reason, and the QA still writes the tests. Offline it returns a scripted report.

- **Skills per persona.** Each role has its own persona (front-end and UX) and reference skills vendored from public repositories, under `skills/<role>/references/<name>/SKILL.md`. They are embedded with `go:embed`, so the example runs on any machine. Each vendored skill is listed in `skills/skills.lock` with its source, its licence, the SHA-256 of the embedded file, and whether it was trimmed to fit the size budget. A skill whose licence does not allow redistribution is dropped, not paraphrased. Skills are pinned: nothing updates them automatically.

- **Compression.** Every worker's model calls go through the same compression layer as `full_team` (caveman on old turns, Ponytail, TOON, RTK for shell commands). It is not described again here: see [full_team](../full_team/README.md#what-it-does).

- **Offline.** The scripted PO calls `backend_read`, `backend_grep` and `backend_get` against an embedded fake backend: a small OpenAPI document, a handler tree, and an `httptest` server. So the guards run for real. The scripted QA writes its tests from the offline spec.

## How to apply it

- **Demand scope.** Pass the backend in the demand, not in the request text: `backend_roots` for source trees, `backend_urls` for the API the PO may call, `app_url` for the running front end that QA may open.
- **Roles and models.** `anthropicDefaults` and `Cast` in `internal/team` hold the defaults. Change them there and keep the twelve-worker pool; the tests check it.
- **Backend tools.** Limits are in the backend tool configuration (the size cap and the timeout). Keep `WORKSPACE_ROOT` as narrow as the backend allows.
- **Skills.** To change a persona, edit its `SKILL.md`. To add a vendored skill, see below.
- **Shell.** The shell tool is copied from `full_team`. With a provider key, the model may run only `ls`, `grep` and `echo`. Do not add interpreters or `find`.

### Update a vendored skill

1. Fetch the candidate into a scratch directory, not the global skills folder: `npx skills add <owner/repo@skill> -y` run inside a temporary project. Do not use `-g`.
2. Read its `SKILL.md` and its licence (the file or the repository licence). If the licence does not allow redistribution, drop it.
3. Copy the file to `skills/<role>/references/<name>/SKILL.md`. If it is over the budget (about 8 KB per skill, 24 KB per role), trim it and set `trimmed` to `true` in its `skills.lock` entry.
4. Update the entry in `skills/skills.lock`: `source`, `skill`, `installs`, `hash` (SHA-256 of the new file), `license`, `trimmed`.
5. Run the tests below. They check that every lock entry has its file embedded, that the hash matches, and the size budgets.

## Run it

Offline, one process:

```bash
go run ./examples/langgraph/full_team_front
```

The run prints the services it started and the delivery report. It starts the hub, the manager and the workers on loopback and stops them at the end.

With a real provider (the models are the defaults above):

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/full_team_front
OPENAI_API_KEY=...    go run ./examples/langgraph/full_team_front   # openai:gpt-4o-mini for every role
```

## Use it as an MCP server in VS Code

The team is served by `Bridge.MCPServer` in `internal/team/bridge.go`, over stdio. It exposes these tools:

- `team_submit`: sends a demand. Its fields are `request` and, optionally, `backend_roots`, `backend_urls` and `app_url`. It returns a `thread_id` and a `run_id` at once.
- `team_status`: the task statuses, the report and the handoff history of a `thread_id`.
- `team_queue`: the registered workers and the queued jobs.
- `team_ask_worker`: asks one worker a question, outside the delivery flow.
- `team_memory`: recalls what the team recorded for a request.
- The generic graph tools: `graph_run`, `graph_state` and `graph_resume`, for any graph on an Agent Server.

The bridge talks to a team that is already running: `MANAGER_URL` is the manager's Agent Server API and `HUB_URL` is the hub. The one-process `go run` above starts its services only for the length of the run, so it cannot be the server VS Code connects to.

Register the bridge in `.vscode/mcp.json` (VS Code's format uses `servers`):

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "/abs/path/to/bridge",
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000"
      }
    }
  }
}
```

Then open the file and click **Start** above the server. In Copilot Chat choose **Agent** and enable `front-team` in the tools picker.

Send a demand with the scope fields. This is an example `team_submit` call (the arguments as JSON; the paths and URLs are placeholders):

```json
{
  "request": "Add a coupon field to the checkout screen. Show a loading state while the coupon is checked, an empty state before any input, and an error message for an invalid code.",
  "backend_roots": ["/abs/path/to/backend"],
  "backend_urls": ["https://api.example.com/v1"],
  "app_url": "http://localhost:3000/checkout"
}
```

Then call `team_status` with the returned `thread_id` until the status is `completed` or `blocked`. The `PLAYWRIGHT_CMD` and `WORKSPACE_ROOT` variables are read by the worker processes where the tools run, not by the bridge.

## Test it

```bash
go test -race -count=1 ./examples/langgraph/full_team_front/...
```

The tests cover the pool and the models, the pinned rework, the joint review and the rejection path back to the same developer, the scope of a demand, the PO backend guards, the evidence rule, the QA tests and the `browser_test` refusals, the persona and skill files, and the offline end-to-end run.

## Test it in production mode

1. Run with `ANTHROPIC_API_KEY` (or `OPENAI_API_KEY`) and check that the models in the report are the ones you set. `AGENT_MODEL_<ROLE>` overrides the defaults.
2. Send a demand with `backend_roots` or `backend_urls`. Check that the spec has an `EVIDENCE:` block and that every endpoint in it matches the backend you pointed at. A spec with `UNVERIFIED` in its open items means the PO did not read the backend.
3. Try a demand that names a host the PO should not reach, and a URL that redirects to another host. Both must be refused in the tool output.
4. Run the QA with `PLAYWRIGHT_CMD` and `app_url` set on the worker host, and check that the report shows the Playwright result, not `NOT RUN`. Confirm Chrome is installed where the workers run.
5. Keep `WORKSPACE_ROOT` and the backend roots as narrow as the work allows. The backend tools read files on the host that runs the PO worker.
6. Keep `AllowedCommands` tight: the shell tool runs on the host without a sandbox.

## See also

- [full_team](../full_team/README.md): the copied team, with the compression layer documented in full
- [independent_engineering_team](../independent_engineering_team/README.md): the same team split into services, with Docker compose
- [compress_shell_rtk](../compress_shell_rtk/README.md), [compress_toon](../compress_toon/README.md), [compress_ponytail](../compress_ponytail/README.md)
