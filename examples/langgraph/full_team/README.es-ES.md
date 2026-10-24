# full_team

[English](README.md) | [Português](README.pt-BR.md) | **Español**

El flujo del equipo de ingeniería (hub, gerente, workers por Agent Protocol y MCP, memoria del equipo, leases) con la capa `compress` en cada worker: las llamadas al modelo de cada agente se comprimen, su herramienta de shell envía los comandos simples por `rtk`, y dos reescrituras más corren sobre el mismo middleware: **Ponytail** (una nota que pide el código mínimo posible) y **TOON** (resultados de herramientas en JSON escritos en una forma más corta). Sin conexión funciona con un decisor guionizado, así que el efecto de cada palanca se ve sin modelo.

Esta carpeta es autocontenida. No importa `examples/langgraph/independent_engineering_team`; el código se copió de allí.

## Qué hace

- **Copiado de `independent_engineering_team`**: `internal/team` (hub, gerente, workers, conocimiento, memoria del equipo, pruebas), `skills/` (personas de los agentes) y `kb/` (estándares y checklists). La copia solo añade los ganchos de abajo, así que el original queda intacto.
- **Ganchos añadidos al equipo**: `WorkerConfig.Middleware` y `WorkerConfig.Tools` (vacío significa sin cambios), `LocalOptions` y `StartLocalWith`, que entrega middleware y herramientas a cada worker. `StartLocal` es `StartLocalWith` sin nada adicional. `TestStartLocalWithMiddleware` comprueba que el middleware del worker se ejecuta.
- **Llamadas al modelo**: el agente de cada worker recibe `middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}`. Los turnos antiguos (Human y AI) se comprimen con caveman en cada llamada; las llamadas y resultados de herramientas mantienen sus ids y orden. El historial almacenado no se reescribe.
- **Ponytail**: cuando el decisor responde `ponytail`, el último mensaje Human de cada copia por llamada recibe la directiva Ponytail (subir la escalera: ¿hace falta que exista?, reutilizar lo que el código ya tiene, biblioteca estándar, función nativa de la plataforma, dependencia ya instalada, una línea; perezoso con la solución, nunca con la lectura). Nunca le pide al modelo que salte validación, manejo de errores, seguridad, accesibilidad ni las pruebas que protegen lógica de riesgo. La nota no se guarda y no entra en el system prompt.
- **TOON**: cuando el decisor responde `toon_tool_results`, cada resultado de herramienta cuyo texto es un objeto o array JSON se reescribe en TOON, pero solo si el texto TOON es estrictamente más corto. Sin conexión, cada worker llama a la herramienta `open_tasks` una vez por turno (`team.OfflineWithTool`): devuelve el backlog como un array JSON de objetos uniformes, que TOON escribe como tabla. El resultado llega al modelo y se reescribe. La salida de otras herramientas no se toca.
- **Shell**: cada worker recibe la herramienta `shell` (`{command}`) sobre `backends.LocalShell`, con `AllowedCommands` (`go git ls grep echo` sin conexión; `ls grep echo` cuando hay clave de proveedor o algún rol usa un modelo real, véase `allowedPrograms`) y `Rewrite: rtk.Rewrite`. `RTK.Env` es el `Env` del shell, así `rtk` se busca en el mismo PATH que usa el shell. `rtk.Executor` registra el tamaño de la salida para la decisión de enrutado. Los comandos ven un entorno mínimo (`PATH` y un `HOME` dentro del directorio temporal, así que ninguna clave de proveedor les llega). Se rechaza un comando con `<`, `>`, `$`, acento grave, barra invertida o llave. Se rechazan los argumentos de ruta y los valores de `--flag=valor` y de flags cortos pegados (`-f/etc/x`) que resuelvan fuera del workspace, enlaces simbólicos incluidos (`shell: paths outside the workspace are not allowed`). Se deniegan los flags que leen un archivo de patrones o siguen enlaces simbólicos (`grep -f`, `--file`, `--exclude-from`, `--include-from`, `-R`) y `-C`, `--git-dir`, `--work-tree` en `go` y `git`. Un glob en un argumento de ruta se rechaza cuando su prefijo literal (la parte antes del primer `*` o `?`) está fuera del workspace, cuando contiene `[` o cuando tiene `..` después de un comodín; los patrones sueltos como `[0-9]` o `*.go` y los globs relativos al workspace como `src/*.tsx` funcionan. Se permite un nombre que no existe en disco, como un patrón de grep `/api/coupon`. Es una protección para la demo, no un sandbox.
- **Decisor**: guionizado sin conexión (responde con confianza a todo, así que compresión y TOON se aplican). `ponytail` lo responde la regla (`compress.RuleDecider`), así que la nota aparece solo donde la conversación tiene una proporción real de bloques de código. Con `LAYA_URL` definido, `compress.HTTPDecider` consulta un endpoint de laya-serve.
- **Informe**: al terminar imprime cuántas llamadas al modelo fueron reescritas, los tokens estimados antes y después (bytes/4), cuántas llamadas llevaron la nota Ponytail, cuántos resultados de herramienta se reescribieron en TOON con los bytes ahorrados, y si `rtk` está en el PATH. Los conteos vienen de un envoltorio del middleware (`meter`), no de cambios en la biblioteca. Una ejecución sin conexión mostró 46 llamadas de modelo, 31 reescrituras TOON por llamada que ahorraron 11625 bytes, y 46114 tokens estimados antes y 42964 después. Los números impresos varían un poco entre ejecuciones (tiempos de las goroutines). La ejecución sin conexión puede no mostrar notas Ponytail: la regla solo añade una cuando la conversación tiene una proporción real de código, y la ejecución guionizada tiene poca. La nota Ponytail añade unos 0,8 KB a una llamada; solo compensa cuando la salida del modelo se encoge, algo que la ejecución guionizada sin conexión no mide.

