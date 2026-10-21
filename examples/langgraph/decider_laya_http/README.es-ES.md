# decider_laya_http

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Pregunta a un endpoint de laya-serve (`POST /v1/systemone`, el mismo protocolo que TypeSafe Jev) las respuestas del historial y del shell, mediante `compress.HTTPDecider` envuelto en `compress.NewGuarded`.

## Qué hace

- Arma una conversación de 3 mensajes y hace las cinco preguntas de historial (`compress_history`, `level`, `compress_reply`, `ponytail`, `toon_tool_results`) y la pregunta de shell (`route_rtk`).
- Sin `LAYA_URL`, levanta dentro del mismo proceso un servidor falso `POST /v1/systemone` (`httptest`) y apunta el decider a él. Nada sale de la máquina y no hace falta ningún modelo.
- Con `LAYA_URL`, llama al endpoint real.
- `NewGuarded` mantiene la ejecución segura: un error, un timeout, un id ausente o una respuesta con baja confianza envían esa pregunta a `RuleDecider`. Apunte `LAYA_URL` a un puerto sin servicio para ver el fallback.

## Cómo aplicarlo

```go
d := compress.HTTPDecider{
    URL:    "http://localhost:8000/v1/systemone", // endpoint completo
    APIKey: os.Getenv("LAYA_API_KEY"),            // opcional, se envía como Bearer
    Model:  "multilingual",                       // opcional, por solicitud
}
g := compress.NewGuarded(d) // Floor 0.7, Timeout 2s, Fallback RuleDecider{}
answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
```

## Cómo ejecutarlo

Sin conexión (servidor falso):

```bash
go run ./examples/langgraph/decider_laya_http
```

Con un laya-serve real:

```bash
pip install "laya[serve]"
LAYA_MODELS=multilingual LAYA_DEVICE=cpu laya-serve   # dirección por defecto 0.0.0.0:8000
```

`laya-serve` escucha en `0.0.0.0:8000` por defecto. Fuera de una máquina de confianza, defina `LAYA_HOST=127.0.0.1` y `LAYA_API_KEY=...`. `LAYA_MODELS` precarga el modelo; la solicitud indica el modelo en su campo `model`, que este ejemplo envía desde `LAYA_MODEL`. La forma `laya serve --model ...` aparece en algunas notas, pero no está en la documentación pública, así que este ejemplo no la usa.

Luego, en otra terminal:

```bash
LAYA_URL=http://localhost:8000/v1/systemone LAYA_MODEL=multilingual \
  go run ./examples/langgraph/decider_laya_http
# añada LAYA_API_KEY=... cuando el servidor lo exija
```

`LAYA_URL` es el endpoint completo, no una URL base.

## Pruébalo en modo de producción

1. Ejecútalo contra el servidor real con el modelo que despliegas y comprueba que cada respuesta aparece como `choice` o `noul`, y no como fallback.
2. Detén el servidor y vuelve a ejecutar: todas las respuestas deben caer en las reglas y la ejecución debe terminar con código 0.
3. Pon un `Timeout` por debajo del presupuesto de latencia del servidor en el valor `Guarded`, y mantén `Floor` en 0,7 salvo que hayas medido un valor mejor.
4. Expón `laya-serve` solo en una red privada, con `LAYA_API_KEY` definido. El ejemplo envía la clave como `Authorization: Bearer`.

## Véase también

- [Middleware de compresión: `agents/middleware/compression.go`](../../../agents/middleware/compression.go)
- [decider_fallback](../decider_fallback/README.es-ES.md): el decider por reglas y el guard por separado
- [decider_laya_native](../decider_laya_native/README.es-ES.md): las mismas preguntas respondidas en el proceso con laya-go
