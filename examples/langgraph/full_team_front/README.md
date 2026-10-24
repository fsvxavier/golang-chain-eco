# full_team_front

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A copy of [full_team](../full_team/README.md) in which the whole team works on front-end and user experience. It runs as an MCP server: VS Code (or another MCP chat) sends a demand, and the team turns it into reviewed screen code and tests. The demand travels product owner, engineering team, developer, joint review (specialist and tech lead), QA. Any problem found in review or QA goes back to the developer who wrote the code.

Offline it runs with fake models and a fake backend, so the whole flow runs without a key. This folder is self-contained: the team code was copied, not imported, because Go does not allow importing another tree's `internal/` package.

## What it does

- **Team and models.** Twelve workers (the `Cast` in `internal/team/local.go`), plus the manager and the hub. In this example everything runs in one process: each service listens on its own loopback port and they talk over real HTTP. Each role has a model; the defaults apply when only `ANTHROPIC_API_KEY` is set.

  | Worker | Count | Model | Job |
  |---|---|---|---|
  | `po` (product owner) | 1 | `claude-opus-5-5` | reads the backend, writes the spec |
  | `leader` (tech lead) | 1 | `claude-sonnet-5-5` | plans the tasks, names a preferred developer, takes part in the joint review |
  | `junior-1`, `junior-2` | 2 | `claude-haiku-5-5` | build the screen code and its unit tests |
  | `mid-1`, `mid-2` | 2 | `claude-haiku-5-5` | same |
  | `senior-1`, `senior-2` | 2 | `claude-haiku-5-5` | same |
  | `specialist-1`, `specialist-2` | 2 | `claude-sonnet-5-5` | review the code with the tech lead |
  | `qa-1`, `qa-2` | 2 | `claude-sonnet-5-5` | write the automated tests of the screens |

  Two more roles have a model but are not workers: `manager` (`claude-sonnet-5-5`, runs the delivery flow and writes the report) and `knowledge` (`claude-haiku-5-5`, answers the knowledge tools in the hub).

  Model selection, first match wins, per role: `AGENT_MODEL_<ROLE>` (`MANAGER`, `PO`, `LEADER`, `JUNIOR`, `MID`, `SENIOR`, `SPECIALIST`, `QA`, `KNOWLEDGE`), then `AGENT_MODEL` (one model for every role), then `ANTHROPIC_API_KEY` (the defaults above), then `OPENAI_API_KEY` (`openai:gpt-4o-mini` for every role). Examples: `AGENT_MODEL_PO=anthropic:opus_5_5`, `AGENT_MODEL_QA=openai:gpt-4o-mini`. With none of them set the run is offline: every role answers with a scripted model.

- **Flow.** The manager receives the demand and kicks off the PO. The PO writes the spec from the demand and the backend it reads. The leader plans the tasks (`TASK|id|title|complexity|description`, optionally `|PREFER:<worker id>`). The developer builds the screen code and its unit tests. Then the **joint review** runs: the specialist and the leader get the same submission at the same time, and the task passes only when both approve. QA then writes the automated tests of the screens, traced to the PO spec, and approves when they cover it. Tasks run concurrently. The leader's `PREFER:` is kept only if it names a worker id of the cast whose role fits the task's complexity (a role name alone, an unknown id, a reviewer, or a junior on a `high` task is dropped, with a log line); an unknown complexity becomes `medium`.

- **Who gets a task.** The leader is told which developers are idle or busy. The hub gives a job to the first free worker in the preference list: the worker the leader preferred, then roles by complexity (`low`: junior, mid, senior; `medium`: mid, senior, junior; `high`: senior, mid, never a junior). A job nobody can take yet waits in the queue.

- **Rework goes to the same developer.** When the joint review or QA rejects a task, the feedback goes to the developer who wrote it, on the same thread. That rework job is pinned: it waits for that developer instead of going to another one. If the developer's lease has expired, the job falls back to a free developer of the same role. Every rejection, from review or from QA, costs one round; a task rejected in `MaxRounds` (3) rounds is recorded as `blocked` in the manager's report and in the handoff history (its tracker status stays at the last rejection). The request still ends `completed`.