`native/` es el mismo programa con el decisor respondido por un modelo laya en proceso. Es un paquete del módulo raíz: `native.go` tiene la build tag `laya_native` e importa `laya-go`; `stub.go` tiene `!laya_native` y se usa por defecto. Su fallback guionizado no tiene la herramienta `open_tasks`, así que su informe no muestra reescrituras TOON sin conexión. Ese fallback responde `ponytail` con no, y su medidor no cuenta notas Ponytail.

## Cómo aplicarlo

- Middleware: `Keep` es cuántos turnos recientes se dejan intactos (por defecto 4; esta demo usa 1 porque los hilos son cortos).
- Shell: `AllowedCommands` en `shellTool`. Nunca listes shells, intérpretes, `env`, `xargs` ni `find`. `go` y `git` pueden ejecutar código, así que el host necesita sandbox para cualquier entrada no confiable. Solo se ofrecen en la ejecución offline con guion: un modelo real (clave de proveedor, `AGENT_MODEL` o `AGENT_MODEL_<ROLE>`) recibe solo `ls`, `grep` y `echo`.
- Decisor: `decider()` en `main.go`. Envuelve cualquier decisor con `compress.NewGuarded` para que uno lento o incorrecto caiga en las reglas.
- Desactivar Ponytail: sin conexión, la regla lo responde; para forzar la desactivación, responde `P: 0.05` para `compress.QPonytail` en `scripted()`. Desactiva TOON igual, para `compress.QTOON`. Con laya, mandan las respuestas del decisor; envuélvelo para que esos dos ids siempre vuelvan en falso si quieres desactivarlos ahí. Desactivar uno o los dos no cambia las herramientas ni el resto del flujo.
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

## Integración con el chat y /present

`serve` ejecuta el mismo equipo en un proceso y lo expone como servidor MCP por stdio, para que un chat (Claude Code, Copilot Chat en modo Agent) lo controle:

```bash
go run ./examples/langgraph/full_team serve
go build -o bin/team-mcp ./examples/langgraph/full_team && ./bin/team-mcp serve
```

El stdout lleva solo el protocolo MCP. Las líneas de arranque (`hub: up on ...`, `qa-1: qa up on ... (model ...)`) van al stderr. Se detiene al terminar la entrada (Ctrl-D) o con una señal. El modo serve difiere de la demo en tres puntos: ningún worker recibe la herramienta `shell`, el manager corre con una API key aleatoria generada al arrancar (un `MANAGER_API_KEY` definido por el operador se ignora; solo la bridge de este proceso conoce la clave) y no se envía ninguna demanda hasta que el chat llame a `team_submit`. Las herramientas son `team_submit`, `team_status`, `team_queue`, `team_ask_worker`, `team_present`, `team_memory`, `graph_run`, `graph_state` y `graph_resume`.

Dos hechos más sobre el modo serve. El argumento `url` de `graph_run`, `graph_state` y `graph_resume` permite que el chat haga que la bridge llame a cualquier URL de Agent Server; la clave del manager solo se envía cuando `url` está vacío o es igual a la URL del propio manager, y cualquier otra URL no recibe clave. El hub y los servidores de los workers escuchan en `127.0.0.1` y no tienen autenticación, así que cualquier proceso local puede alcanzarlos.

