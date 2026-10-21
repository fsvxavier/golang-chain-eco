# full_team

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El flujo del equipo de ingeniería (hub, gerente, workers por Agent Protocol y MCP, memoria del equipo, leases) con la capa `compress` en cada worker: las llamadas al modelo de cada agente se comprimen, su herramienta de shell envía los comandos simples por `rtk`, y dos reescrituras más corren sobre el mismo middleware: **Ponytail** (una nota que pide el código mínimo posible) y **TOON** (resultados de herramientas en JSON escritos en una forma más corta). Sin conexión funciona con un decisor guionizado, así que el ahorro se ve sin modelo.

Esta carpeta es autocontenida. No importa `examples/langgraph/independent_engineering_team`; el código se copió de allí.

## Qué hace

- **Copiado de `independent_engineering_team`**: `internal/team` (hub, gerente, workers, conocimiento, memoria del equipo, pruebas), `skills/` (personas de los agentes) y `kb/` (estándares y checklists). La copia solo añade los ganchos de abajo, así que el original queda intacto.
- **Ganchos añadidos al equipo**: `WorkerConfig.Middleware` y `WorkerConfig.Tools` (vacío significa sin cambios), `LocalOptions` y `StartLocalWith`, que entrega middleware y herramientas a cada worker. `StartLocal` es `StartLocalWith` sin nada adicional. `TestStartLocalWithMiddleware` comprueba que el middleware del worker se ejecuta.
- **Llamadas al modelo**: el agente de cada worker recibe `middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}`. Los turnos antiguos (Human y AI) se comprimen con caveman en cada llamada; las llamadas y resultados de herramientas mantienen sus ids y orden. El historial almacenado no se reescribe.
- **Ponytail**: cuando el decisor responde `ponytail`, el último mensaje Human de cada copia por llamada recibe la directiva Ponytail (subir la escalera: ¿hace falta que exista?, reutilizar lo que el código ya tiene, biblioteca estándar, función nativa de la plataforma, dependencia ya instalada, una línea; perezoso con la solución, nunca con la lectura). Nunca le pide al modelo que salte validación, manejo de errores, seguridad, accesibilidad ni las pruebas que protegen lógica de riesgo. La nota no se guarda y no entra en el system prompt.
- **TOON**: cuando el decisor responde `toon_tool_results`, cada resultado de herramienta cuyo texto es un objeto o array JSON se reescribe en TOON, pero solo si el texto TOON es estrictamente más corto. Sin conexión, cada worker llama a la herramienta `open_tasks` una vez por turno (`team.OfflineWithTool`): devuelve el backlog como un array JSON de objetos uniformes, que TOON escribe como tabla. El resultado llega al modelo y se reescribe. La salida de otras herramientas no se toca.
- **Shell**: cada worker recibe la herramienta `shell` (`{command}`) sobre `backends.LocalShell`, con `AllowedCommands` (`go git ls grep echo` sin conexión; `ls grep echo` cuando `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` está definida, véase `allowedPrograms`) y `Rewrite: rtk.Rewrite`. `RTK.Env` es el `Env` del shell, así `rtk` se busca en el mismo PATH que usa el shell. `rtk.Executor` registra el tamaño de la salida para la decisión de enrutado.
- **Decisor**: guionizado sin conexión (responde con confianza a todo, incluidos `ponytail` y `toon_tool_results`, así que compresión, Ponytail y TOON se aplican). Con `LAYA_URL` definido, `compress.HTTPDecider` consulta un endpoint de laya-serve.
- **Informe**: al terminar imprime cuántas llamadas al modelo fueron reescritas, los tokens estimados antes y después (bytes/4), cuántas llamadas llevaron la nota Ponytail, cuántos resultados de herramienta se reescribieron en TOON con los bytes ahorrados, y si `rtk` está en el PATH. Los conteos vienen de un envoltorio del middleware (`meter`), no de cambios en la biblioteca. Una ejecución sin conexión mostró 46 llamadas con la nota Ponytail, 33 reescrituras TOON que ahorraron 12375 bytes, y 47818 tokens estimados antes y 53919 después. Los tokens suben porque la nota Ponytail añade unos 1 KB a cada llamada; solo compensa cuando la salida del modelo se encoge, algo que la ejecución guionizada sin conexión no mide.

`native/` es el mismo programa con el decisor respondido por un modelo laya en proceso. Es un paquete del módulo raíz: `native.go` tiene la build tag `laya_native` e importa `laya-go`; `stub.go` tiene `!laya_native` y se usa por defecto. Su fallback guionizado no tiene la herramienta `open_tasks`, así que su informe no muestra reescrituras TOON sin conexión.

## Cómo aplicarlo

- Middleware: `Keep` es cuántos turnos recientes se dejan intactos (por defecto 4; esta demo usa 1 porque los hilos son cortos).
- Shell: `AllowedCommands` en `shellTool`. Nunca listes shells, intérpretes, `env`, `xargs` ni `find`. `go` y `git` pueden ejecutar código, así que el host necesita sandbox para cualquier entrada no confiable. Solo se ofrecen en la ejecución offline con guion: un modelo real con clave de proveedor recibe solo `ls`, `grep` y `echo`.
- Decisor: `decider()` en `main.go`. Envuelve cualquier decisor con `compress.NewGuarded` para que uno lento o incorrecto caiga en las reglas.
- Desactivar Ponytail: sin conexión, responde `P: 0.05` para `compress.QPonytail` en `scripted()`. Desactiva TOON igual, para `compress.QTOON`. Con laya, mandan las respuestas del decisor; envuélvelo para que esos dos ids siempre vuelvan en falso si quieres desactivarlos ahí. Desactivar uno o los dos no cambia las herramientas ni el resto del flujo.
- Activar TOON en tus herramientas: cualquier herramienta cuyo resultado sea un objeto o array JSON se beneficia, siempre que sea una lista uniforme de objetos con campos primitivos; `open_tasks` en `main.go` es el ejemplo.

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
go test -race -count=1 ./examples/langgraph/full_team/...   # incluye TestStartLocalWithMiddleware y TestOfflineRunReportsPonytailAndTOON (ejecución sin conexión: al menos una nota Ponytail y una reescritura TOON con bytes ahorrados)
```

## Cómo probarlo en modo producción

1. Define `LAYA_URL` (o `LAYA_BUNDLE_DIR`, en nativo) y comprueba que el informe muestra llamadas reescritas y tokens bajando en hilos largos. Los hilos cortos pueden no ahorrar, o subir un poco por la directiva de respuesta y la nota Ponytail.
2. Compara el conteo de Ponytail con la parte de código: el decisor laya decide por llamada, y una llamada con la nota solo compensa si la respuesta tiene menos código. Compara los tokens de salida con y sin `ponytail` en la misma petición antes de dejarlo activo.
3. Observa la latencia de la primera llamada: el timeout de 2s del guard cae en las reglas, y el informe no separa esas respuestas de las de laya. Compara con una ejecución sin `LAYA_URL`.
4. Confirma `rtk on PATH: true` en la máquina que ejecuta los workers. Sin él, los comandos de shell pasan sin cambios.
5. Mantén `AllowedCommands` restringido: `LocalShell` corre en el host, sin sandbox.

## Ver también

- [independent_engineering_team](../independent_engineering_team/README.es-ES.md): el origen del equipo copiado, sin compresión
- [compress_history_summary](../compress_history_summary/README.es-ES.md), [compress_shell_rtk](../compress_shell_rtk/README.es-ES.md), [decider_laya_http](../decider_laya_http/README.es-ES.md), [decider_laya_native](../decider_laya_native/README.es-ES.md)
