# decider_laya_native

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Answers the history and shell questions with an in-process Laya model, through `laya-go` and `compress.Decider`. Without native support it falls back to the rules, so the example always runs.

This is its own Go module (`replace github.com/fsvxavier/golang-chain-eco => ../../..`). Its `go.mod` requires `github.com/metalagman/laya-go`; the root `go.mod` also requires it, for `full_team/native` (behind the `laya_native` tag).

## What it does

- `native.go` (build tag `laya_native`) implements `compress.Decider` with `laya-go`: `NewRuntime`, `OpenModelDir` (with `TruncateOverflow`, so long histories are shortened instead of failing with `ErrInputTooLong`) and one `Predict` call per batch. It sets `ORT_DISABLE_TELEMETRY=1`.
- `compress.Noul` questions map to `NewNoulQuestion`; `choice` questions map to `NewChoiceQuestion`, one criterion per option.
- `stub.go` (build tag `!laya_native`) returns an error, so `main` prints why and uses `compress.RuleDecider`.
- `main.go` prints each answer with its source, `laya` or `rules`.

## Requirements

- Linux on amd64 (`linux/amd64`), FP32 models only.
- `laya-go` v0.2.6, a pre-v1 release: its API may change before v1.
- For the native build: cgo, the ONNX Runtime shared library and the tokenizers library, both caller-supplied and pinned, and a complete local Laya bundle.

Environment variables for the native build:

| Variable | Meaning |
|---|---|
| `LAYA_ONNXRUNTIME_LIBRARY` | path to the ONNX Runtime shared library |
| `LAYA_TOKENIZERS_LIBRARY` | path to the tokenizers library |
| `LAYA_BUNDLE_DIR` | directory of a complete Laya bundle (read by `main`; unset means rules) |

## How to apply it

```go
rt, _ := laya.NewRuntime(ctx, laya.RuntimeOptions{})
m, _ := rt.OpenModelDir(ctx, dir, laya.ModelOptions{QueueCapacity: 8, Truncation: laya.TruncateOverflow})
g := compress.NewGuarded(nativeDecider{model: m}) // falls back to rules on error
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

## Run it

Stub path (no native libraries, no cgo):

```bash
cd examples/langgraph/decider_laya_native
go run .
```

Native path:

```bash
cd examples/langgraph/decider_laya_native
export LAYA_ONNXRUNTIME_LIBRARY=/path/to/libonnxruntime.so
export LAYA_TOKENIZERS_LIBRARY=/path/to/libtokenizers
export LAYA_BUNDLE_DIR=/path/to/bundle
CGO_ENABLED=1 go run -tags laya_native .
```

Without `LAYA_BUNDLE_DIR` the native build also uses the rules and says so.

## Test it in production mode

1. Build with `-tags laya_native` in CI on linux/amd64 with the pinned libraries, and check that the output shows `laya` for the questions.
2. Measure the first `Predict` latency after startup; keep `Timeout` (default 2s in `compress.NewGuarded`) above it, or the run falls back to rules.
3. Close the model before the runtime (the example does this in its closer). Keep the bundle directory immutable while the model is open.
4. Pin `laya-go` to an exact version and review its changelog before each upgrade, since the API is pre-v1.

## See also

- [decider_laya_http](../decider_laya_http/README.md): the same questions against a laya-serve endpoint
- [decider_fallback](../decider_fallback/README.md): the rule decider and the guard on their own