Regístralo en el cliente (mismos formatos que [Use it from VS Code](../full_team_front/README.es-ES.md#usarlo-desde-vs-code) en `full_team_front`, que este sigue; el primer `go run` compila el ejemplo, y el equipo se rinde tras 10 segundos si sus workers no suben, así que prefiere el binario compilado si el chat agota el tiempo). `.mcp.json` de Claude Code, en la raíz del proyecto:

```json
{
  "mcpServers": {
    "team": {
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team", "serve"]
    }
  }
}
```

o por la línea de comandos:

```bash
claude mcp add --transport stdio --scope project team -- go run ./examples/langgraph/full_team serve
claude mcp add --transport stdio --scope user --env ANTHROPIC_API_KEY=... team -- /abs/path/to/this/repo/bin/team-mcp serve
```

`.vscode/mcp.json` para Copilot Chat (habilita el servidor en el selector de herramientas del modo Agent):

```json
{
  "servers": {
    "team": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team", "serve"]
    }
  }
}
```

Ni el panel de la extensión Claude Code en VS Code ni el fragmento de Copilot se probaron aquí contra este servidor; son los formatos que usa el README hermano.

### /present

Un easter egg para comprobar que el chat llegó al equipo: pasar lista como en la escuela. A cada worker registrado se le pregunta, en paralelo (30 segundos como máximo cada uno), y debe responder una línea que empieza con `Presente!` y nombra su id, rol y modelo. La herramienta `team_present` hace las preguntas y devuelve la lista. El prompt MCP `present` ("Roll call: every persona answers Presente") solo le dice al chat que llame a esa herramienta y muestre el resultado, porque un prompt por sí solo no hace responder a nadie. Si el chat no encuentra la herramienta, el texto del prompt le hace decir que la integración no funciona.

Escritura del slash (NO VERIFICADA: no confirmada en los clientes, tomada de cómo nombran los prompts MCP): en Claude Code `/mcp__team__present` cuando el servidor está registrado como `team`; en Copilot Chat `/mcp.team.present`. Si el slash no aparece, pídelo con palabras: "llama a la herramienta team_present y muestra la lista".

La columna de modelo viene del propio worker: informa al hub, al registrarse, el modelo con el que se creó, así que la lista no depende de que el modelo sepa su propio nombre. Offline toda fila dice `offline`. Con `ANTHROPIC_API_KEY` definida, cada rol corre su modelo (`po` y `architect` en `anthropic:claude-opus-5-5`; `leader`, `senior` y `specialist` en `anthropic:claude-sonnet-5-5`; `junior`, `mid` y `qa` en `anthropic:claude-haiku-5-5`), salvo que `AGENT_MODEL` o `AGENT_MODEL_<ROLE>` lo sustituya, así que la lista muestra cada rol en el modelo configurado para él. La lista parte del elenco esperado (los 11 workers del cast) combinado con lo que lista el hub, así que un worker que cayó y perdió el lease, o que nunca arrancó, igualmente recibe una fila (`absent: not registered (lease expired or never started)`) y el resumen dice `10 of 11 present` seguido de `Absent: <id>`. Un worker que falla, agota el tiempo o no responde nada también aparece como ausente. Un worker registrado fuera del elenco aparece con la marca `extra (not in the roster, not counted)` y no se cuenta; el resumen es siempre `N of M present`, con M el tamaño del elenco. Salida real del equipo offline (11 workers, ordenados por id; las respuestas son las guionizadas, un modelo real redacta las suyas):

```text
ROLL CALL
- architect (architect) [model offline] present: Presente! architect (architect), com o diagrama na mão
- junior-1 (junior) [model offline] present: Presente! junior-1 (junior), de caderno aberto
- junior-2 (junior) [model offline] present: Presente! junior-2 (junior), de caderno aberto
- leader (leader) [model offline] present: Presente! leader (leader), organizando a fila
- mid-1 (mid) [model offline] present: Presente! mid-1 (mid), no ritmo
- po (po) [model offline] present: Presente! po (po), com o backlog debaixo do braço
- qa-1 (qa) [model offline] present: Presente! qa-1 (qa), testando até a chamada
- qa-2 (qa) [model offline] present: Presente! qa-2 (qa), testando até a chamada
- senior-1 (senior) [model offline] present: Presente! senior-1 (senior), sempre pronto
- specialist-1 (specialist) [model offline] present: Presente! specialist-1 (specialist), de lupa na mão
- specialist-2 (specialist) [model offline] present: Presente! specialist-2 (specialist), de lupa na mão
11 of 11 present
```

Con un worker caído, las últimas líneas son `10 of 11 present` y `Absent: qa-2`.

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
