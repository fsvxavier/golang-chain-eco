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
- **Shell**: each worker gets a `shell` tool (`{command}`) over `backends.LocalShell` with `AllowedCommands` (`go git ls grep echo` offline; `ls grep echo` when `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` is set, see `allowedPrograms`) and `Rewrite: rtk.Rewrite`. `RTK.Env` is the shell's `Env`, so `rtk` is looked up on the same PATH the shell uses. `rtk.Executor` records output sizes for the routing decision.
- **Decider**: scripted offline (answers every question as confident, so compression and TOON apply). `ponytail` is answered by the rule (`compress.RuleDecider`), so a note appears only where the conversation has a real share of code blocks. With `LAYA_URL` set, `compress.HTTPDecider` asks a laya-serve endpoint instead.
- **Report**: after the run, it prints how many model calls were rewritten, the estimated tokens before and after (bytes/4), how many calls carried the Ponytail note, how many tool results were rewritten to TOON with the bytes they saved, and whether `rtk` is on PATH. The counts come from a wrapper around the middleware (`meter`), not from changes to the library. One offline run showed 46 model calls, 31 per-call TOON rewrites saving 11625 bytes, and 46114 estimated tokens before and 42964 after. The printed numbers vary a little between runs (goroutine timing). The offline run may show no Ponytail notes: the rule adds one only when the conversation has a real share of code, and the scripted run has little. A Ponytail note adds about 0.8 KB to a call; it pays off only when the model's output shrinks, which the offline scripted run does not measure.

`native/` is the same program with the decider answered by an in-process laya model. It is a package of the root module: `native.go` has the build tag `laya_native` and imports `laya-go`; `stub.go` has `!laya_native` and is used by default. Its scripted fallback has no `open_tasks` tool, so its report shows no TOON rewrites offline. That fallback answers `ponytail` no, and its meter does not count Ponytail notes.

## How to apply it

- Middleware: `Keep` is how many recent turns stay as they are (default 4; this demo uses 1 because its threads are short).
- Shell: `AllowedCommands` in `shellTool`. Never list shells, interpreters, `env`, `xargs` or `find`. Note that `go` and `git` can run code, so the host needs a sandbox for anything untrusted. They are offered only in the offline scripted run: a live model with a provider key gets `ls`, `grep` and `echo` only.
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
