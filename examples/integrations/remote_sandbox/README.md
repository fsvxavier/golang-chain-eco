# remote_sandbox

Any "run a command somewhere" function becomes a full deep-agent backend (files and shell) through `backends.Remote`.

## What it does

- `backends.Remote{Run: runLocal, Root: dir}` implements write, read, edit, grep, ls and execute on top of a single command runner.
- Here the runner is bash in a temp directory so the example works anywhere. **It executes commands on the host, so do not point an untrusted agent at it.**

## How to apply it

```go
b := backends.Remote{Run: runner.Run, Root: "/workspace"}
agent, _ := deepagents.Create(deepagents.Options{Model: model, Backend: b})
```

In production, `runner.Run` is the isolated runner: `kubernetes.Sandbox.Run` (a pod), a container runtime or a remote VM. The agent gets file tools and an `execute` tool without any of them touching your host.

## Run it

```bash
go run ./examples/integrations/remote_sandbox
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Replace `runLocal` with `kubernetes.Sandbox.Run` (see the Kubernetes integration) or another isolated runner: no host mounts, no credentials, network off or allow-listed, CPU/memory/time limits.
2. Create one sandbox per thread and never reuse it between users; destroy it when the thread ends.
3. Run an adversarial prompt ("read ~/.ssh", "curl the metadata service") and verify it fails inside the sandbox.
4. Keep `LocalShell`-style access out of production entirely.

## See also

- [INTEGRATIONS.md](../../../docs/en-US/INTEGRATIONS.md)
- [Manual, chapter 14: Production, security and troubleshooting](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
