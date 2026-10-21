# decider_laya_native

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Responde las preguntas del historial y del shell con un modelo Laya dentro del propio proceso, mediante `laya-go` y `compress.Decider`. Sin soporte nativo, recurre a las reglas, así que el ejemplo siempre se ejecuta.

Es un módulo Go propio (`replace github.com/fsvxavier/golang-chain-eco => ../../..`). Su `go.mod` requiere `github.com/metalagman/laya-go`; el `go.mod` de la raíz también lo requiere, para `full_team/native` (tras la tag `laya_native`).

## Qué hace

- `native.go` (etiqueta de build `laya_native`) implementa `compress.Decider` con `laya-go`: `NewRuntime`, `OpenModelDir` (con `TruncateOverflow`, para que los historiales largos se acorten en lugar de fallar con `ErrInputTooLong`) y una llamada `Predict` por lote. Define `ORT_DISABLE_TELEMETRY=1`.
- Las preguntas `noul` se convierten en `NewNoulQuestion`; las preguntas `choice` en `NewChoiceQuestion`, con un criterio por opción.
- `stub.go` (etiqueta de build `!laya_native`) devuelve un error, así que `main` muestra el motivo y usa `compress.RuleDecider`.
- `main.go` muestra cada respuesta con su origen: `laya` o `rules`.

## Requisitos

- Linux en amd64 (`linux/amd64`), solo modelos FP32.
- `laya-go` v0.1.0, versión pre-v1: la API puede cambiar antes de la v1.
- Para el build nativo: cgo, la biblioteca compartida de ONNX Runtime y la biblioteca de tokenizers, ambas aportadas por quien construye y fijadas en versión, y un bundle Laya completo y local.

Variables de entorno para el build nativo:

| Variable | Significado |
|---|---|
| `LAYA_ONNXRUNTIME_LIBRARY` | ruta de la biblioteca compartida de ONNX Runtime |
| `LAYA_TOKENIZERS_LIBRARY` | ruta de la biblioteca de tokenizers |
| `LAYA_BUNDLE_DIR` | directorio de un bundle Laya completo (lo lee `main`; sin él, usa reglas) |

## Cómo aplicarlo

```go
rt, _ := laya.NewRuntime(ctx, laya.RuntimeOptions{})
m, _ := rt.OpenModelDir(ctx, dir, laya.ModelOptions{QueueCapacity: 8, Truncation: laya.TruncateOverflow})
g := compress.NewGuarded(nativeDecider{model: m}) // cae en las reglas ante un error
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

## Cómo ejecutarlo

Ruta stub (sin bibliotecas nativas, sin cgo):

```bash
cd examples/langgraph/decider_laya_native
go run .
```

Ruta nativa:

```bash
cd examples/langgraph/decider_laya_native
export LAYA_ONNXRUNTIME_LIBRARY=/ruta/a/libonnxruntime.so
export LAYA_TOKENIZERS_LIBRARY=/ruta/a/libtokenizers
export LAYA_BUNDLE_DIR=/ruta/a/bundle
CGO_ENABLED=1 go run -tags laya_native .
```

Sin `LAYA_BUNDLE_DIR`, el build nativo también usa las reglas y lo indica.

## Pruébalo en modo de producción

1. Compila con `-tags laya_native` en el CI en linux/amd64, con las bibliotecas fijadas, y comprueba que la salida muestra `laya` en las preguntas.
2. Mide la latencia del primer `Predict` tras el arranque; mantén el `Timeout` (2s por defecto en `compress.NewGuarded`) por encima de ella, o la ejecución caerá en las reglas.
3. Cierra el modelo antes que el runtime (el ejemplo lo hace en el closer). Mantén inmutable el directorio del bundle mientras el modelo esté abierto.
4. Fija `laya-go` en una versión exacta y revisa el changelog antes de cada actualización, ya que la API es pre-v1.

## Véase también

- [decider_laya_http](../decider_laya_http/README.es-ES.md): las mismas preguntas contra un endpoint de laya-serve
- [decider_fallback](../decider_fallback/README.es-ES.md): el decider por reglas y el guard por separado