- **The PO reads the backend named in the demand.** The PO has three read-only tools, and only the PO has them. It also has one write tool, `project_doc_write`, limited to markdown project documents (see [The PO keeps the project documents](#the-po-keeps-the-project-documents)). The PO never has a shell tool (see Shell under How to apply it). The roots and URLs come only from the structured fields of `team_submit` (`backend_roots`, `backend_urls`), never from free text in the request. A demand with no roots and no URLs gives the PO no access at all: each tool refuses.
  - `backend_read`: reads one file or lists one directory. The path may be relative to the first root or absolute inside a root. Any `..` element is refused, and so is a real path (after resolving symlinks) outside the named roots. Every named root must itself lie under the workspace root (`WORKSPACE_ROOT`, else the working directory); a relative root is taken relative to the workspace root, and an empty root grants nothing. Output is capped at 1 MB.
  - `backend_grep`: searches a file or directory under the same roots with a Go regular expression and returns `path:line: text`. It never follows symlinks, stops when the output reaches 1 MB, and opens at most 5000 files (`[truncated: stopped after 5000 files]`).
  - `backend_get`: an HTTP `GET` only; there is no method argument. The scheme must be http or https, the URL may not carry credentials, and its host and port must equal those of a URL named in the demand. A redirect is checked with the same rule (at most 5), so a redirect to another host is refused. No proxy is used. The address actually dialed is checked too: loopback, link-local (which holds the `169.254.169.254` metadata address), unspecified and the Alibaba and AWS IPv6 metadata addresses are refused unless that exact `ip:port` was named as an IP literal. The name `localhost`, when the demand names it, may reach loopback addresses and nothing else. Private (RFC 1918) addresses are allowed. The body is capped at 1 MB and the call times out after 10 seconds.

  `WORKSPACE_ROOT` is read by the backend tools when the server starts; see the MCP section for which process that is.

- **The spec.** The PO lists the endpoints it read, the request and response payloads, the required fields, the errors and the UI states (loading, empty, error). Every payload it describes must come from something it read. The spec has an `EVIDENCE:` block (file and line, or URL, for each payload) and an `OPEN ITEMS:` block for what it could not confirm. The PO does not block on open items and does not invent a payload. **Evidence rule:** if the demand names a backend root or URL, the spec needs a line starting with `EVIDENCE:` and at least one backend call that passed the guards and got an answer (refused calls are not counted). The count lives in the hub (`POST /calls/{id}`, `GET /calls/{id}`): the PO tools report each such call there under the request id, and the manager reads it, so the rule holds when the manager and the PO run as separate services. The hub resets a request's count when its scope is stored at intake, so a re-run of the same thread starts from zero. Otherwise the manager inserts the line `- UNVERIFIED: no backend evidence` under `OPEN ITEMS:` (or appends an `OPEN ITEMS:` block). A demand with only an `app_url` does not trigger the rule.

- **QA writes the tests.** QA writes one test set per task for the screens the developers built, from the PO spec. Each test is named after the acceptance criterion or UI state it covers, and one `TRACE: AC1 -> <test name>` line per criterion maps tests to criteria. QA answers `VERDICT: APPROVE` or `VERDICT: RETURN` with `FEEDBACK:` naming the spec item the screen code cannot satisfy (a wrong state, a missing field, a selector or accessibility problem).

- **Optional browser run (`browser_test`).** Only QA has this tool. It runs the Playwright spec QA wrote against the `app_url` named in the demand (the model cannot choose the URL). The command is fixed by the operator in `PLAYWRIGHT_CMD`, split on spaces and run without a shell, in a fresh empty temporary directory, with a 2 minute timeout and the output capped at 1 MB. The spec (at most 256 KB) is written to a file and its path is passed in `SPEC_FILE`; the URL is passed in `APP_URL`. The child gets a minimal environment: `PATH`, a `HOME` and `TMPDIR` inside the temporary directory, `CI=1`, `PLAYWRIGHT_BROWSERS_PATH` if set, `APP_URL`, `SPEC_FILE`, and only the variables you list in `PLAYWRIGHT_ENV_ALLOW` (comma separated). Provider keys never reach it unless listed. On timeout or cancel the whole process group is killed, and the wait for its pipes is bounded to 5 seconds (Unix only). `app_url` must be an http or https URL with a host and no control characters. If `PLAYWRIGHT_CMD` is empty, or the demand has no valid `app_url`, the tool returns `NOT RUN: <reason>` and QA still writes the tests. A failing run is returned as `FAILED: ...` text, not as a tool error. The child therefore has no Playwright config and no `node_modules`, and its `HOME` is the temporary directory, so a bare `npx playwright test` has nothing to run there (untested). Recommended: point `PLAYWRIGHT_CMD` at a wrapper script that lives in your own Playwright project; the script reads `APP_URL` and `SPEC_FILE` from the environment, copies the spec into the project, `cd`s into it and runs Playwright there (untested here). Playwright needs `PLAYWRIGHT_BROWSERS_PATH` (set, or listed in `PLAYWRIGHT_ENV_ALLOW`) to find its browsers, and any other variable the script needs must be listed in `PLAYWRIGHT_ENV_ALLOW`. The offline QA never calls the tool.

- **Skills per persona.** Each role has its own persona (front-end and UX) and reference skills vendored from public repositories. They are embedded with `go:embed`, so the example runs on any machine. See [Vendored skills](#vendored-skills).

- **Compression.** Every worker's model calls go through the same compression layer as `full_team` (caveman on old turns, Ponytail, TOON, RTK for shell commands). It is not described again here: see [full_team](../full_team/README.md#what-it-does). The shell tool exists only in the demo run; see Shell under How to apply it.

- **Offline.** The scripted PO calls `backend_read`, `backend_grep` and `backend_get` against an embedded fake backend (a small OpenAPI document, a handler tree, and an `httptest` server), so the guards run for real, and the spec comes back with an `EVIDENCE:` block. The scenario is a front-end screen, a coupon field on the checkout form. The scripted leader plans five tasks: `T1` coupon field component, `T2` validation hook, `T3` error announcement and double-submit guard (preferring `senior-1`), `T4` discount line and UI states, `T5` keyboard and focus; the scripted developers answer with a TSX block. The scripted reviewers and QA reject three of them once, so the return to the same developer shows: the tech lead rejects `T2`, the specialist rejects `T3`, QA rejects `T4`. Offline the workers also have an `open_tasks` tool (a fixed backlog that gives the compression layer a tool result to rewrite); it is registered only when no role runs on a live model. The scripted QA answers with `VERDICT` and `TRACE:` lines only; it does not write test code and does not run `browser_test`. The demo run fails if the offline spec comes back `UNVERIFIED`.

## How to apply it

- **Demand scope.** Pass the backend in the demand, not in the request text: `backend_roots` for source trees, `backend_urls` for the API the PO may call, `app_url` for the running front end that QA may open.
- **Roles and models.** `anthropicDefaults` in `internal/team/models.go` and `Cast` in `internal/team/local.go` hold the defaults and the pool. Change them there and keep the twelve-worker pool; `TestCastHasTwelveWorkers` and `TestDefaultModels` check it.
- **Backend tools.** The limits (1 MB output, 10 second timeout, 5000 grep files) are in `internal/backend/backend.go`. Keep `WORKSPACE_ROOT` as narrow as the backend allows.
- **Skills.** To change a persona, edit its `SKILL.md`. To add or update a vendored skill, see below.
- **Shell.** The shell tool is copied from `full_team` and exists only in the demo run (`go run` without `serve`): `serve` mode gives no role a shell, so developer output is text. In the demo the shell child sees only `PATH` and a `HOME` in a temporary directory (no provider keys), and a command containing `<`, `>`, `$`, a backtick, a backslash or a brace is rejected. Offline the allowlist is `go`, `git`, `ls`, `grep` and `echo`; `go` and `git` are dropped whenever any role has a live model (a provider key, `AGENT_MODEL` or `AGENT_MODEL_<ROLE>`), leaving `ls`, `grep` and `echo`. Do not add interpreters or `find`. Path arguments are confined to the shell's working directory. The command is tokenised once, honouring single and double quotes, and split into segments only on `;`, `&`, `|` and newline outside quotes; every token of every segment is checked. A bare name, a path argument, the value after `=` in a `--flag=value` or `VAR=value` word, and the text attached to a short flag (`-fPATH`) that names an existing path outside the directory (absolute path, `..` or a symlink) are rejected with `shell: paths outside the workspace are not allowed: <token>`, as is anything starting with `~`. A glob (`*`, `?`, `[`) is checked in two ways. A path-like glob (it has a `/`, or starts with `.` or `~`) is rejected when it contains `[` at all, when its literal prefix (the components before the first wildcard one) is not inside the directory (so `/etc/pass*`, `../*/` and `/*/` are rejected without looking at matches), or when a `..` component follows a wildcard. Any glob is also rejected when a match of it, or of its cleaned form (trailing `/`, `/.`, `//`), is outside the directory; this applies inside quotes too, which is stricter than bash. For bare globs, and for any glob containing `[`, the match check also runs on a copy where each bracket expression becomes `?` (it matches exactly one character in bash and in Go, so the copy matches a superset), because the two dialects read `[!x]`, `[]x]` and `[[:alpha:]]` differently. A wildcard component that starts with `.` and can match `.` or `..` (`.*`, `.?`, `..*`) is refused, because bash before 5.2 expands it to `..`; this also refuses a bare grep regex such as `'.*'`. A bare pattern (`grep '[0-9]' file`) and a workspace glob (`ls *`, `src/*.tsx`) work, and a glob with no match passes. Every token after a standalone `--` counts as a path. A token that does not exist on disk (a grep pattern such as `/api/coupon`) passes. Backslashes and braces are rejected outright. File-reading flags are denied: for `grep`, `-f`, `--file`, `--exclude-from`, `--include-from`, `-R` and `--dereference-recursive`, as any unambiguous long prefix (`--deref`) and inside short bundles such as `-nf` (so `grep -efoo` is refused too; `-r`, `-n` and `--include=*.go` still work); for `go` and `git`, `--git-dir`, `--work-tree` and a `-C` placed before the subcommand. Other flags are not inspected. This is a guard for a demo-only tool, not a sandbox.

## Vendored skills

`skills/skills.lock` has 29 entries, one per copy: each persona folder has its own copy of every skill it carries, under `skills/<role>/references/<name>/SKILL.md`. The loader appends them to the persona's system prompt, sorted by name, under `## Skill: <name>`.

| Persona folder | Skills (copies) | Size |
|---|---|---|
| `product-owner` | `contract-first`, `openapi-spec-generation`, `user-stories` (3) | 9.1 KB |
| `engineering-manager` | `code-review`, `planning-and-task-breakdown` (2) | 5.9 KB |
| `tech-leader` | the same two (2) | 5.9 KB |
| `software-engineer-junior`, `-mid`, `-senior` | `frontend-design`, `frontend-patterns`, `react-testing`, `ui-ux-pro-max`, `vercel-react-best-practices` (5 each, 15) | 17.3 KB each |
| `specialist-reviewer` | `accessibility`, `frontend-code-review`, `performance-optimization`, `web-design-guidelines` (4) | 18.3 KB |
| `qa-engineer` | `playwright-best-practices`, `playwright-cli`, `webapp-testing` (3) | 10.7 KB |

That is 17 distinct skills in 29 copies. Facts about them:

- **They are trimmed, sanitised copies, not the upstream files.** Examples and boilerplate were cut to fit the budget, and installer commands, remote-fetch instructions, script invocations for scripts that are not vendored and tool permission grants (`allowed-tools`) were removed. Every lock entry has `"trimmed": true`.
- **Lock fields.** Each line of `skills/skills.lock` is a JSON object: `role`, `source` (`owner/repo@skill`), `skill`, `installs` (the install count `skills` showed, may be empty), `hash` (SHA-256 of the embedded file), `license`, `trimmed`, and, for the three snapshots, `pinned_from` (the upstream commit URL).
- **Pinned snapshots.** `web-design-guidelines` is a pinned snapshot of `command.md` of vercel-labs/web-interface-guidelines at commit `434b7f9` (the upstream skill only fetched its rules from a URL at run time, and the copy fetches nothing). `playwright-best-practices` is a condensed copy of currents-dev/playwright-best-practices-skill at `283d5cb` (upstream is an index of many reference files). `user-stories` is pinned to phuryn/pm-skills at `07b34f6`.
- **One requested skill is not vendored.** `user-story` from deanpeters/product-manager-skills is CC BY-NC-SA 4.0 (non-commercial, share-alike), which does not fit redistribution here. The PO uses `user-stories` from phuryn/pm-skills (MIT) instead. A skill whose licence does not allow redistribution is dropped, not paraphrased.
- **Licences.** MIT for most; Apache-2.0 for `frontend-design`, `webapp-testing` and `playwright-cli`. `frontend-code-review` (specialist) is under Dify's modified Apache-2.0 with two additional conditions; they are quoted in full in the notices. [skills/THIRD_PARTY_NOTICES.md](skills/THIRD_PARTY_NOTICES.md) lists every source repository, skill, licence and copyright holder as stated upstream, with the licence texts, and it is embedded too.
- **Pinned.** Nothing updates them automatically.

### Update or add a vendored skill

1. Fetch the candidate into a scratch directory, not the global skills folder: `npx skills add <owner/repo@skill> -y` run inside a temporary project. Do not use `-g`.
2. Read its `SKILL.md` and its licence (the file or the repository licence). If the licence does not allow redistribution, drop it.
3. Trim and sanitise it to the budget: about 8 KB per file and 24 KB per role. Remove installer commands, remote fetches, script calls for files you do not vendor and `allowed-tools` grants. Copy the result to `skills/<role>/references/<name>/SKILL.md`, once for every persona that carries it.
4. Add or update the entry in `skills/skills.lock`: `source`, `skill`, `installs`, `hash` (`sha256sum` of the new file), `license`, `trimmed` (`true`), and `pinned_from` if it is a snapshot.
5. Add the licence, repository and copyright holder to `skills/THIRD_PARTY_NOTICES.md`.
6. Run the tests below. `skills/skills_test.go` checks that every lock entry has its file embedded and its hash matches, that no file is over 8 KB or any role over 24 KB, that every entry has a licence and source, that every embedded reference is in the lock, and that the notices mention every licence, repository and skill in the lock.

## Run it

Offline, one process:

```bash
go run ./examples/langgraph/full_team_front
```

The run prints the services it started and the delivery report. It starts the hub, the manager and the workers on loopback and stops them at the end. It gives the PO a fake backend in a temporary workspace; `WORKSPACE_ROOT` and `app_url` do not apply to it.

With a real provider (the models are the defaults above):

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/full_team_front
OPENAI_API_KEY=...    go run ./examples/langgraph/full_team_front   # openai:gpt-4o-mini for every role
```

## Use it as an MCP server

The `serve` argument runs the whole team in the same process as an MCP server over stdio (`serve.go`). An editor starts it as a command; there is nothing else to start. It serves the team through `Bridge.MCPServer` in `internal/team/bridge.go`.

From the repository root:

```bash
go run ./examples/langgraph/full_team_front serve
```

Or build a binary and run it (starts faster than `go run`, which compiles on every start):

```bash
go build -o bin/team-mcp ./examples/langgraph/full_team_front
bin/team-mcp serve
```

On stdout the process writes only MCP protocol messages. Logs, the status lines of the console notifier and the compression report at shutdown go to stderr. The server stops on end of input (the editor closing) or on SIGINT/SIGTERM.

Offline fake models are used unless `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` is set (or `AGENT_MODEL*`, see above).

### Environment

Because the team runs in the server's process, every variable comes from the server's environment. In VS Code that is the `env` block of `mcp.json`; for Claude Code it is `--env` or the shell.

| Variable | Read by | Effect |
|---|---|---|
| `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `AGENT_MODEL`, `AGENT_MODEL_<ROLE>` | every worker, the manager and the hub when they start (`internal/team/models.go`) | real models instead of the scripted ones |
| `WORKSPACE_ROOT` | the PO backend tools (`internal/backend/backend.go`) | every `backend_roots` entry must lie under it; default is the working directory of the server |
| `DASHBOARD_ADDR` | the progress dashboard (`internal/team/dashboard_gw.go`) | `host:port` to serve it on; serve mode defaults to `127.0.0.1:8088`, other runs serve nothing unless it is set; see [Progress dashboard](#progress-dashboard) |
| `PLAYWRIGHT_CMD` | the QA `browser_test` tool, read at start (`main.go`) | command that runs the spec; empty means `NOT RUN` |
| `PLAYWRIGHT_ENV_ALLOW` | the `browser_test` command runner (`internal/qa/qa.go`) | parent variables passed to the browser run |
| `NOTIFY`, `NOTIFY_STATUSES`, ... | the manager's status notifier (`internal/tasktrack`) | where status lines go; `console` (stderr in serve mode) is the default |
| `BRIDGE_CIAM_CMD`, `BRIDGE_KNOWLEDGE_CMD` | the bridge | command lines of extra MCP servers whose tools are served next to the team's |
| `LAYA_URL`, `LAYA_API_KEY`, `LAYA_MODEL` | the compression decider (`main.go`) | a real laya-serve decider instead of the scripted one |

`MANAGER_URL` and `HUB_URL` are not read in serve mode: the bridge is wired to the in-process services. In serve mode the manager is protected by a random per-run `x-api-key` (32 random bytes, hex) that only the in-process bridge knows; an operator-set `MANAGER_API_KEY` is ignored. The hub and the workers' Agent Protocol servers listen on `127.0.0.1` and are not authenticated: any local process can reach them. Cancelling during startup (SIGINT, SIGTERM, the editor closing) ends the process promptly and cleanly.

### Tools

The server is named `engineering-team` and serves:

- `team_submit`: sends a demand. Its fields are `request` and, optionally, `backend_roots`, `backend_urls`, `docs_roots` and `app_url`. It returns `thread_id`, `run_id` and a status at once.
- `team_status`: for a `thread_id`, returns `status` (the request and each task), `report`, `history` (the handoffs, including each review and QA rejection and who it went back to) and `running`. `status`, `report` and the full `history` are filled in when the last step ends; while the run is going, the console notifier prints each status change on stderr.
- `team_queue`: the registered workers (with their current job) and the queued jobs.
- `team_cancel`: cancels a request by `thread_id` (ends its thread): queued jobs are dropped, running agents are stopped (they learn on their next heartbeat) and the delivery ends as `cancelled`. The manager's flow stops at its next step; the model call it is making then still finishes. Tokens of a cancelled job are not counted. Sending the same thread again starts a clean run.
- `team_ask_worker`: asks one worker a question, outside the delivery flow. A worker answering this way has no demand scope, so the PO's backend tools refuse.
- `team_present`: roll call, see [/present roll call](#present-roll-call). Every expected worker answers `Presente!` with its id, role and model.
- `team_memory`: recalls what the team recorded for a request (arguments `scope`, the request's `thread_id`, and `query`).
- `graph_run`, `graph_state`, `graph_resume`: generic tools for any graph on an Agent Server (`url` defaults to the manager).
- Any tools of the servers named by `BRIDGE_CIAM_CMD` and `BRIDGE_KNOWLEDGE_CMD`, if set.

### Register it

See [Use it from VS Code](#use-it-from-vs-code) for the VS Code, Claude Code and other client registrations.

### A demand

An example `team_submit` call (the arguments as JSON; the paths and URLs are placeholders). `backend_roots` must be under `WORKSPACE_ROOT` (or the server's working directory):

```json
{
  "request": "Add a coupon field to the checkout screen. Show a loading state while the coupon is checked, an empty state before any input, and an error message for an invalid code.",
  "backend_roots": ["/abs/path/to/workspace/backend"],
  "backend_urls": ["https://api.example.com/v1"],
  "app_url": "http://localhost:3000/checkout"
}
```

Then call `team_status` with the returned `thread_id` and repeat until `report` is present and `running` is empty:

```json
{ "thread_id": "<thread_id from team_submit>" }
```

## Use it from VS Code

### Prerequisites

- The Go 1.27 toolchain on `PATH` (`go version`; `go.mod` says `go 1.27`).
- The repository checked out and opened as the VS Code workspace folder. Then `go run ./examples/langgraph/full_team_front serve` works with the workspace as working directory, and `WORKSPACE_ROOT` defaults to it. The alternative is the built binary (`go build -o bin/team-mcp ./examples/langgraph/full_team_front`) started by absolute path, with `WORKSPACE_ROOT` set.
- Optional: `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`. Without a key the team runs offline with scripted models. In the chat that means: every request gets the same scripted coupon plan (five tasks, `T1` to `T5`) whatever you ask, and the scripted reviewers and QA return `T2`, `T3` and `T4` once. That is enough to check the wiring and to see the flow; it is not a real delivery.

### Claude Code in VS Code

Claude Code reads project-scoped MCP servers from `.mcp.json` at the project root (see [independent_engineering_team](../independent_engineering_team/README.md#claude-code), whose snippets this follows). Create `.mcp.json` at the workspace root:

```json
{
  "mcpServers": {
    "front-team": {
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team_front", "serve"],
      "env": {
        "WORKSPACE_ROOT": "/abs/path/to/this/repo",
        "PLAYWRIGHT_CMD": "",
        "PLAYWRIGHT_ENV_ALLOW": ""
      }
    }
  }
}
```

The values are literal. This README does not rely on `${VAR}` expansion in `.mcp.json`, because neither the sibling README nor the code shows it. Do not write a key into a file you commit: add `ANTHROPIC_API_KEY` with `claude mcp add` (below, local scope) or export it in the shell that starts VS Code and check in `/mcp` that the models are not offline. `PLAYWRIGHT_CMD` and `PLAYWRIGHT_ENV_ALLOW` can be left out to run without the browser tool. The `go run` form assumes the server starts with the repository root as working directory; if it does not (untested), use the binary with an absolute `command` instead.

The same server from the command line (`--scope local` is the default; `project` writes `.mcp.json`; `user` applies to every project; `--` separates the options from the command):

```bash
claude mcp add --transport stdio --scope project \
  --env WORKSPACE_ROOT=/abs/path/to/this/repo \
  front-team -- go run ./examples/langgraph/full_team_front serve

claude mcp add --transport stdio --scope user \
  --env WORKSPACE_ROOT=/abs/path/to/this/repo --env ANTHROPIC_API_KEY=... \
  front-team -- /abs/path/to/this/repo/bin/team-mcp serve
```

Project-scoped servers ask for approval the first time. Check the server with `claude mcp list`, or with `/mcp` in the chat: `front-team` should show as connected and list these tools: `team_submit`, `team_status`, `team_queue`, `team_cancel`, `team_ask_worker`, `team_present`, `team_memory`, `graph_run`, `graph_state` and `graph_resume` (plus the tools of `BRIDGE_CIAM_CMD` and `BRIDGE_KNOWLEDGE_CMD` if you set them); the server also lists the prompt `present`. Whether the chat panel of the Claude Code extension in VS Code uses the same configuration files and shows the same `/mcp` screen as the CLI was not tested here.

If it does not connect:

1. Run the command in a terminal from the repository root: `go run ./examples/langgraph/full_team_front serve`. It must print nothing on stdout until a client speaks (stdout is protocol only). Startup lines (`hub: up on ...`, `qa-1: qa up on ... (model ...)`) appear on stderr. Press Ctrl-D to stop it.
2. Read that stderr output. If a worker cannot start (typically a bad `AGENT_MODEL_<ROLE>` value or a missing key for the chosen provider), the process ends with `team did not come up` after 10 seconds; the worker's own error is not printed, so check those variables first.
3. Run `go version` and check that it is 1.27 or newer, and that `go` is on the `PATH` of the process that starts the server (VS Code started from a launcher may have a shorter `PATH` than your shell).
4. Check the paths: the working directory for `go run`, the absolute path of the binary, `WORKSPACE_ROOT`. The first `go run` compiles the whole example, so the first connection can be slow; the team gives up if its workers are not up within 10 seconds, so prefer the built binary.

### Copilot Chat in VS Code (Agent mode)

Create `.vscode/mcp.json` in the workspace (VS Code's format uses `servers`, each with `"type": "stdio"`, as in the sibling README's [Copilot snippet](../independent_engineering_team/README.md#github-copilot-vs-code)). `go run` form:

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team_front", "serve"],
      "env": { "ANTHROPIC_API_KEY": "${env:ANTHROPIC_API_KEY}" }
    }
  }
}
```

Built binary form:

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "${workspaceFolder}/bin/team-mcp",
      "args": ["serve"],
      "env": {
        "WORKSPACE_ROOT": "${workspaceFolder}",
        "ANTHROPIC_API_KEY": "${env:ANTHROPIC_API_KEY}"
      }
    }
  }
}
```

Leave the `ANTHROPIC_API_KEY` line out to run offline. Then open the file and click **Start** above the server. In Copilot Chat choose **Agent** and enable `front-team` in the tools picker.

Other MCP clients (Cursor, Kilo Code, Opencode) take a stdio command in the same shape (`command`, `args`, `env`); the snippets in [independent_engineering_team](../independent_engineering_team/README.md#4-register-it-in-each-tool) show each client's file, with `command` and `args` replaced by the ones above. They were not tested against this server.

### Use it from the chat

Steps with prompts you can paste into the Claude Code chat (or Copilot Agent mode). The chat decides when to call the tools; naming the tool makes it do so.

1. **Send a demand.** Prompt:

   > Use the `team_submit` tool with request "Add a coupon field to the checkout screen. Show a loading state while the coupon is checked, an empty state before any input, and an error message for an invalid code.", backend_roots ["/abs/path/to/this/repo/backend"], backend_urls ["http://localhost:8080"], app_url "http://localhost:3000/checkout". Then tell me the thread_id.

   The fields: `request` is what the team must deliver. `backend_roots` are the source trees the PO may read; give absolute paths, each under `WORKSPACE_ROOT` (a relative one is taken relative to it, and one outside it is refused). `backend_urls` are the APIs the PO may call with `GET`; only those hosts and ports are reachable. `app_url` is the running front end QA may open with `browser_test` (needs `PLAYWRIGHT_CMD`). All three are optional; without roots and URLs the PO reads nothing and the spec says so. The call returns at once:

   ```text
   thread_id=35a034fed3493843fc60f393894dd7ed run_id=646bef17962e894921b0a5e4fe5c0d92 status=pending
   ```

2. **Poll the status.** Prompt: "Call `team_status` with that thread_id every few seconds until it reports a `report`, then show me the result." The fields: `status` maps the request (key `""`) and each task (`T1`...) to its stage (`received` ... `completed`); `report` is the manager's summary; `history` is the list of handoffs; `running` lists the nodes still to run (`null` when the run ended). `status`, `report` and the full `history` are filled in when the last step ends, so poll until `report` is present.

3. **Read the history.** Joint-review and QA returns show as lines such as `T2: review returned by leader -> mid-1 | tech-leader: ...` and `T4: qa returned by qa-1 -> senior-1`. The next line for that task, `T2: mid-1 -> review`, shows the same developer building it again.

4. **See the team.** Prompt: "Call `team_queue`." It returns the registered workers (`id`, `role`, `url`, `seen`, and `job` while one is busy; 12 of them) and the `queued` jobs.

5. **Ask one agent.** Prompt: "Call `team_ask_worker` with worker `leader` and question: which developers are busy?" The worker id comes from `team_queue`. The question runs outside the delivery flow, so it carries no demand scope: asking `po` to read the backend this way is refused.

6. **Recall what the team recorded.** Prompt: "Call `team_memory` with scope <thread_id> and query `coupon`." The scope is the request's `thread_id`.

To read the PO spec, call `graph_state` with the `thread_id` and look at `values.spec`.

A worked example, offline (no key, no backend named). User prompt: "Use the team_submit tool with request 'Add a coupon field to checkout'." Tool call:

```json
{ "name": "team_submit", "arguments": { "request": "Add a coupon field to checkout" } }
```

Result: `thread_id=35a034fe... run_id=646bef17... status=pending`. User prompt: "Check it with team_status." Tool call `team_status` with `{ "thread_id": "35a034fe..." }`; the offline result, trimmed (the real one lists every task):

```json
{
  "history": [
    "manager -> product-owner",
    "product-owner (po) done",
    "tech-leader (leader) -> 5 tasks",
    "T2: mid-1 -> review",
    "T2: review returned by leader -> mid-1 | tech-leader: 1. The hook recomputes the discount in the component: show the discount_percent and total the API returned, ...",
    "T2: mid-1 -> review",
    "T2: review approved by specialist-1 and leader",
    "T2: qa approved by qa-1",
    "T2: approved",
    "T4: senior-1 -> review",
    "T4: review approved by specialist-1 and leader",
    "T4: qa returned by qa-1 -> senior-1",
    "T4: senior-1 -> review",
    "...",
    "manager -> requester"
  ],
  "report": "REPORT:\n- Shipped: T1, T5, T2, T3, T4\n- Blocked: none\n- Next: roll out behind a flag for 5% of checkout traffic.",
  "running": null,
  "status": { "": "completed", "T1": "completed", "T2": "completed", "T3": "completed", "T4": "completed", "T5": "completed" }
}
```

Troubleshooting:

- Tools not listed: the server did not connect; see "If it does not connect" above.
- `team_submit` returns but `team_status` never reaches a `report`: with a real key, a worker failed or a model call is failing. Read the server's stderr (run the command in a terminal to see it) and check the key, the model names (`AGENT_MODEL_<ROLE>`) and the network.
- The PO spec has `UNVERIFIED: no backend evidence` under `OPEN ITEMS:`: the demand named a backend but the spec has no `EVIDENCE:` block or the PO made no backend call that got an answer. The roots or URLs may have been refused (outside `WORKSPACE_ROOT`, a host not named, a blocked address). Fix the `team_submit` arguments. The same marker appears if the PO tools could not report their calls to the hub (the failure is logged as `backend: counting a call ...`, the tool call itself still succeeds) or the manager could not read the count: the rule then fails closed.
- `browser_test` says `NOT RUN`: `PLAYWRIGHT_CMD` is empty in the server's environment, or the demand has no valid `app_url`. The reason follows `NOT RUN:`.

### /present roll call

An easter egg to check that the chat reached the team: a school roll call. The `team_present` tool asks every expected worker (the 12 of this example) to answer one line that starts with `Presente!` and names its id, role and model, in parallel and at most 30 seconds each. The MCP prompt `present` ("Roll call: every persona answers Presente") only tells the chat to call that tool and print the result, because a prompt alone cannot make anyone answer. If the chat cannot find the tool, the prompt text makes it say the integration is not working.

How to call it: ask in plain words, "call the `team_present` tool and print the roll call", or use the slash command. Slash spellings (UNVERIFIED: not confirmed against the clients, taken from how they name MCP prompts): in Claude Code `/mcp__<server>__present`, so `/mcp__front-team__present` when the server is registered as `front-team` as above; in Copilot Chat `/mcp.<server>.present`, here `/mcp.front-team.present`.

What the roll shows: one row per worker with its id, role and model, then `N of 12 present`, then `Absent:` with the ids that did not answer. The model column comes from the worker itself: it reports the model it was built with when it registers with the hub, so the roll does not depend on the model knowing its own name. Offline every row says `offline`. With `ANTHROPIC_API_KEY` set, each role runs its own model (`po` on `anthropic:claude-opus-5-5`; `leader`, `specialist` and `qa` on `anthropic:claude-sonnet-5-5`; `junior`, `mid` and `senior` on `anthropic:claude-haiku-5-5`), unless `AGENT_MODEL` or `AGENT_MODEL_<ROLE>` overrides it, so the roll shows whether each role runs on the model configured for it.

The roll is checked against the expected team, not only against the workers the hub knows. A worker that crashed and whose 15 second lease expired has left the hub, and it still gets a row: `absent: not registered (lease expired or never started)`, and the count drops. A worker that is registered but fails, times out or answers nothing is also absent. A registered worker that is not one of the 12 is listed with `extra` and does not count toward the total. Present means a non-empty answer: a real model that ignores the `Presente!` instruction still counts as present. A good roll tells you the chat integration works end to end: tool call, hub, every worker's agent, model.

Real output of the offline team over stdio (ordered as the cast; the answers are the scripted ones, a real model words its own):

```
ROLL CALL
- po (po) [model offline] present: Presente! po (po), com a spec debaixo do braço
- leader (leader) [model offline] present: Presente! leader (leader), organizando a fila
- junior-1 (junior) [model offline] present: Presente! junior-1 (junior), de caderno aberto
- junior-2 (junior) [model offline] present: Presente! junior-2 (junior), de caderno aberto
- mid-1 (mid) [model offline] present: Presente! mid-1 (mid), no ritmo
- mid-2 (mid) [model offline] present: Presente! mid-2 (mid), no ritmo
- senior-1 (senior) [model offline] present: Presente! senior-1 (senior), sempre pronto
- senior-2 (senior) [model offline] present: Presente! senior-2 (senior), sempre pronto
- specialist-1 (specialist) [model offline] present: Presente! specialist-1 (specialist), de lupa na mão
- specialist-2 (specialist) [model offline] present: Presente! specialist-2 (specialist), de lupa na mão
- qa-1 (qa) [model offline] present: Presente! qa-1 (qa), testando até a chamada
- qa-2 (qa) [model offline] present: Presente! qa-2 (qa), testando até a chamada
12 of 12 present
```

With one worker down for longer than the lease (the same test run, offline), the roll ends like this:

```
- qa-2 (qa) [model unknown] absent: not registered (lease expired or never started)
11 of 12 present
Absent: qa-2
```

### Security notes

- The server is a local stdio process started by your editor, with your user's permissions and the environment you gave it. The chat is the trust root: whatever the chat passes in `team_submit` (`backend_roots`, `backend_urls`, `app_url`) becomes the scope of the request.
- File reads by the PO are bounded by `WORKSPACE_ROOT` (the server's working directory if unset) and by the named roots; symlinks and `..` cannot leave them. Keep `WORKSPACE_ROOT` narrow.
- The `url` argument of `graph_run`, `graph_state` and `graph_resume` lets the chat make the bridge call any Agent Server URL. The manager's key is sent only when `url` is empty or equals the manager's own URL; any other URL gets no key. `team_ask_worker` has no `url`: it reaches only the workers registered in the hub, on loopback.
- In serve mode no role has a `shell` tool: developers return code as text. The offline demo run keeps a hardened shell (see Shell under How to apply it). The PO never has a shell.
- `browser_test` runs the command you set in `PLAYWRIGHT_CMD`, without a shell, with a minimal environment: your provider keys do not reach it unless listed in `PLAYWRIGHT_ENV_ALLOW`. The spec it runs is written by a model.
- In serve mode the manager requires a random per-run key that only the in-process bridge holds; an operator-set `MANAGER_API_KEY` is ignored. The hub and the workers' servers listen on `127.0.0.1` and are not authenticated, so any local process can reach them.
- The call counter behind the evidence rule lives in the hub (`POST /calls/{id}`, `GET /calls/{id}`), which is loopback-only and unauthenticated: any local client can add calls to a request, while the model cannot reach it. A late call from an earlier run on the same thread can still count after a new run resets the counter.
- The PO's `project_doc_write` can overwrite or extend any `.md` file under the `docs_roots` the chat passes in `team_submit`, with your user's permissions, as long as they lie under `WORKSPACE_ROOT`. It cannot write other extensions, other directories, or through symlinks that leave them. A model decides the content: review the diff (`git diff`) of those files after a run.
- The dashboard has no authentication. It listens on loopback by default and shows statuses, timings and token counts only (no prompts, no outputs). Projects register with the owner at `POST /api/register`, which accepts only loopback peers and loopback URLs. The hub's `POST /cancel/{scope}`, `POST /usage/{id}` and `POST /workers/{id}/activity` are as open as the rest of the hub: loopback, unauthenticated.

## The PO keeps the project documents

The product owner is the only role that can write, and only through `project_doc_write`: markdown files (`.md`, up to 256 KB), with `replace` or `append`. Where is set by each demand: the `docs_roots` field of `team_submit` lists the directories (relative to the workspace, or absolute under `WORKSPACE_ROOT`) where the PO may write, and a relative path in the tool starts at the first one. A demand with no `docs_roots` gives the PO no write access at all, like a demand with no backend roots gives it no read access. Anything else is refused: other extensions, paths outside the named directories, `..`, and symlinks that lead out. The manager lists the directories in the PO prompt and tells it to keep the documentation, the plan and the next steps current. Source code, and every other role, stay read-only for it.

Token use: every worker and the manager's own agent run through the same compression middleware (old turns caveman-compressed, the Ponytail note when the rules ask for it, TOON for JSON tool results).

## Progress dashboard

A read-only web page shows every request and task with its status, progress bar and execution time, and every persona (worker) with its state, current job, jobs done and busy time. It refreshes every 2 seconds and shows statuses, timings and token counts only, never prompts or outputs. Tokens are listed per persona (the manager counts as one), per task, per request and for the whole project; the knowledge model's own calls are not counted. Each persona row also names its skill folder (with its vendored references, in the tooltip) and, while it works, what it is doing: the reference it consults (`rag_answer`, `cag_answer`, `knowledge_answer`, team memory), the tool it calls, or the model.

Every project has its own URL, `http://127.0.0.1:8088/p/<project>/` (`/?project=<project>` redirects there), and the header has a project selector. The first team to start owns the port and lists the others; a team that finds the port taken serves on a free loopback port and registers with the owner (loopback only), so one address covers every project. A project that stops answering shows as offline; if the owner exits, the others register again to whichever team binds the port next.

- `serve` mode (what `team-mcp-run serve` starts) opens it on `http://127.0.0.1:8088` (loopback only).
- Demo and other runs: set `DASHBOARD_ADDR=host:port` (the demo exits when the run ends).
- To reach it from another machine, set `DASHBOARD_ADDR=0.0.0.0:8088` behind your own auth proxy; the page has no authentication.

## Test it

```bash
go test -race -count=1 ./examples/langgraph/full_team_front/...
go vet ./examples/langgraph/full_team_front/...
```

One test: `go test -race -count=1 -run TestJointReviewNeedsBothApprovals ./examples/langgraph/full_team_front/internal/team`. The tests, by package:

- `main` (`main_test.go`, `serve_test.go`): `TestAllowedProgramsWithLiveModelButNoKey`, `TestAllowedProgramsWithProviderKey`, `TestScriptedPonytailFollowsRule`, `TestOfflineRunReportsTOON`, `TestPOGetsNoShell`, `TestServeStackHasNoShell`, `TestDemoShellRejectsExpansionAndRedirects`, `TestShellEnvHasNoSecrets`, `TestDemoShellConfinesPaths`, `TestDemoShellRejectsFlagAndExpansionBypasses`, `TestDemoShellRejectsGlobAndQuoteBypasses`, `TestDemoShellRejectsStructuralGlobBypasses`, `TestDemoShellRejectsBareBracketGlobThroughSymlink`, `TestShellRefusesDotGlobs`, `TestOpenTasksOnlyOffline`, `TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`, `TestServeStopsOnContextCancel`, `TestServeCancelDuringStartupIsClean`, `TestServeRestoresGlobals`, `TestServePresentOverStdio`. The serve tests also check that nothing but protocol reaches stdout.
- `internal/team`: `TestBrainScenarioIsFrontEnd`, `TestGraphToolsSendKeyOnlyToManager`, `TestStartLocalWithHonoursCancel`, `TestManagerAPIKeyProtectsManager`, `TestOfflineRunPOUsesBackendTools`, `TestPresentToolRollCall`, `TestPresentAbsentWhenWorkerDown`, `TestPresentAbsentAfterLeaseExpired`, `TestPresentExtraWorkerNotCounted`, `TestPresentPromptListed`, `TestBrainRollCall`, `TestRegisterCarriesModel`, `TestQAPromptCarriesSpec`, `TestQAOutputTracesCriteria`, `TestDossierDropsUnsafeAppURL`, `TestSkillIncludesReferences`, `TestSkillUnknownRole`, `TestNoArchitectInSkills`, `TestSkillSectionsHaveNoFrontmatter`, `TestManagerSkillHasNoFences`, `TestReservationIsAtomic`, `TestPreferenceOrder`, `TestLeaseExpiryRequeues`, `TestJobFailsAfterMaxAttempts`, `TestKnowledgeAndMemoryOverMCP`, `TestNormalizeModel`, `TestTeamDeliversOffline`, `TestBridgeQueueAndAskUnknown`, `TestBridgeToolSetAndProxy`, `TestStartLocalWithMiddleware`, `TestCastHasTwelveWorkers`, `TestDefaultModels`, `TestPinnedJobWaitsForItsWorker`, `TestPinnedJobFallsBackWhenLeaseExpired`, `TestJointReviewNeedsBothApprovals`, `TestRejectedWorkReturnsToSameDeveloper`, `TestPlanParsesPreferField`, `TestThreadScopeFailsClosedOnAmbiguity`, `TestPlanValidatesPrefer`, `TestPlanNormalisesComplexity`, `TestTaskBlockedAfterMaxRounds`, `TestScopeRoundTripOverHTTP`, `TestSubmitStoresScope`, `TestToolSeesJobScope`, `TestScopeNeverResolvedByPrefix`, `TestScopeIdCollision`, `TestPOSpecWithoutEvidenceIsMarkedUnverified`, `TestVerifiedSpec`, `TestHubCountsCallsAndSetScopeResets`, `TestCallsRoundTripOverHTTP`, `TestEvidenceRuleWorksAcrossServices`.
- `internal/backend`: `TestReadRefusesOutsideRoots`, `TestReadWithinRoot`, `TestGrepStaysInsideRoot`, `TestGrepLimits`, `TestRootsResolveAgainstWorkspace`, `TestGetRefusesNonGetAndUnnamedHost`, `TestGetRefusesRedirectToOtherHost`, `TestGetRefusesLoopbackUnlessNamed`, `TestGetNamedLocalhost`, `TestGetBodyCapped`, `TestNoScopeMeansNoAccess`, `TestReportsGuardedCallsPerRequest`, `TestReportFailureDoesNotFailTheCall`.
- `internal/qa`: `TestBrowserTestNotRunWithoutCommand`, `TestBrowserTestNotRunWithoutAppURL`, `TestBrowserTestRejectsNonHTTPAppURL`, `TestBrowserTestRunsRunnerWithScopeURL`, `TestCommandRunnerUsesNoShell`, `TestCommandRunnerCapsOutput`, `TestCommandRunnerMinimalEnv`, `TestCommandRunnerTimeoutKillsProcessGroup`, `TestToolNotGivenToOtherRoles`.
- `internal/tasktrack`: `TestTaskFlowAllowsRework`, `TestIllegalMoveIsRejectedAndNotRecorded`, `TestWebhookSinksAndFilter`, `TestNotifierFailureDoesNotBlock`, `TestFromEnvAndEmailMessage`.
- `internal/team` (dashboard, tokens, cancel): `TestBuildDash`, `TestBuildDashNoTasksIsEmptyArray`, `TestBuildDashTokens`, `TestDashboardHandler`, `TestGatewayProxiesRegisteredProject`, `TestCancelScope`, `TestUsageMeter`.
- `internal/backend` (PO documents): `TestDocPath`, `TestDocToolWritesOnlyForPO`.
- `skills`: `TestEveryLockEntryIsEmbedded`, `TestVendoredSkillWithinBudget`, `TestEveryLockEntryHasLicense`, `TestEveryRoleDirHasOwnCopies`, `TestNoticesCoverLockedLicensesAndSources`, `TestPinnedFromIsPlausibleCommit`.

The serve tests that drive the offline team (`TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`) skip when any role has a model spec (a key or `AGENT_MODEL*` is set).

## Test it in production mode

1. Register the server (see above) with `ANTHROPIC_API_KEY` (or `OPENAI_API_KEY`). In the server log on stderr, each worker prints `up on ... (model ...)`; check that the models are the ones you set (`AGENT_MODEL_<ROLE>` overrides the defaults).
2. Send one demand through the MCP chat with `team_submit`, with `backend_roots` and/or `backend_urls` pointing at a real backend (and `app_url` for the browser run). Poll with `team_status` until `report` is present.
3. In the `history` of `team_status`, look for a joint-review rejection (`review returned by ... -> <developer>`) and a QA rejection (`qa returned by ... -> <developer>`). The next line for that task must show the same developer building it again (`<task>: <developer> -> review`). Models do not always reject; if none rejected, send a demand with a deliberate gap.
4. Check the PO spec: call `graph_state` with the `thread_id` and read `values.spec`. It must contain an `EVIDENCE:` block whose every endpoint matches the backend you pointed at, and the open items must not contain `UNVERIFIED: no backend evidence`. That marker means the PO did not read the backend.
5. Try a demand that names a host the PO should not reach, and a URL that redirects to another host. Both must be refused in the tool output.
6. Without `PLAYWRIGHT_CMD`, or without `app_url`, QA's `browser_test` result must say `NOT RUN` with the reason. With both set (and `PLAYWRIGHT_BROWSERS_PATH`, and Chrome installed where the server runs), it must show the Playwright output.
7. Keep `WORKSPACE_ROOT` and the backend roots as narrow as the work allows: the backend tools read files on the host that runs the server. In serve mode no role has a shell. If you run the demo with a live model, keep the shell allowlist tight (`ls`, `grep` and `echo`); the shell has no sandbox.
8. Open the dashboard (`http://127.0.0.1:8088/` redirects to the project page; the log line `dashboard: ...` has the URL) while a demand runs. The request and its tasks must move through the statuses, each persona must show `busy` with its skill and what it is doing, and the token counts must grow (a live model; offline there are none). Start a second project with its own `WORKSPACE_ROOT`: it must appear in the project selector.
9. Call `team_cancel` with the `thread_id` of a running demand. The dashboard must show the request and its open tasks as `cancelled`, the workers must go back to `idle`, and `team_status` must stop advancing. After a demand that changes what is true, run `git diff` on the `docs_roots` you passed to see what the PO wrote.

## See also

- [full_team](../full_team/README.md): the copied team, with the compression layer documented in full
- [independent_engineering_team](../independent_engineering_team/README.md): the same team split into services, with Docker compose, and the registration snippets for each MCP client
- [compress_shell_rtk](../compress_shell_rtk/README.md), [compress_toon](../compress_toon/README.md), [compress_ponytail](../compress_ponytail/README.md)
