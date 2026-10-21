# Using the router with several gateways

**English** | [Português](../pt-BR/gateway-router.md) | [Español](../es-ES/gateway-router.md)

`router:<model>` sends each model call to an ordered list of targets and moves to the next one when a target fails with a retryable error. This manual shows how to wire freellmapi, omniroute, bifrost, Anthropic (Claude) and OpenAI into one `.env`, how to run each mode, and how to use it in the engineering-team examples. The variable reference is not repeated here: see the [Router section of PROVIDERS.md](../../en-US/PROVIDERS.md#router) and [manual chapter 3.6.1](../../en-US/manual/03-modelos-e-provedores.md).

Scope: the router is a Go library used through `chatmodels.Init("router:<model>")`. It is not a server, so VS Code, Cursor or a console cannot be pointed at it; those clients are pointed at a gateway (see [clients-mcp.md](clients-mcp.md#pointing-a-client-at-a-gateway)).

## The gateways

Each gateway is an OpenAI-compatible HTTP endpoint, so the router treats it as a target named after it and reads `<NAME>_BASE_URL` (and optionally `<NAME>_API_KEY`).

| Gateway | Base URL for the router | Key | Source |
|---|---|---|---|
| freellmapi | `http://localhost:3001/v1` (default port 3001) | the unified key shown in its dashboard, starts with `freellmapi-`; model `auto` lets it choose | https://github.com/tashfeenahmed/freellmapi |
| omniroute | `http://localhost:20128/v1` (default port 20128) | the key from its Dashboard, Endpoints; model `auto` or `auto/<variant>` or `provider/model` | https://github.com/diegosouzapw/OmniRoute |
| bifrost | `http://localhost:8080/v1` (default port 8080; `/v1/chat/completions` is the documented path) | check bifrost docs for virtual keys (the quickstart sends no key); model as `provider/model` such as `openai/gpt-4o-mini` | https://docs.getbifrost.ai/quickstart/gateway/setting-up |
| Anthropic (native) | built in | `ANTHROPIC_API_KEY` | - |
| OpenAI (native) | built in | `OPENAI_API_KEY` | - |

Ports are the defaults those projects document; if you changed them, use yours. The router appends the OpenAI paths to the base URL, so keep the `/v1`.

Model names differ per gateway (`auto` on freellmapi, `provider/model` on bifrost). That is what `ROUTER_MODEL_<NAME>` is for.

## One `.env` with the five targets

```bash
# .env
ROUTER_TARGETS=freellmapi,omniroute,bifrost,anthropic,openai
ROUTER_MODE=failover
ROUTER_TIMEOUT=60s

FREELLMAPI_BASE_URL=http://localhost:3001/v1
FREELLMAPI_API_KEY=freellmapi-...
ROUTER_MODEL_FREELLMAPI=auto

OMNIROUTE_BASE_URL=http://localhost:20128/v1
OMNIROUTE_API_KEY=...
ROUTER_MODEL_OMNIROUTE=auto

BIFROST_BASE_URL=http://localhost:8080/v1
# BIFROST_API_KEY=            # only if your bifrost requires one: check bifrost docs
ROUTER_MODEL_BIFROST=openai/gpt-4o-mini

ANTHROPIC_API_KEY=sk-ant-...
ROUTER_MODEL_ANTHROPIC=claude-sonnet-4-5
OPENAI_API_KEY=sk-...
ROUTER_MODEL_OPENAI=gpt-4o-mini
```

```go
m, err := chatmodels.Init("router:gpt-4o-mini") // the model after "router:" is used by every target without a ROUTER_MODEL_<NAME>
```

`Init` fails at once, before any call, when a target is incomplete: `router target "bifrost" needs $BIFROST_BASE_URL`, `router target "anthropic" needs $ANTHROPIC_API_KEY`, or `router target "x" has no model; set ROUTER_MODEL_X or the model after router:`.

## One example per mode

`ROUTER_MODE` only decides which target is tried first on each call; the others follow in `ROUTER_TARGETS` order if it fails.

```bash
# failover (default): always start with freellmapi, fall back down the list
ROUTER_MODE=failover

# round_robin: rotate the first target on every call (the counter lives in one Init call)
ROUTER_MODE=round_robin

# random: a random first target on every call
ROUTER_MODE=random

# weighted: 70% of calls start with freellmapi, 30% with omniroute, bifrost never first
ROUTER_MODE=weighted
ROUTER_WEIGHTS=freellmapi:7,omniroute:3,bifrost:0,anthropic:0,openai:0
```

A target missing from `ROUTER_WEIGHTS` weighs 1; weight 0 means never first (it still receives the call when the earlier ones fail).

## Per-role models in the engineering team

Two examples, two different ways the environment reaches the code.

**`examples/langgraph/independent_engineering_team`** (hub, manager and one container per worker). Each role reads `AGENT_MODEL_<ROLE>`, then `AGENT_MODEL`, then the key-based defaults, and passes the value to `chatmodels.Init`, so `router:` works. Roles: `MANAGER PO ARCHITECT LEADER JUNIOR MID SENIOR SPECIALIST QA KNOWLEDGE`. The example's `.env.example` is copied to `.env`, and every service in `docker-compose.yml` has `env_file: [{ path: .env, required: false }]`, so every container sees the same file, including the `ROUTER_*`, `*_BASE_URL` and key variables.

```bash
cp examples/langgraph/independent_engineering_team/.env.example examples/langgraph/independent_engineering_team/.env
# append to that .env: the block from the previous section, plus
AGENT_MODEL=router:gpt-4o-mini
AGENT_MODEL_PO=router:gpt-4o
AGENT_MODEL_ARCHITECT=router:gpt-4o
AGENT_MODEL_JUNIOR=router:gpt-4o-mini
AGENT_MODEL_QA=router:gpt-4o-mini
```

```bash
docker compose -f examples/langgraph/independent_engineering_team/docker-compose.yml up --build
```

Things to know before using it:

- A gateway on the host is not `localhost` inside a container. Use `http://host.docker.internal:PORT/v1` (Docker Desktop; on Linux add `extra_hosts: ["host.docker.internal:host-gateway"]` to the services) or run the gateway as another compose service. Check your Docker setup; this is not something the example configures.
- The role model string goes through the example's `normalize`, which turns `_` into `-` in the model and the first `/` into `:`. `router:gpt-4o-mini` is safe. A model such as `router:openai/gpt-4o-mini` would become `router:openai:gpt-4o-mini`, so put names with `/` or `_` in `ROUTER_MODEL_<NAME>` instead, which is not normalized.
- All roles share one set of `ROUTER_*` variables, so they share targets and mode. To give a role a different set of targets you would need separate processes with separate environments; the example does not do that.
- `AGENT_MODEL_<ROLE>=router:...` counts as set, so it replaces the offline scripted model; no `ANTHROPIC_API_KEY` is needed if the router's targets are all gateways.

**`examples/langgraph/engineering_team`** (one process). It does not read `AGENT_MODEL_*`: it uses `examples/internal/demo`, which picks `anthropic:claude-sonnet-4-5` when `ANTHROPIC_API_KEY` is set, `openai:gpt-4o-mini` when `OPENAI_API_KEY` is set, otherwise the scripted model. One model serves the whole team and there is no per-role mapping, and the router is not wired in. Use `independent_engineering_team` for the router. (If you want the router here, `demo.Smart` would have to be changed; that is a code change, not configuration.)

The `knowledge` CLI and hub also accept the router: `KNOWLEDGE_MODEL=router:gpt-4o-mini` (read by `cmd/knowledge`), `AGENT_MODEL_KNOWLEDGE` in the independent example.

## Which target answered

Nothing in the code reports it on success. The router does not log, does not set a field on the answer and `ModelID()` always returns `router:<model>`. What you can observe:

- When every target fails, the error lists each one: `router: all targets failed: freellmapi: <error>; omniroute: <error>; ...`.
- The gateways' own logs and dashboards (freellmapi, omniroute and bifrost each have a dashboard or log of requests; check their docs for how) show which requests reached them.
- With `ROUTER_MODE=failover`, a call that reaches the second target means the first failed, but the router does not say so. If you need a per-call record, the way to get it today is a gateway log or a timeout/error test against a fake gateway.

## What happens to the context on a switch

- **Between calls and on failover.** Every target receives the same complete message list and options, so conversation history is never lost on a switch: the next target is simply sent the full history again. Tool definitions travel the same way.
- **Streaming, failure before any text.** The next target gets the original messages; the caller sees one normal stream.
- **Streaming, failure after some text (no tool call).** The next target receives the original messages, the text already emitted as an assistant message, and a user message asking it to continue. The caller sees one uninterrupted answer. How seamless it is depends on the model; it can repeat or rephrase.
- **Streaming, failure after a partial tool call.** It cannot be continued: the error is returned to the caller.
- **Idle timeout.** In `Stream`, `ROUTER_TIMEOUT` is an idle timeout (time to the first chunk, then the gap between chunks), so a long answer that keeps flowing is not cut. In `Generate` it limits the whole call.
- Different targets use different models, so tone, tool-calling quality and context size can differ after a switch. Keep the models in the same class and check that every one supports tool calling if your agents use tools.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `401` or `403` returned at once, no failover | Those are final by design: the next target would not fix a bad key. Fix the key of the target the error names. 400, 404 and other 4xx are final too. |
| `router: all targets failed: a: ...; b: ...` | Each target's error is listed in order. Typical: connection refused (gateway not running or wrong port), 429, 5xx. |
| `router target "bifrost" needs $BIFROST_BASE_URL` at `Init` | A target name without a native provider needs `<NAME>_BASE_URL`; the name is upper-cased and non-alphanumerics become `_` (`my-gw` -> `MY_GW_BASE_URL`). |
| `set ROUTER_TARGETS to an ordered, comma-separated list` | `ROUTER_TARGETS` is empty or not exported to this process (in Docker, check `env_file`). |
| `ROUTER_WEIGHTS: "x" is not in ROUTER_TARGETS` | Weights must name existing targets. |
| Works on the host, fails in the container | `localhost` inside the container is the container. See the note on `host.docker.internal` above. |
| A gateway rejects the model name | Set `ROUTER_MODEL_<NAME>` for that target (see the gateway table). |
| A very slow target holds up every call | Set `ROUTER_TIMEOUT` (for example `30s`); a timeout is retryable, so the next target is tried. |
