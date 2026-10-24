# full_team

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

The engineering team flow (hub, manager, workers over Agent Protocol and MCP, team memory, leases) with the `compress` layer on every worker: each agent's model calls are compressed, its shell tool routes simple commands through `rtk`, and two more rewrites run on the same middleware: **Ponytail** (a note asking for the least code) and **TOON** (JSON tool results written in a shorter form). Offline it runs with a scripted decider, so the effect of each lever shows without a model.

This folder is self-contained. It does not import `examples/langgraph/independent_engineering_team`; the code was copied from it.

## What it does

- **Copied from `independent_engineering_team`**: `internal/team` (hub, manager, workers, knowledge, team memory, tests), `skills/` (agent personas) and `kb/` (standards and checklists). The copy adds only the hooks below, so the original stays unchanged.
- **Hooks added to the team**: `WorkerConfig.Middleware` and `WorkerConfig.Tools` (empty means unchanged), `LocalOptions`, and `StartLocalWith`, which gives middleware and tools to every worker. `StartLocal` is `StartLocalWith` with nothing added. `TestStartLocalWithMiddleware` checks that worker middleware runs.
- **Model calls**: every worker's agent gets `middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}`. Old Human and AI turns are caveman-compressed per call; tool calls and results keep their ids and order. Stored history is not rewritten.
- **Ponytail**: when the decider answers `ponytail`, the last Human message of each per-call copy gets the Ponytail directive (climb the ladder: does it need to exist, reuse what the codebase has, stdlib, native feature, installed dependency, one line; lazy about the solution, never about reading). It never asks the model to skip validation, error handling, security, accessibility or the tests that guard risky logic. The note is not stored and is not in the system prompt.
- **TOON**: when the decider answers `toon_tool_results`, each tool result whose text is a JSON object or array is rewritten to TOON, but only when the TOON text is strictly shorter. Offline, every worker calls the `open_tasks` tool once per turn (`team.OfflineWithTool`): it returns the backlog as a JSON array of uniform objects, which TOON writes as a table. Its result reaches the model and gets rewritten. Other tool output is untouched.
- **Shell**: each worker gets a `shell` tool (`{command}`) over `backends.LocalShell` with `AllowedCommands` (`go git ls grep echo` offline; `ls grep echo` when a provider key is set or any role has a live model, see `allowedPrograms`) and `Rewrite: rtk.Rewrite`. `RTK.Env` is the shell's `Env`, so `rtk` is looked up on the same PATH the shell uses. `rtk.Executor` records output sizes for the routing decision. The commands see a minimal environment (`PATH` and a `HOME` inside the scratch directory, so no provider key reaches them). A command with `<`, `>`, `$`, a backtick, a backslash or a brace is refused. Path arguments and the values of `--flag=value` and attached short flags (`-f/etc/x`) that resolve outside the workspace, symlinks included, are refused (`shell: paths outside the workspace are not allowed`). Flags that read a file of patterns or follow symlinks (`grep -f`, `--file`, `--exclude-from`, `--include-from`, `-R`) and `-C`, `--git-dir`, `--work-tree` for `go` and `git` are denied. A glob in a path argument is refused when its literal prefix (the part before the first `*` or `?`) is outside the workspace, when it contains `[`, or when it has `..` after a wildcard; bare patterns such as `[0-9]` or `*.go` and workspace-relative globs such as `src/*.tsx` work. A wildcard that can expand to `.` or `..` (`.*`, `.?`) is refused, and a bare bracket glob is checked through a superset pattern (each `[...]` read as `?`), so a match that leaves the workspace is refused. A name that does not exist on disk, such as a grep pattern `/api/coupon`, is allowed. This is a guard for a demo, not a sandbox.
- **Decider**: scripted offline (answers every question as confident, so compression and TOON apply). `ponytail` is answered by the rule (`compress.RuleDecider`), so a note appears only where the conversation has a real share of code blocks. With `LAYA_URL` set, `compress.HTTPDecider` asks a laya-serve endpoint instead.
- **Report**: after the run, it prints how many model calls were rewritten, the estimated tokens before and after (bytes/4), how many calls carried the Ponytail note, how many tool results were rewritten to TOON with the bytes they saved, and whether `rtk` is on PATH. The counts come from a wrapper around the middleware (`meter`), not from changes to the library. One offline run showed 46 model calls, 31 per-call TOON rewrites saving 11625 bytes, and 46114 estimated tokens before and 42964 after. The printed numbers vary a little between runs (goroutine timing). The offline run may show no Ponytail notes: the rule adds one only when the conversation has a real share of code, and the scripted run has little. A Ponytail note adds about 0.8 KB to a call; it pays off only when the model's output shrinks, which the offline scripted run does not measure.

