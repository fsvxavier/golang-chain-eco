# decider_laya_http

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Asks a laya-serve endpoint (`POST /v1/systemone`, the same protocol as TypeSafe Jev) for the history and shell answers, through `compress.HTTPDecider` wrapped in `compress.NewGuarded`.

## What it does

- Builds a 3-message conversation and asks the three history questions (`compress_history`, `level`, `compress_reply`) and the shell question (`route_rtk`).
- Without `LAYA_URL` it starts an in-process fake `POST /v1/systemone` server (`httptest`) and points the decider at it. Nothing leaves the machine and no model is needed.
- With `LAYA_URL` it calls the real endpoint instead.
- `NewGuarded` keeps the run safe: an error, a timeout, a missing id or a low-confidence answer sends that question to `RuleDecider`. Point `LAYA_URL` at a dead port to see the fallback.

## How to apply it

```go
d := compress.HTTPDecider{
    URL:    "http://localhost:8000/v1/systemone", // full endpoint
    APIKey: os.Getenv("LAYA_API_KEY"),            // optional, sent as Bearer
    Model:  "multilingual",                       // optional, per request
}
g := compress.NewGuarded(d) // Floor 0.7, Timeout 2s, Fallback RuleDecider{}
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

## Run it

Offline (fake server):

```bash
go run ./examples/langgraph/decider_laya_http
```

Against a real laya-serve:

```bash
pip install "laya[serve]"
LAYA_MODELS=multilingual LAYA_DEVICE=cpu laya-serve   # default listen address 0.0.0.0:8000
```

`laya-serve` listens on `0.0.0.0:8000` by default. Outside a trusted machine set `LAYA_HOST=127.0.0.1` and `LAYA_API_KEY=...`. `LAYA_MODELS` preloads the model; the request names the model in its `model` field, which this example sends from `LAYA_MODEL`. The form `laya serve --model ...` appears in some notes but is not in the public docs, so this example does not use it.

Then, in another shell:

```bash
LAYA_URL=http://localhost:8000/v1/systemone LAYA_MODEL=multilingual \
  go run ./examples/langgraph/decider_laya_http
# add LAYA_API_KEY=... when the server requires it
```

`LAYA_URL` is the full endpoint, not a base URL.

## Test it in production mode

1. Run against the real server with the model you deploy, and check that each answer prints as `choice` or `noul`, not as a fallback.
2. Stop the server and rerun: every answer must fall back to rules and the run must still exit 0.
3. Put a `Timeout` below the server's latency budget on the `Guarded` value, and keep `Floor` at 0.7 unless you measured a better one.
4. Serve `laya-serve` on a private network with `LAYA_API_KEY` set. The example sends the key as `Authorization: Bearer`.

## See also

- [Compression middleware: `agents/middleware/compression.go`](../../../agents/middleware/compression.go)
- [decider_fallback](../decider_fallback/README.md): the rule decider and the guard on their own
- [decider_laya_native](../decider_laya_native/README.md): the same questions answered in-process with laya-go
