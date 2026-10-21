# full_team

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

The engineering team flow (hub, manager, workers over Agent Protocol and MCP, team memory, leases) with the `compress` layer on every worker: each agent's model calls are compressed, and its shell tool routes simple commands through `rtk`. Offline it runs with a scripted decider, so the savings show without a model.

This folder is self-contained. It does not import `examples/langgraph/independent_engineering_team`; the code was copied from it.

## What it does

- **Copied from `independent_engineering_team`**: `internal/team` (hub, manager, workers, knowledge, team memory, tests), `skills/` (agent personas) and `kb/` (standards and checklists). The copy adds only the hooks below, so the original stays unchanged.
- **Hooks added to the team**: `WorkerConfig.Middleware` and `WorkerConfig.Tools` (empty means unchanged), `LocalOptions`, and `StartLocalWith`, which gives middleware and tools to every worker. `StartLocal` is `StartLocalWith` with nothing added. `TestStartLocalWithMiddleware` checks that worker middleware runs.
- **Model calls**: every worker's agent gets `middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}`. Old Human and AI turns are caveman-compressed per call; tool calls and results are never touched. Stored history is not rewritten.
- **Shell**: each worker gets a `shell` tool (`{command}`) over `backends.LocalShell` with `AllowedCommands` (`go git ls grep echo`) and `Rewrite: rtk.Rewrite`. `RTK.Env` is the shell's `Env`, so `rtk` is looked up on the same PATH the shell uses. `rtk.Executor` records output sizes for the routing decision.
- **Decider**: scripted offline (answers every question as confident, so compression applies). With `LAYA_URL` set, `compress.HTTPDecider` asks a laya-serve endpoint instead.
- **Report**: after the run, it prints how many model calls were rewritten, the estimated tokens before and after (bytes/4), and whether `rtk` is on PATH.

`native/` is the same program with the decider answered by an in-process laya model. It is a package of the root module: `native.go` has the build tag `laya_native` and imports `laya-go`; `stub.go` has `!laya_native` and is used by default.

## How to apply it

- Middleware: `Keep` is how many recent turns stay as they are (default 4; this demo uses 1 because its threads are short).
- Shell: `AllowedCommands` in `shellTool`. Never list shells, interpreters, `env`, `xargs` or `find`. Note that `go` and `git` can run code, so the host needs a sandbox for anything untrusted.
- Decider: `decider()` in `main.go`. Wrap any decider in `compress.NewGuarded` so a slow or wrong one falls back to the rules.

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
go test -race -count=1 ./examples/langgraph/full_team/...   # includes TestStartLocalWithMiddleware
```

## Test it in production mode

1. Set `LAYA_URL` (or `LAYA_BUNDLE_DIR` for native) and check that the report shows rewritten calls and tokens dropping on long threads. Short threads may show no savings, or a small increase from the reply directive.
2. Watch the first call's latency: the guard's 2s timeout falls back to the rules, and the report does not separate those answers from laya's. Compare with a run where `LAYA_URL` is unset.
3. Check `rtk on PATH: true` on the host that runs the workers. Without it, shell commands pass through unchanged.
4. Keep `AllowedCommands` tight: `LocalShell` runs on the host, without a sandbox.

## See also

- [independent_engineering_team](../independent_engineering_team/README.md): the source of the copied team, without compression
- [compress_history_summary](../compress_history_summary/README.md), [compress_shell_rtk](../compress_shell_rtk/README.md), [decider_laya_http](../decider_laya_http/README.md), [decider_laya_native](../decider_laya_native/README.md)