`native/` is the same program with the decider answered by an in-process laya model. It is a package of the root module: `native.go` has the build tag `laya_native` and imports `laya-go`; `stub.go` has `!laya_native` and is used by default. Its scripted fallback has no `open_tasks` tool, so its report shows no TOON rewrites offline. That fallback answers `ponytail` no, and its meter does not count Ponytail notes.

## How to apply it

- Middleware: `Keep` is how many recent turns stay as they are (default 4; this demo uses 1 because its threads are short).
- Shell: `AllowedCommands` in `shellTool`. Never list shells, interpreters, `env`, `xargs` or `find`. Note that `go` and `git` can run code, so the host needs a sandbox for anything untrusted. They are offered only in the offline scripted run: a live model (a provider key, `AGENT_MODEL` or `AGENT_MODEL_<ROLE>`) gets `ls`, `grep` and `echo` only.
- Decider: `decider()` in `main.go`. Wrap any decider in `compress.NewGuarded` so a slow or wrong one falls back to the rules.
- Turn Ponytail off: offline, the rule answers it; to force it off, answer `P: 0.05` for `compress.QPonytail` in `scripted()`. Turn TOON off the same way for `compress.QTOON`. With laya, the decider's answers decide; wrap it so those two ids always come back false if you want them off there. Turning off one or both does not change the tools or the rest of the flow.
- Turn TOON on for your own tools: any tool whose result is a JSON object or array benefits, as long as it is a uniform list of objects with primitive fields; `open_tasks` in `main.go` is the example.

## Run it

Offline, one process, real HTTP between services:

```bash
go run ./examples/langgraph/full_team
```

Against a laya-serve endpoint:

```bash
LAYA_URL=http://localhost:8099/v1/systemone LAYA_API_KEY=... go run ./examples/langgraph/full_team
```

Native (in-process laya model):

```bash
export LAYA_ONNXRUNTIME_LIBRARY=/path/to/libonnxruntime.so
export LAYA_TOKENIZERS_LIBRARY=/path/to/libtokenizers
export LAYA_BUNDLE_DIR=/path/to/bundle
CGO_ENABLED=1 go run -tags laya_native ./examples/langgraph/full_team/native
```

Without `-tags laya_native`, or without `LAYA_BUNDLE_DIR`, the native program uses the scripted decider and prints why. The native build needs linux/amd64 and FP32 models, as in `decider_laya_native`.

## Chat integration and /present

`serve` runs the same team in one process and serves it as an MCP server on stdio, so a chat (Claude Code, Copilot Chat in Agent mode) can drive it:

```bash
go run ./examples/langgraph/full_team serve
go build -o bin/team-mcp ./examples/langgraph/full_team && ./bin/team-mcp serve
```

