# full_team

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El flujo del equipo de ingeniería (hub, gerente, workers por Agent Protocol y MCP, memoria del equipo, leases) con la capa `compress` en cada worker: las llamadas al modelo de cada agente se comprimen, y su herramienta de shell envía los comandos simples por `rtk`. Sin conexión funciona con un decisor guionizado, así que el ahorro se ve sin modelo.

Esta carpeta es autocontenida. No importa `examples/langgraph/independent_engineering_team`; el código se copió de allí.

## Qué hace

- **Copiado de `independent_engineering_team`**: `internal/team` (hub, gerente, workers, conocimiento, memoria del equipo, pruebas), `skills/` (personas de los agentes) y `kb/` (estándares y checklists). La copia solo añade los ganchos de abajo, así que el original queda intacto.
- **Ganchos añadidos al equipo**: `WorkerConfig.Middleware` y `WorkerConfig.Tools` (vacío significa sin cambios), `LocalOptions` y `StartLocalWith`, que entrega middleware y herramientas a cada worker. `StartLocal` es `StartLocalWith` sin nada adicional. `TestStartLocalWithMiddleware` comprueba que el middleware del worker se ejecuta.
- **Llamadas al modelo**: el agente de cada worker recibe `middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}`. Los turnos antiguos (Human y AI) se comprimen con caveman en cada llamada; las llamadas y resultados de herramientas nunca se tocan. El historial almacenado no se reescribe.
- **Shell**: cada worker recibe la herramienta `shell` (`{command}`) sobre `backends.LocalShell`, con `AllowedCommands` (`go git ls grep echo` sin conexión; `ls grep echo` cuando `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` está definida, véase `allowedPrograms`) y `Rewrite: rtk.Rewrite`. `RTK.Env` es el `Env` del shell, así `rtk` se busca en el mismo PATH que usa el shell. `rtk.Executor` registra el tamaño de la salida para la decisión de enrutado.
- **Decisor**: guionizado sin conexión (responde con confianza a todo, así la compresión se aplica). Con `LAYA_URL` definido, `compress.HTTPDecider` consulta un endpoint de laya-serve.
- **Informe**: al terminar imprime cuántas llamadas al modelo fueron reescritas, los tokens estimados antes y después (bytes/4) y si `rtk` está en el PATH.

`native/` es el mismo programa con el decisor respondido por un modelo laya en proceso. Es un paquete del módulo raíz: `native.go` tiene la build tag `laya_native` e importa `laya-go`; `stub.go` tiene `!laya_native` y se usa por defecto.

## Cómo aplicarlo

- Middleware: `Keep` es cuántos turnos recientes se dejan intactos (por defecto 4; esta demo usa 1 porque los hilos son cortos).
- Shell: `AllowedCommands` en `shellTool`. Nunca listes shells, intérpretes, `env`, `xargs` ni `find`. `go` y `git` pueden ejecutar código, así que el host necesita sandbox para cualquier entrada no confiable. Solo se ofrecen en la ejecución offline con guion: un modelo real con clave de proveedor recibe solo `ls`, `grep` y `echo`.
- Decisor: `decider()` en `main.go`. Envuelve cualquier decisor con `compress.NewGuarded` para que uno lento o incorrecto caiga en las reglas.

## Cómo ejecutarlo

Sin conexión, un proceso, HTTP real entre servicios:

```bash
go run ./examples/langgraph/full_team
```

Contra un endpoint de laya-serve:

```bash
LAYA_URL=http://localhost:8099/v1/systemone LAYA_API_KEY=... go run ./examples/langgraph/full_team
```

Nativo (modelo laya en proceso):

```bash
export LAYA_ONNXRUNTIME_LIBRARY=/ruta/a/libonnxruntime.so
export LAYA_TOKENIZERS_LIBRARY=/ruta/a/libtokenizers
export LAYA_BUNDLE_DIR=/ruta/al/bundle
CGO_ENABLED=1 go run -tags laya_native ./examples/langgraph/full_team/native
```

Sin `-tags laya_native`, o sin `LAYA_BUNDLE_DIR`, el programa nativo usa el decisor guionizado y dice por qué. La build nativa necesita linux/amd64 y modelos FP32, como en `decider_laya_native`.

## Cómo probarlo

```bash
go test -race -count=1 ./examples/langgraph/full_team/...   # incluye TestStartLocalWithMiddleware
```

## Cómo probarlo en modo producción

1. Define `LAYA_URL` (o `LAYA_BUNDLE_DIR`, en nativo) y comprueba que el informe muestra llamadas reescritas y tokens bajando en hilos largos. Los hilos cortos pueden no ahorrar, o subir un poco por la directiva de respuesta.
2. Observa la latencia de la primera llamada: el timeout de 2s del guard cae en las reglas, y el informe no separa esas respuestas de las de laya. Compara con una ejecución sin `LAYA_URL`.
3. Confirma `rtk on PATH: true` en la máquina que ejecuta los workers. Sin él, los comandos de shell pasan sin cambios.
4. Mantén `AllowedCommands` restringido: `LocalShell` corre en el host, sin sandbox.

## Ver también

- [independent_engineering_team](../independent_engineering_team/README.es-ES.md): el origen del equipo copiado, sin compresión
- [compress_history_summary](../compress_history_summary/README.es-ES.md), [compress_shell_rtk](../compress_shell_rtk/README.es-ES.md), [decider_laya_http](../decider_laya_http/README.es-ES.md), [decider_laya_native](../decider_laya_native/README.es-ES.md)
