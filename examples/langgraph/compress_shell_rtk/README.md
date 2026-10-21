# compress_shell_rtk

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Routes a shell command through the `rtk` output filter when a decider says so, and runs it with `backends.LocalShell`.

## What it does

- `compress.NewRTK(decider)` builds the router. The scripted decider answers yes to `route_rtk`.
- `LocalShell.Rewrite` is wired to `rtk.Rewrite`. The allowlist (`echo`, `go`) checks the command the caller asked for, then the rewrite runs, so the executed command is printed.
- `rtk.Executor(shell)` records each run's output size; the next decision gets it as `prev_output_bytes`.
- A refused command (`curl`) is rejected by `AllowedCommands` before any rewrite.
- If `rtk` is not on `PATH`, the command runs unchanged and the example says so.

## How to apply it

```go
rtk := compress.NewRTK(decider)               // decider: your Decider, guarded by rules
shell := backends.LocalShell{
    Filesystem:      backends.Filesystem{Root: dir},
    AllowedCommands: []string{"echo", "go"},
    Rewrite:         rtk.Rewrite,
    Quiet:           true,
}
res, err := rtk.Executor(shell).Execute(ctx, "go version", 30*time.Second)
```

Only simple commands are rewritten: pipes, `&&`, `;`, redirections, `$(...)`, backticks and leading `VAR=` are passed through unchanged.

## Run it

```bash
go run ./examples/langgraph/compress_shell_rtk
```

The example runs offline. It needs `go` on `PATH`; `rtk` is optional.

## Test it in production mode

1. Install `rtk` and confirm `rtk --version` works, so the rewrite path is exercised.
2. Replace the scripted decider with `compress.HTTPDecider` or a real model. `NewRTK` already guards it with rules, so a slow or wrong answer falls back.
3. Keep `AllowedCommands` tight. `LocalShell` runs on the host; use a sandboxed runner for untrusted input.
4. Check that rewritten commands keep the exit code you expect, since `rtk` filters output only.

## See also

- [Compression package](../../../compress/)
- [Example: compress_prompt_caveman](../compress_prompt_caveman/README.md)
- [Manual, chapter 5: Tools](../../../docs/en-US/manual/05-tools.md)