Stdout carries only the MCP protocol. Startup lines (`hub: up on ...`, `qa-1: qa up on ... (model ...)`) go to stderr. It stops on end of input (Ctrl-D) or on a signal. Serve mode differs from the demo in three ways: no worker gets the `shell` tool, the manager runs with a random API key generated at start (an operator-set `MANAGER_API_KEY` is ignored; only this process's bridge knows the key), and no demand is submitted until the chat calls `team_submit`. The tools are `team_submit` (field `request`, and optionally `docs_roots`), `team_status`, `team_queue`, `team_cancel`, `team_ask_worker`, `team_present`, `team_memory`, `graph_run`, `graph_state` and `graph_resume`.

Two more facts about serve mode. The `url` argument of `graph_run`, `graph_state` and `graph_resume` lets the chat make the bridge call any Agent Server URL; the manager's key is sent only when `url` is empty or equals the manager's own URL, and any other URL gets no key. The hub and the workers' servers listen on `127.0.0.1` and are not authenticated, so any local process can reach them.

Register it in the client (same shapes as [Use it from VS Code](../full_team_front/README.md#use-it-from-vs-code) in `full_team_front`, which this follows; the first `go run` compiles the example, and the team gives up after 10 seconds if its workers are not up, so prefer the built binary if the chat times out). `.mcp.json` for Claude Code, at the project root:

```json
{
  "mcpServers": {
    "team": {
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team", "serve"]
    }
  }
}
```

or from the command line:

```bash
claude mcp add --transport stdio --scope project team -- go run ./examples/langgraph/full_team serve
claude mcp add --transport stdio --scope user --env ANTHROPIC_API_KEY=... team -- /abs/path/to/this/repo/bin/team-mcp serve
```

`.vscode/mcp.json` for Copilot Chat (enable the server in the tools picker of Agent mode):

```json
{
  "servers": {
    "team": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team", "serve"]
    }
  }
}
```

Neither the Claude Code extension panel in VS Code nor the Copilot snippet was tested against this server here; they are the shapes the sibling README uses.

### /present

An easter egg to check that the chat reached the team: a school roll call. Every registered worker is asked, in parallel (30 seconds each at most), to answer one line that starts with `Presente!` and names its id, role and model. The `team_present` tool does the asking and returns the roll. The MCP prompt `present` ("Roll call: every persona answers Presente") only tells the chat to call that tool and print the result, because a prompt alone cannot make anyone answer. If the chat cannot find the tool, the prompt text makes it say the integration is not working.

Slash spellings (UNVERIFIED: not confirmed against the clients, taken from how they name MCP prompts): in Claude Code `/mcp__team__present` when the server is registered as `team`; in Copilot Chat `/mcp.team.present`. If the slash command does not show up, ask in plain words: "call the team_present tool and print the roll call".

The model column comes from the worker itself: it reports the model it was built with when it registers with the hub, so the roll does not depend on the model knowing its own name. Offline every row says `offline`. With `ANTHROPIC_API_KEY` set, each role runs its own model (`po` and `architect` on `anthropic:claude-opus-5-5`; `leader`, `senior` and `specialist` on `anthropic:claude-sonnet-5-5`; `junior`, `mid` and `qa` on `anthropic:claude-haiku-5-5`), unless `AGENT_MODEL` or `AGENT_MODEL_<ROLE>` overrides it, so the roll shows each role on the model configured for it. The roll is built from the expected roster (the 11 workers of the cast) merged with what the hub lists, so a worker that crashed and lost its lease, or never started, still gets a row (`absent: not registered (lease expired or never started)`) and the summary reads `10 of 11 present` followed by `Absent: <id>`. A worker that fails, times out or answers nothing is also listed as absent. A registered worker outside the roster is listed with the marker `extra (not in the roster, not counted)` and is not counted; the summary is always `N of M present` with M the size of the roster. Real output of the offline team (11 workers, ordered by id; the answers are the scripted ones, a real model words its own):

```text
ROLL CALL
- architect (architect) [model offline] present: Presente! architect (architect), com o diagrama na mão
- junior-1 (junior) [model offline] present: Presente! junior-1 (junior), de caderno aberto
- junior-2 (junior) [model offline] present: Presente! junior-2 (junior), de caderno aberto
- leader (leader) [model offline] present: Presente! leader (leader), organizando a fila
- mid-1 (mid) [model offline] present: Presente! mid-1 (mid), no ritmo
- po (po) [model offline] present: Presente! po (po), com o backlog debaixo do braço
- qa-1 (qa) [model offline] present: Presente! qa-1 (qa), testando até a chamada
- qa-2 (qa) [model offline] present: Presente! qa-2 (qa), testando até a chamada
- senior-1 (senior) [model offline] present: Presente! senior-1 (senior), sempre pronto
- specialist-1 (specialist) [model offline] present: Presente! specialist-1 (specialist), de lupa na mão
- specialist-2 (specialist) [model offline] present: Presente! specialist-2 (specialist), de lupa na mão
11 of 11 present
```

With a worker down the last lines read `10 of 11 present` and `Absent: qa-2`.

## The PO keeps the project documents

The product owner is the only role that can write, and only through `project_doc_write`: markdown files (`.md`, up to 256 KB), with `replace` or `append`. Where is set by each demand: the optional `docs_roots` field of `team_submit` lists the directories (relative to the workspace, or absolute under `WORKSPACE_ROOT`, else the server's working directory) where the PO may write, and a relative path in the tool starts at the first one. A demand with no `docs_roots` gives the PO no write access at all. Anything else is refused: other extensions, paths outside the named directories, `..`, and symlinks that lead out. The directories reach the tool through the hub, by the job's exact thread (`internal/docs`), and the manager lists them in the PO prompt. A model decides the content: review `git diff` of those files after a run. Source code, and every other role, stay read-only for it.

## Progress dashboard and cancelling

A read-only web page shows every request and task with its status, progress bar, execution time and token use, and every persona (worker) with its state, current job, skill, what it is doing now (the reference it consults, the tool it calls, or the model), jobs done, busy time and tokens. Tokens are also totalled per task, per request and for the project; the knowledge model's own calls are not counted. It refreshes every 2 seconds and shows statuses, timings and token counts only, never prompts or outputs.

- `serve` mode opens it on `http://127.0.0.1:8088` (loopback only); other runs serve it only when `DASHBOARD_ADDR=host:port` is set. The page has no authentication: for another machine use `DASHBOARD_ADDR=0.0.0.0:8088` behind your own auth proxy.
- Every project has its own URL, `http://127.0.0.1:8088/p/<project>/` (`/?project=<project>` redirects there), and the header has a project selector. The first team to start owns the port and lists the others; a team that finds it taken serves on a free loopback port and registers with the owner (loopback only). The project is `WORKSPACE_ROOT`, else the working directory.
- `team_cancel` (MCP tool, argument `thread_id`) cancels a request: queued jobs are dropped, running agents are stopped on their next heartbeat and the delivery ends as `cancelled`. The manager's current model call still finishes, and the tokens of a cancelled job are not counted. Sending the same thread again starts a clean run.
- Token use: the manager's own agent runs through the same compression middleware as the workers. Compression only touches old turns and JSON tool results; the system prompt and the tool schemas (most of what a demand sends) are resent on every call, and the offline demo prints their share next to the compression figures. For Anthropic models the system prompt is marked as a cache breakpoint, which also covers the tools (`PROMPT_CACHE=off` disables it; checked against a stub of the API, not the real one). The request dossier goes only in the first prompt of a thread; later prompts on it point at the first message. `ANTHROPIC_BASE_URL` sends the Anthropic calls through a gateway, and `ANTHROPIC_AUTH_TOKEN` is sent as `Authorization: Bearer`, alone or next to `ANTHROPIC_API_KEY`.

## Test it

```bash
go test -race -count=1 ./examples/langgraph/full_team/...   # includes TestStartLocalWithMiddleware and TestOfflineRunReportsPonytailAndTOON (offline run: at least one Ponytail note and one TOON rewrite with bytes saved)
```

## Test it in production mode

1. Set `LAYA_URL` (or `LAYA_BUNDLE_DIR` for native) and check that the report shows rewritten calls and tokens dropping on long threads. Short threads may show no savings, or a small increase from the reply directive and the Ponytail note.
2. Check the Ponytail count against the code share: the laya decider decides per call, and a call with the note is only worth it if the answer contains less code. Compare output tokens with and without `ponytail` on the same request before you keep it on.
3. Watch the first call's latency: the guard's 2s timeout falls back to the rules, and the report does not separate those answers from laya's. Compare with a run where `LAYA_URL` is unset.
4. Check `rtk on PATH: true` on the host that runs the workers. Without it, shell commands pass through unchanged.
5. Keep `AllowedCommands` tight: `LocalShell` runs on the host, without a sandbox.

## See also

- [independent_engineering_team](../independent_engineering_team/README.md): the source of the copied team, without compression
- [compress_history_summary](../compress_history_summary/README.md), [compress_shell_rtk](../compress_shell_rtk/README.md), [decider_laya_http](../decider_laya_http/README.md), [decider_laya_native](../decider_laya_native/README.md)
