# full_team_front

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Una copia de [full_team](../full_team/README.es-ES.md) en la que todo el equipo trabaja en front-end y experiencia de usuario. Funciona como servidor MCP: VS Code (u otro chat con MCP) envía una demanda, y el equipo la convierte en código de pantallas revisado y en pruebas. La demanda pasa por el product owner, el equipo de ingeniería, el desarrollador, la revisión conjunta (especialista y tech lead) y QA. Cualquier problema que se encuentre en la revisión o en QA vuelve al desarrollador que escribió el código.

Sin conexión, funciona con modelos falsos y un backend falso, así que todo el flujo corre sin clave. Esta carpeta es autocontenida: el código del equipo se copió, no se importó, porque Go no permite importar el paquete `internal/` de otro árbol.

## Qué hace

- **Equipo y modelos.** Doce workers (el `Cast` en `internal/team/local.go`), más el manager y el hub. En este ejemplo todo corre en un solo proceso: cada servicio escucha en su propio puerto de loopback y se hablan por HTTP real. Cada rol tiene un modelo; los valores por defecto aplican cuando solo está definida `ANTHROPIC_API_KEY`.

  | Worker | Cant. | Modelo | Función |
  |---|---|---|---|
  | `po` (product owner) | 1 | `claude-opus-5-5` | lee el backend, escribe la spec |
  | `leader` (tech lead) | 1 | `claude-sonnet-5-5` | planifica las tareas, indica un desarrollador preferido, participa en la revisión conjunta |
  | `junior-1`, `junior-2` | 2 | `claude-haiku-5-5` | construyen el código de las pantallas y sus pruebas unitarias |
  | `mid-1`, `mid-2` | 2 | `claude-haiku-5-5` | igual |
  | `senior-1`, `senior-2` | 2 | `claude-haiku-5-5` | igual |
  | `specialist-1`, `specialist-2` | 2 | `claude-sonnet-5-5` | revisan el código con el tech lead |
  | `qa-1`, `qa-2` | 2 | `claude-sonnet-5-5` | escriben las pruebas automatizadas de las pantallas |

  Dos roles más tienen modelo pero no son workers: `manager` (`claude-sonnet-5-5`, conduce el flujo de entrega y escribe el informe) y `knowledge` (`claude-haiku-5-5`, responde a las herramientas de conocimiento en el hub).

  Elección del modelo, vale la primera que coincida, por rol: `AGENT_MODEL_<ROL>` (`MANAGER`, `PO`, `LEADER`, `JUNIOR`, `MID`, `SENIOR`, `SPECIALIST`, `QA`, `KNOWLEDGE`), luego `AGENT_MODEL` (un modelo para todos los roles), luego `ANTHROPIC_API_KEY` (los valores por defecto de arriba), luego `OPENAI_API_KEY` (`openai:gpt-4o-mini` para todos los roles). Ejemplos: `AGENT_MODEL_PO=anthropic:opus_5_5`, `AGENT_MODEL_QA=openai:gpt-4o-mini`. Sin ninguna de ellas la ejecución es sin conexión: todos los roles responden con un modelo guionado.

- **Flujo.** El manager recibe la demanda y activa al PO. El PO escribe la spec a partir de la demanda y del backend que lee. El leader planifica las tareas (`TASK|id|título|complejidad|descripción`, opcionalmente `|PREFER:<id del worker>`). El desarrollador construye el código de las pantallas y sus pruebas unitarias. Luego corre la **revisión conjunta**: el especialista y el leader reciben la misma entrega al mismo tiempo, y la tarea solo pasa cuando ambos aprueban. Después el QA escribe las pruebas automatizadas de las pantallas, trazadas a la spec del PO, y aprueba cuando la cubren. Las tareas corren en paralelo. El `PREFER:` del leader solo se mantiene si nombra un id de worker del elenco cuyo rol sirve a la complejidad de la tarea (un nombre de rol solo, un id desconocido, un revisor o un junior en una tarea `high` se descarta, con una línea de log); una complejidad desconocida pasa a `medium`.

- **Quién recibe una tarea.** Al leader se le informa qué desarrolladores están libres u ocupados. El hub entrega el job al primer worker libre de la lista de preferencia: el worker que el leader prefirió, luego los roles por complejidad (`low`: junior, mid, senior; `medium`: mid, senior, junior; `high`: senior, mid, nunca un junior). Un job que nadie puede tomar todavía espera en la cola.

- **El retrabajo vuelve al mismo desarrollador.** Cuando la revisión conjunta o el QA rechaza una tarea, el feedback va al desarrollador que la escribió, en el mismo hilo. Ese job de retrabajo está fijado (pinned): espera a ese desarrollador en lugar de ir a otro. Si el lease del desarrollador expiró, el job pasa a un desarrollador libre del mismo rol. Todo rechazo, de la revisión o del QA, cuesta una ronda; una tarea rechazada en `MaxRounds` (3) rondas se registra como `blocked` en el informe del manager y en el historial de handoffs (su estado en el tracker queda en el último rechazo). La solicitud igualmente termina `completed`.

- **El PO lee el backend nombrado en la demanda.** El PO tiene tres herramientas de solo lectura, y solo él las tiene. El PO nunca tiene herramienta de shell (vea Shell en Cómo aplicarlo). Las raíces y las URL vienen solo de los campos estructurados de `team_submit` (`backend_roots`, `backend_urls`), nunca de texto libre de la solicitud. Una demanda sin raíces y sin URL no da al PO ningún acceso: cada herramienta rechaza.
  - `backend_read`: lee un archivo o lista un directorio. La ruta puede ser relativa a la primera raíz o absoluta dentro de una raíz. Cualquier elemento `..` se rechaza, y también una ruta real (tras resolver symlinks) fuera de las raíces nombradas. Toda raíz nombrada debe estar dentro de la raíz del workspace (`WORKSPACE_ROOT`, si no el directorio de trabajo); una raíz relativa se toma respecto de la raíz del workspace, y una raíz vacía no concede nada. La salida está limitada a 1 MB.
  - `backend_grep`: busca en un archivo o directorio bajo las mismas raíces con una expresión regular de Go y devuelve `ruta:línea: texto`. Nunca sigue symlinks, se detiene cuando la salida llega a 1 MB y abre como máximo 5000 archivos (`[truncated: stopped after 5000 files]`).
  - `backend_get`: solo `GET` HTTP; no hay argumento de método. El esquema debe ser http o https, la URL no puede llevar credenciales, y su host y puerto deben ser iguales a los de una URL nombrada en la demanda. Una redirección se comprueba con la misma regla (como máximo 5), así que una redirección a otro host se rechaza. No se usa proxy. También se comprueba la dirección realmente marcada: loopback, link-local (que contiene la dirección de metadatos `169.254.169.254`), no especificada y las direcciones IPv6 de metadatos de Alibaba y AWS se rechazan, salvo que ese `ip:puerto` exacto se haya nombrado como literal de IP. El nombre `localhost`, cuando la demanda lo nombra, puede llegar a direcciones de loopback y nada más. Las direcciones privadas (RFC 1918) se permiten. El cuerpo está limitado a 1 MB y la llamada expira a los 10 segundos.

  `WORKSPACE_ROOT` la leen las herramientas de backend cuando el servidor arranca; vea la sección de MCP para saber qué proceso es ese.

- **La spec.** El PO lista los endpoints que leyó, los payloads de solicitud y respuesta, los campos obligatorios, los errores y los estados de UI (cargando, vacío, error). Todo payload que describe debe venir de algo que leyó. La spec tiene un bloque `EVIDENCE:` (archivo y línea, o URL, de cada payload) y un bloque `OPEN ITEMS:` para lo que no pudo confirmar. El PO no se bloquea por ítems abiertos y no inventa payloads. **Regla de evidencia:** si la demanda nombra una raíz o URL de backend, la spec necesita una línea que empiece con `EVIDENCE:` y al menos una llamada al backend que pasó las protecciones y obtuvo respuesta (las llamadas rechazadas no cuentan). La cuenta vive en el hub (`POST /calls/{id}`, `GET /calls/{id}`): las herramientas del PO reportan allí cada llamada así, bajo el id de la solicitud, y el manager lee la cuenta, así que la regla vale cuando el manager y el PO corren como servicios separados. El hub pone en cero la cuenta de una solicitud cuando se guarda su alcance al entrar, así que una nueva ejecución del mismo hilo parte de cero. Si no, el manager inserta la línea `- UNVERIFIED: no backend evidence` bajo `OPEN ITEMS:` (o agrega un bloque `OPEN ITEMS:`). Una demanda solo con `app_url` no activa la regla.

- **El QA escribe las pruebas.** El QA escribe un conjunto de pruebas por tarea para las pantallas que construyeron los desarrolladores, a partir de la spec del PO. Cada prueba lleva el nombre del criterio de aceptación o del estado de UI que cubre, y una línea `TRACE: AC1 -> <nombre de la prueba>` por criterio vincula pruebas con criterios. El QA responde `VERDICT: APPROVE` o `VERDICT: RETURN` con `FEEDBACK:` que nombra el ítem de la spec que el código de la pantalla no puede satisfacer (un estado incorrecto, un campo faltante, un problema de selector o de accesibilidad).

- **Ejecución opcional en el navegador (`browser_test`).** Solo el QA tiene esta herramienta. Ejecuta la spec de Playwright que el QA escribió contra la `app_url` nombrada en la demanda (el modelo no elige la URL). El comando lo fija el operador en `PLAYWRIGHT_CMD` se separa por espacios y se ejecuta sin shell, en un directorio temporal nuevo y vacío, con un timeout de 2 minutos y la salida limitada a 1 MB. La spec (como máximo 256 KB) se escribe en un archivo y su ruta se pasa en `SPEC_FILE`; la URL se pasa en `APP_URL`. El proceso hijo recibe un entorno mínimo: `PATH`, un `HOME` y un `TMPDIR` dentro del directorio temporal, `CI=1`, `PLAYWRIGHT_BROWSERS_PATH` si está definida, `APP_URL`, `SPEC_FILE`, y solo las variables que usted liste en `PLAYWRIGHT_ENV_ALLOW` (separadas por comas). Las claves de proveedor nunca le llegan, salvo que se listen. En timeout o cancelación se mata todo el grupo de procesos, y la espera por sus pipes se limita a 5 segundos (solo Unix). `app_url` debe ser una URL http o https con host y sin caracteres de control. Si `PLAYWRIGHT_CMD` está vacía, o la demanda no tiene una `app_url` válida, la herramienta devuelve `NOT RUN: <motivo>` y el QA igualmente escribe las pruebas. Una ejecución que falla se devuelve como texto `FAILED: ...`, no como error de la herramienta. Por eso el proceso hijo no tiene configuración de Playwright ni `node_modules`, y su `HOME` es el directorio temporal, así que un `npx playwright test` simple no tiene nada que ejecutar allí (sin probar). Recomendado: apunte `PLAYWRIGHT_CMD` a un script wrapper que viva en su propio proyecto de Playwright; el script lee `APP_URL` y `SPEC_FILE` del entorno, copia la spec al proyecto, entra en él con `cd` y ejecuta Playwright allí (sin probar aquí). Playwright necesita `PLAYWRIGHT_BROWSERS_PATH` (definida, o listada en `PLAYWRIGHT_ENV_ALLOW`) para encontrar sus navegadores, y cualquier otra variable que el script necesite debe listarse en `PLAYWRIGHT_ENV_ALLOW`. El QA sin conexión nunca llama a la herramienta.

- **Skills por persona.** Cada rol tiene su propia persona (front-end y UX) y skills de referencia incorporadas de repositorios públicos. Se embeben con `go:embed`, así que el ejemplo corre en cualquier máquina. Vea [Skills incorporadas](#skills-incorporadas).

- **Compresión.** Las llamadas al modelo de todo worker pasan por la misma capa de compresión que `full_team` (caveman en los turnos antiguos, Ponytail, TOON, RTK para comandos de shell). No se describe de nuevo aquí: vea [full_team](../full_team/README.es-ES.md#qué-hace). La herramienta de shell existe solo en la ejecución de demostración; vea Shell en Cómo aplicarlo.

- **Sin conexión.** El PO guionado llama a `backend_read`, `backend_grep` y `backend_get` contra un backend falso embebido (un pequeño documento OpenAPI, un árbol de handlers y un servidor `httptest`), así que las protecciones corren de verdad, y la spec vuelve con un bloque `EVIDENCE:`. El escenario es una pantalla de front-end, un campo de cupón en el formulario de checkout. El leader guionado planifica cinco tareas: `T1` componente del campo de cupón, `T2` hook de validación, `T3` anuncio de error y protección contra doble envío (prefiriendo `senior-1`), `T4` línea de descuento y estados de UI, `T5` teclado y foco; los desarrolladores guionados responden con un bloque TSX. Los revisores y el QA guionados rechazan tres de ellas una vez, así que se ve el regreso al mismo desarrollador: el tech lead rechaza `T2`, el especialista rechaza `T3`, el QA rechaza `T4`. Sin conexión los workers también tienen una herramienta `open_tasks` (un backlog fijo que da a la capa de compresión un resultado de herramienta que reescribir); solo se registra cuando ningún rol corre en un modelo real. El QA guionado responde solo con líneas `VERDICT` y `TRACE:`; no escribe código de pruebas y no ejecuta `browser_test`. La ejecución de demostración falla si la spec sin conexión vuelve `UNVERIFIED`.

## Cómo aplicarlo

- **Alcance de la demanda.** Pase el backend en la demanda, no en el texto de la solicitud: `backend_roots` para árboles de código, `backend_urls` para la API que el PO puede llamar, `app_url` para el front-end en ejecución que el QA puede abrir.
- **Roles y modelos.** `anthropicDefaults` en `internal/team/models.go` y `Cast` en `internal/team/local.go` guardan los valores por defecto y el pool. Cámbielos allí y mantenga el pool de doce workers; `TestCastHasTwelveWorkers` y `TestDefaultModels` lo comprueban.
- **Herramientas de backend.** Los límites (salida de 1 MB, timeout de 10 segundos, 5000 archivos de grep) están en `internal/backend/backend.go`. Mantenga `WORKSPACE_ROOT` tan estrecha como el backend lo permita.
- **Skills.** Para cambiar una persona, edite su `SKILL.md`. Para agregar o actualizar una skill incorporada, vea abajo.
- **Shell.** La herramienta de shell se copió de `full_team` y existe solo en la ejecución de demostración (`go run` sin `serve`): el modo `serve` no da shell a ningún rol, así que la salida de los desarrolladores es texto. En la demostración el proceso hijo del shell ve solo `PATH` y un `HOME` en un directorio temporal (sin claves de proveedor), y se rechaza un comando que contenga `<`, `>`, `$`, acento grave, barra invertida o llave. Sin conexión la lista de permitidos es `go`, `git`, `ls`, `grep` y `echo`; `go` y `git` se quitan siempre que algún rol tenga un modelo real (una clave de proveedor, `AGENT_MODEL` o `AGENT_MODEL_<ROL>`), quedando `ls`, `grep` y `echo`. No agregue intérpretes ni `find`. Los argumentos de ruta quedan confinados al directorio de trabajo del shell. El comando se separa en tokens una sola vez, respetando comillas simples y dobles, y se divide en segmentos solo en `;`, `&`, `|` y salto de línea fuera de comillas; se comprueba todo token de todo segmento. Un nombre simple, un argumento de ruta, el valor tras `=` en un `--opción=valor` o palabra `VAR=valor`, y el texto pegado a una opción corta (`-fRUTA`) que nombre una ruta existente fuera del directorio (ruta absoluta, `..` o enlace simbólico) se rechazan con `shell: paths outside the workspace are not allowed: <token>`, igual que todo lo que empiece con `~`. Un glob (`*`, `?`, `[`) se comprueba de dos maneras. Un glob con forma de ruta (tiene `/`, o empieza con `.` o `~`) se rechaza cuando contiene `[`, cuando su prefijo literal (los componentes antes del primer componente con comodín) no está dentro del directorio (así `/etc/pass*`, `../*/` y `/*/` se rechazan sin mirar las coincidencias), o cuando un componente `..` va después de un comodín. Cualquier glob también se rechaza cuando una coincidencia suya, o de su forma limpia (`/` final, `/.`, `//`), queda fuera del directorio; esto vale también dentro de comillas, más estricto que bash. Para globs simples, y para cualquier glob con `[`, la comprobación de coincidencias corre también sobre una copia en la que cada expresión de corchetes pasa a `?` (casa exactamente un carácter en bash y en Go, así que la copia casa un superconjunto), porque los dos dialectos leen `[!x]`, `[]x]` y `[[:alpha:]]` de forma distinta. Un componente con comodín que empieza con `.` y puede casar `.` o `..` (`.*`, `.?`, `..*`) se rechaza, porque bash anterior a 5.2 lo expande a `..`; esto rechaza también una regex simple de grep como `'.*'`. Un patrón simple (`grep '[0-9]' file`) y un glob del workspace (`ls *`, `src/*.tsx`) funcionan, y un glob sin coincidencias pasa. Todo token tras un `--` aislado cuenta como ruta. Un token que no existe en disco (un patrón de grep como `/api/coupon`) pasa. Las barras invertidas y las llaves se rechazan de entrada. Las opciones que leen archivos se deniegan: en `grep`, `-f`, `--file`, `--exclude-from`, `--include-from`, `-R` y `--dereference-recursive`, también como cualquier prefijo largo no ambiguo (`--deref`) y dentro de grupos cortos como `-nf` (así que `grep -efoo` también se rechaza; `-r`, `-n` y `--include=*.go` siguen funcionando); en `go` y `git`, `--git-dir`, `--work-tree` y un `-C` antes del subcomando. Las demás opciones no se inspeccionan. Es una protección para una herramienta solo de demostración, no un sandbox.

## Skills incorporadas

`skills/skills.lock` tiene 29 entradas, una por copia: cada carpeta de persona tiene su propia copia de toda skill que lleva, en `skills/<rol>/references/<nombre>/SKILL.md`. El cargador las agrega al prompt de sistema de la persona, ordenadas por nombre, bajo `## Skill: <nombre>`.

| Carpeta de la persona | Skills (copias) | Tamaño |
|---|---|---|
| `product-owner` | `contract-first`, `openapi-spec-generation`, `user-stories` (3) | 9,1 KB |
| `engineering-manager` | `code-review`, `planning-and-task-breakdown` (2) | 5,9 KB |
| `tech-leader` | las mismas dos (2) | 5,9 KB |
| `software-engineer-junior`, `-mid`, `-senior` | `frontend-design`, `frontend-patterns`, `react-testing`, `ui-ux-pro-max`, `vercel-react-best-practices` (5 cada una, 15) | 17,3 KB cada una |
| `specialist-reviewer` | `accessibility`, `frontend-code-review`, `performance-optimization`, `web-design-guidelines` (4) | 18,3 KB |
| `qa-engineer` | `playwright-best-practices`, `playwright-cli`, `webapp-testing` (3) | 10,7 KB |

Son 17 skills distintas en 29 copias. Hechos sobre ellas:

- **Son copias recortadas y saneadas, no los archivos originales.** Se cortaron ejemplos y texto de relleno para caber en el presupuesto, y se quitaron comandos de instalación, instrucciones de descarga remota, llamadas a scripts que no están incorporados y concesiones de permisos de herramientas (`allowed-tools`). Toda entrada del lock tiene `"trimmed": true`.
- **Campos del lock.** Cada línea de `skills/skills.lock` es un objeto JSON: `role`, `source` (`dueño/repo@skill`), `skill`, `installs` (el conteo de instalaciones que mostró `skills`, puede estar vacío), `hash` (SHA-256 del archivo embebido), `license`, `trimmed` y, para los tres snapshots, `pinned_from` (la URL del commit original).
- **Snapshots fijados.** `web-design-guidelines` es un snapshot fijado del `command.md` de vercel-labs/web-interface-guidelines en el commit `434b7f9` (la skill original solo descargaba sus reglas de una URL en tiempo de ejecución, y la copia no descarga nada). `playwright-best-practices` es una copia condensada de currents-dev/playwright-best-practices-skill en `283d5cb` (el original es un índice de muchos archivos de referencia). `user-stories` está fijada en phuryn/pm-skills en `07b34f6`.
- **Una skill pedida no se incorporó.** `user-story` de deanpeters/product-manager-skills es CC BY-NC-SA 4.0 (uso no comercial, compartir igual), lo que no sirve para redistribuir aquí. El PO usa `user-stories` de phuryn/pm-skills (MIT) en su lugar. Una skill cuya licencia no permite la redistribución se descarta, no se parafrasea.
- **Licencias.** MIT en la mayoría; Apache-2.0 en `frontend-design`, `webapp-testing` y `playwright-cli`. `frontend-code-review` (especialista) está bajo la Apache-2.0 modificada de Dify, con dos condiciones adicionales; se citan completas en los avisos. [skills/THIRD_PARTY_NOTICES.md](skills/THIRD_PARTY_NOTICES.md) lista cada repositorio de origen, skill, licencia y titular de copyright como se declara en el origen, con los textos de las licencias, y también está embebido.
- **Fijadas.** Nada las actualiza automáticamente.

### Actualizar o agregar una skill incorporada

1. Descargue la candidata en un directorio temporal, no en la carpeta global de skills: `npx skills add <dueño/repo@skill> -y` ejecutado dentro de un proyecto temporal. No use `-g`.
2. Lea su `SKILL.md` y su licencia (el archivo o la licencia del repositorio). Si la licencia no permite la redistribución, descártela.
3. Recórtela y sanéela hasta el presupuesto: unos 8 KB por archivo y 24 KB por rol. Quite comandos de instalación, descargas remotas, llamadas a scripts de archivos que no incorpora y concesiones `allowed-tools`. Copie el resultado a `skills/<rol>/references/<nombre>/SKILL.md`, una vez por cada persona que la lleva.
4. Agregue o actualice la entrada en `skills/skills.lock`: `source`, `skill`, `installs`, `hash` (`sha256sum` del nuevo archivo), `license`, `trimmed` (`true`) y `pinned_from` si es un snapshot.
5. Agregue la licencia, el repositorio y el titular de copyright a `skills/THIRD_PARTY_NOTICES.md`.
6. Ejecute las pruebas de abajo. `skills/skills_test.go` comprueba que toda entrada del lock tiene su archivo embebido y que el hash coincide, que ningún archivo pasa de 8 KB ni ningún rol de 24 KB, que toda entrada tiene licencia y origen, que toda referencia embebida está en el lock y que los avisos mencionan toda licencia, repositorio y skill del lock.

## Cómo ejecutarlo

Sin conexión, un proceso:

```bash
go run ./examples/langgraph/full_team_front
```

La ejecución imprime los servicios que inició y el informe de entrega. Levanta el hub, el manager y los workers en loopback y los detiene al final. Da al PO un backend falso en un workspace temporal; `WORKSPACE_ROOT` y `app_url` no se aplican a ella.

Con un proveedor real (los modelos son los valores por defecto de arriba):

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/full_team_front
OPENAI_API_KEY=...    go run ./examples/langgraph/full_team_front   # openai:gpt-4o-mini para todos los roles
```

## Usarlo como servidor MCP

El argumento `serve` ejecuta todo el equipo en el mismo proceso como servidor MCP por stdio (`serve.go`). Un editor lo inicia como comando; no hay nada más que levantar. Sirve al equipo mediante `Bridge.MCPServer` en `internal/team/bridge.go`.

Desde la raíz del repositorio:

```bash
go run ./examples/langgraph/full_team_front serve
```

O compile un binario y ejecútelo (arranca más rápido que `go run`, que compila en cada arranque):

```bash
go build -o bin/team-mcp ./examples/langgraph/full_team_front
bin/team-mcp serve
```

En la salida estándar el proceso escribe solo mensajes del protocolo MCP. Los logs, las líneas de estado del notificador de consola y el informe de compresión al apagar van a stderr. El servidor se detiene al terminar la entrada (el editor se cierra) o con SIGINT/SIGTERM.

Se usan modelos falsos sin conexión salvo que `ANTHROPIC_API_KEY` u `OPENAI_API_KEY` esté definida (o `AGENT_MODEL*`, vea arriba).

### Entorno

Como el equipo corre en el proceso del servidor, toda variable viene del entorno del servidor. En VS Code es el bloque `env` de `mcp.json`; en Claude Code es `--env` o el shell.

| Variable | La lee | Efecto |
|---|---|---|
| `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `AGENT_MODEL`, `AGENT_MODEL_<ROL>` | cada worker, el manager y el hub al iniciar (`internal/team/models.go`) | modelos reales en lugar de los guionados |
| `WORKSPACE_ROOT` | las herramientas de backend del PO (`internal/backend/backend.go`) | toda entrada de `backend_roots` debe estar dentro de ella; el valor por defecto es el directorio de trabajo del servidor |
| `PLAYWRIGHT_CMD` | la herramienta `browser_test` del QA, leída al arrancar (`main.go`) | comando que ejecuta la spec; vacío significa `NOT RUN` |
| `PLAYWRIGHT_ENV_ALLOW` | el ejecutor de comandos de `browser_test` (`internal/qa/qa.go`) | variables del proceso padre que se pasan a la ejecución en el navegador |
| `NOTIFY`, `NOTIFY_STATUSES`, ... | el notificador de estado del manager (`internal/tasktrack`) | adónde van las líneas de estado; `console` (stderr en modo serve) es el valor por defecto |
| `BRIDGE_CIAM_CMD`, `BRIDGE_KNOWLEDGE_CMD` | la bridge | líneas de comando de servidores MCP adicionales cuyas herramientas se sirven junto a las del equipo |
| `LAYA_URL`, `LAYA_API_KEY`, `LAYA_MODEL` | el decisor de compresión (`main.go`) | un decisor laya-serve real en lugar del guionado |

`MANAGER_URL` y `HUB_URL` no se leen en modo serve: la bridge está conectada a los servicios del propio proceso. En modo serve el manager está protegido por un `x-api-key` aleatorio por ejecución (32 bytes aleatorios, en hexadecimal) que solo conoce la bridge del propio proceso; una `MANAGER_API_KEY` definida por el operador se ignora. El hub y los servidores Agent Protocol de los workers escuchan en `127.0.0.1` y no tienen autenticación: cualquier proceso local puede alcanzarlos. Cancelar durante el arranque (SIGINT, SIGTERM, el cierre del editor) termina el proceso de inmediato y sin error.

### Herramientas

El servidor se llama `engineering-team` y sirve:

- `team_submit`: envía una demanda. Sus campos son `request` y, opcionalmente, `backend_roots`, `backend_urls` y `app_url`. Devuelve al instante `thread_id`, `run_id` y un estado.
- `team_status`: para un `thread_id`, devuelve `status` (la solicitud y cada tarea), `report`, `history` (los handoffs, incluido cada rechazo de revisión y de QA y a quién volvió) y `running`. `status`, `report` y el `history` completo se llenan cuando termina el último paso; mientras la ejecución avanza, el notificador de consola imprime cada cambio de estado en stderr.
- `team_queue`: los workers registrados (con su job actual) y los jobs en cola.
- `team_cancel`: cancela un pedido por `thread_id` (termina su thread): los jobs en cola se descartan, los agentes en ejecución se detienen (se enteran en su próximo heartbeat) y la entrega termina como `cancelled`. El flujo del manager se detiene en su siguiente paso; la llamada al modelo en curso aún termina. Los tokens de un job cancelado no se cuentan. Enviar el mismo thread otra vez inicia una ejecución limpia.
- `team_ask_worker`: hace una pregunta a un worker, fuera del flujo de entrega. Un worker que responde así no tiene alcance de demanda, así que las herramientas de backend del PO rechazan.
- `team_present`: pase de lista, véase [Pase de lista /present](#pase-de-lista-present). Cada worker esperado responde `Presente!` con id, rol y modelo.
- `team_memory`: recupera lo que el equipo registró para una solicitud (argumentos `scope`, el `thread_id` de la solicitud, y `query`).
- `graph_run`, `graph_state`, `graph_resume`: herramientas genéricas para cualquier grafo en un Agent Server (`url` por defecto es el manager).
- Las herramientas de los servidores nombrados en `BRIDGE_CIAM_CMD` y `BRIDGE_KNOWLEDGE_CMD`, si están definidas.

### Registrarlo

Vea [Usarlo desde VS Code](#usarlo-desde-vs-code) para el registro en VS Code, Claude Code y otros clientes.

### Una demanda

Un ejemplo de llamada a `team_submit` (los argumentos en JSON; las rutas y URL son marcadores). `backend_roots` debe estar dentro de `WORKSPACE_ROOT` (o del directorio de trabajo del servidor):

```json
{
  "request": "Add a coupon field to the checkout screen. Show a loading state while the coupon is checked, an empty state before any input, and an error message for an invalid code.",
  "backend_roots": ["/abs/path/to/workspace/backend"],
  "backend_urls": ["https://api.example.com/v1"],
  "app_url": "http://localhost:3000/checkout"
}
```

Luego llame a `team_status` con el `thread_id` devuelto y repita hasta que `report` esté presente y `running` esté vacío:

```json
{ "thread_id": "<thread_id from team_submit>" }
```

## Usarlo desde VS Code

### Requisitos previos

- El toolchain de Go 1.27 en el `PATH` (`go version`; `go.mod` dice `go 1.27`).
- El repositorio clonado y abierto como carpeta de workspace de VS Code. Así `go run ./examples/langgraph/full_team_front serve` funciona con el workspace como directorio de trabajo, y `WORKSPACE_ROOT` lo tiene como valor por defecto. La alternativa es el binario compilado (`go build -o bin/team-mcp ./examples/langgraph/full_team_front`) iniciado por ruta absoluta, con `WORKSPACE_ROOT` definida.
- Opcional: `ANTHROPIC_API_KEY` u `OPENAI_API_KEY`. Sin clave el equipo corre sin conexión con modelos guionados. En el chat eso significa: toda solicitud recibe el mismo plan guionado de cupones (cinco tareas, `T1` a `T5`), sin importar lo que pida, y los revisores y el QA guionados devuelven `T2`, `T3` y `T4` una vez. Eso basta para comprobar la conexión y ver el flujo; no es una entrega real.

### Claude Code en VS Code

Claude Code lee los servidores MCP con alcance de proyecto de `.mcp.json` en la raíz del proyecto (vea [independent_engineering_team](../independent_engineering_team/README.es-ES.md#claude-code), cuyos fragmentos esto sigue). Cree `.mcp.json` en la raíz del workspace:

```json
{
  "mcpServers": {
    "front-team": {
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team_front", "serve"],
      "env": {
        "WORKSPACE_ROOT": "/abs/path/to/this/repo",
        "PLAYWRIGHT_CMD": "",
        "PLAYWRIGHT_ENV_ALLOW": ""
      }
    }
  }
}
```

Los valores son literales. Este README no depende de la expansión `${VAR}` en `.mcp.json`, porque ni el README hermano ni el código la muestran. No escriba una clave en un archivo que versiona: agregue `ANTHROPIC_API_KEY` con `claude mcp add` (abajo, alcance local) o expórtela en el shell que inicia VS Code y compruebe en `/mcp` que los modelos no están sin conexión. `PLAYWRIGHT_CMD` y `PLAYWRIGHT_ENV_ALLOW` pueden omitirse para correr sin la herramienta de navegador. La forma con `go run` supone que el servidor arranca con la raíz del repositorio como directorio de trabajo; si no lo hace (sin probar), use el binario con un `command` absoluto.

El mismo servidor desde la línea de comandos (`--scope local` es el valor por defecto; `project` escribe `.mcp.json`; `user` vale para todos los proyectos; `--` separa las opciones del comando):

```bash
claude mcp add --transport stdio --scope project \
  --env WORKSPACE_ROOT=/abs/path/to/this/repo \
  front-team -- go run ./examples/langgraph/full_team_front serve

claude mcp add --transport stdio --scope user \
  --env WORKSPACE_ROOT=/abs/path/to/this/repo --env ANTHROPIC_API_KEY=... \
  front-team -- /abs/path/to/this/repo/bin/team-mcp serve
```

Los servidores con alcance de proyecto piden aprobación la primera vez. Compruebe el servidor con `claude mcp list`, o con `/mcp` en el chat: `front-team` debe aparecer como conectado y listar estas herramientas: `team_submit`, `team_status`, `team_queue`, `team_cancel`, `team_ask_worker`, `team_present`, `team_memory`, `graph_run`, `graph_state` y `graph_resume` (más las herramientas de `BRIDGE_CIAM_CMD` y `BRIDGE_KNOWLEDGE_CMD`, si las define); el servidor también lista el prompt `present`. No se probó aquí si el panel de chat de la extensión de Claude Code en VS Code usa los mismos archivos de configuración y muestra la misma pantalla `/mcp` que la CLI.

Si no conecta:

1. Ejecute el comando en una terminal, desde la raíz del repositorio: `go run ./examples/langgraph/full_team_front serve`. No debe imprimir nada en la salida estándar hasta que un cliente hable (la salida estándar es solo protocolo). Las líneas de arranque (`hub: up on ...`, `qa-1: qa up on ... (model ...)`) aparecen en stderr. Pulse Ctrl-D para detenerlo.
2. Lea esa salida de stderr. Si un worker no puede iniciar (típicamente un valor incorrecto en `AGENT_MODEL_<ROL>` o la falta de clave del proveedor elegido), el proceso termina con `team did not come up` tras 10 segundos; el error del propio worker no se imprime, así que revise primero esas variables.
3. Ejecute `go version` y compruebe que es 1.27 o más nuevo, y que `go` está en el `PATH` del proceso que inicia el servidor (un VS Code abierto desde un lanzador puede tener un `PATH` más corto que el de su shell).
4. Compruebe las rutas: el directorio de trabajo de `go run`, la ruta absoluta del binario, `WORKSPACE_ROOT`. El primer `go run` compila todo el ejemplo, así que la primera conexión puede ser lenta; el equipo se rinde si los workers no están arriba en 10 segundos, así que prefiera el binario compilado.

### Copilot Chat en VS Code (modo Agent)

Cree `.vscode/mcp.json` en el workspace (el formato de VS Code usa `servers`, cada uno con `"type": "stdio"`, como en el [fragmento de Copilot](../independent_engineering_team/README.es-ES.md#github-copilot-vs-code) del README hermano). Forma con `go run`:

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./examples/langgraph/full_team_front", "serve"],
      "env": { "ANTHROPIC_API_KEY": "${env:ANTHROPIC_API_KEY}" }
    }
  }
}
```

Forma con binario compilado:

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "${workspaceFolder}/bin/team-mcp",
      "args": ["serve"],
      "env": {
        "WORKSPACE_ROOT": "${workspaceFolder}",
        "ANTHROPIC_API_KEY": "${env:ANTHROPIC_API_KEY}"
      }
    }
  }
}
```

Quite la línea `ANTHROPIC_API_KEY` para ejecutar sin conexión. Luego abra el archivo y haga clic en **Start** sobre el servidor. En Copilot Chat elija **Agent** y habilite `front-team` en el selector de herramientas.

Otros clientes MCP (Cursor, Kilo Code, Opencode) aceptan un comando stdio con la misma forma (`command`, `args`, `env`); los fragmentos de [independent_engineering_team](../independent_engineering_team/README.es-ES.md#4-regístralo-en-cada-aplicación) muestran el archivo de cada cliente, con `command` y `args` reemplazados por los de arriba. No se probaron con este servidor.

### Usarlo desde el chat

Pasos con prompts que puede pegar en el chat de Claude Code (o en el modo Agent de Copilot). El chat decide cuándo llamar a las herramientas; nombrar la herramienta hace que la llame.

1. **Enviar una demanda.** Prompt:

   > Usa la herramienta `team_submit` con request "Add a coupon field to the checkout screen. Show a loading state while the coupon is checked, an empty state before any input, and an error message for an invalid code.", backend_roots ["/abs/path/to/this/repo/backend"], backend_urls ["http://localhost:8080"], app_url "http://localhost:3000/checkout". Luego dime el thread_id.

   Los campos: `request` es lo que el equipo debe entregar. `backend_roots` son los árboles de código que el PO puede leer; dé rutas absolutas, cada una dentro de `WORKSPACE_ROOT` (una relativa se toma respecto de ella, y una fuera de ella se rechaza). `backend_urls` son las API que el PO puede llamar con `GET`; solo esos hosts y puertos son alcanzables. `app_url` es el front-end en ejecución que el QA puede abrir con `browser_test` (requiere `PLAYWRIGHT_CMD`). Los tres son opcionales; sin raíces y URL el PO no lee nada y la spec lo dice. La llamada vuelve al instante:

   ```text
   thread_id=35a034fed3493843fc60f393894dd7ed run_id=646bef17962e894921b0a5e4fe5c0d92 status=pending
   ```

2. **Consultar el estado.** Prompt: "Llama a `team_status` con ese thread_id cada pocos segundos hasta que haya un `report`, y luego muéstrame el resultado." Los campos: `status` asigna a la solicitud (clave `""`) y a cada tarea (`T1`...) su etapa (`received` ... `completed`); `report` es el resumen del manager; `history` es la lista de handoffs; `running` lista los nodos que aún van a correr (`null` cuando la ejecución terminó). `status`, `report` y el `history` completo se llenan cuando termina el último paso, así que consulte hasta que `report` esté presente.

3. **Leer el historial.** Las devoluciones de la revisión conjunta y del QA aparecen en líneas como `T2: review returned by leader -> mid-1 | tech-leader: ...` y `T4: qa returned by qa-1 -> senior-1`. La línea siguiente de esa tarea, `T2: mid-1 -> review`, muestra al mismo desarrollador construyéndola de nuevo.

4. **Ver el equipo.** Prompt: "Llama a `team_queue`." Devuelve los workers registrados (`id`, `role`, `url`, `seen` y `job` mientras uno está ocupado; son 12) y los jobs en `queued`.

5. **Preguntar a un agente.** Prompt: "Llama a `team_ask_worker` con worker `leader` y pregunta: ¿qué desarrolladores están ocupados?" El id del worker sale de `team_queue`. La pregunta corre fuera del flujo de entrega, así que no tiene alcance de demanda: pedir al `po` que lea el backend así se rechaza.

6. **Recuperar lo que el equipo registró.** Prompt: "Llama a `team_memory` con scope <thread_id> y query `coupon`." El scope es el `thread_id` de la solicitud.

Para leer la spec del PO, llame a `graph_state` con el `thread_id` y mire `values.spec`.

Un ejemplo completo, sin conexión (sin clave, sin backend nombrado). Prompt del usuario: "Usa la herramienta team_submit con request 'Add a coupon field to checkout'." Llamada a la herramienta:

```json
{ "name": "team_submit", "arguments": { "request": "Add a coupon field to checkout" } }
```

Resultado: `thread_id=35a034fe... run_id=646bef17... status=pending`. Prompt del usuario: "Compruébalo con team_status." Llamada a la herramienta `team_status` con `{ "thread_id": "35a034fe..." }`; el resultado sin conexión, recortado (el real lista todas las tareas):

```json
{
  "history": [
    "manager -> product-owner",
    "product-owner (po) done",
    "tech-leader (leader) -> 5 tasks",
    "T2: mid-1 -> review",
    "T2: review returned by leader -> mid-1 | tech-leader: 1. The hook recomputes the discount in the component: show the discount_percent and total the API returned, ...",
    "T2: mid-1 -> review",
    "T2: review approved by specialist-1 and leader",
    "T2: qa approved by qa-1",
    "T2: approved",
    "T4: senior-1 -> review",
    "T4: review approved by specialist-1 and leader",
    "T4: qa returned by qa-1 -> senior-1",
    "T4: senior-1 -> review",
    "...",
    "manager -> requester"
  ],
  "report": "REPORT:\n- Shipped: T1, T5, T2, T3, T4\n- Blocked: none\n- Next: roll out behind a flag for 5% of checkout traffic.",
  "running": null,
  "status": { "": "completed", "T1": "completed", "T2": "completed", "T3": "completed", "T4": "completed", "T5": "completed" }
}
```

Solución de problemas:

- Herramientas no listadas: el servidor no conectó; vea "Si no conecta" arriba.
- `team_submit` vuelve pero `team_status` nunca llega a un `report`: con una clave real, un worker falló o una llamada al modelo está fallando. Lea el stderr del servidor (ejecute el comando en una terminal para verlo) y compruebe la clave, los nombres de modelo (`AGENT_MODEL_<ROL>`) y la red.
- La spec del PO tiene `UNVERIFIED: no backend evidence` bajo `OPEN ITEMS:`: la demanda nombró un backend pero la spec no tiene bloque `EVIDENCE:` o el PO no hizo ninguna llamada al backend que obtuviera respuesta. Las raíces o URL pueden haberse rechazado (fuera de `WORKSPACE_ROOT`, un host no nombrado, una dirección bloqueada). Corrija los argumentos de `team_submit`. El mismo marcador aparece si las herramientas del PO no pudieron reportar sus llamadas al hub (el fallo se registra en el log como `backend: counting a call ...`, pero la llamada a la herramienta sigue teniendo éxito) o si el manager no pudo leer la cuenta: la regla falla entonces hacia el lado seguro.
- `browser_test` dice `NOT RUN`: `PLAYWRIGHT_CMD` está vacía en el entorno del servidor, o la demanda no tiene una `app_url` válida. El motivo sigue a `NOT RUN:`.

### Pase de lista /present

Un easter egg para comprobar que el chat llegó al equipo: un pase de lista de escuela. La herramienta `team_present` pide a cada worker esperado (los 12 de este ejemplo) que responda una línea que empiece por `Presente!` y diga su id, rol y modelo, en paralelo y con 30 segundos como máximo cada uno. El prompt MCP `present` ("Roll call: every persona answers Presente") solo indica al chat que llame a esa herramienta e imprima el resultado, porque un prompt por sí solo no hace responder a nadie. Si el chat no encuentra la herramienta, el texto del prompt le hace decir que la integración no funciona.

Cómo llamarlo: pídalo en palabras simples, "llama a la herramienta `team_present` e imprime el pase de lista", o use el slash command. Grafías del slash (NO VERIFICADAS: no confirmadas en los clientes, tomadas de cómo nombran los prompts MCP): en Claude Code `/mcp__<servidor>__present`, es decir `/mcp__front-team__present` cuando el servidor se registra como `front-team` como arriba; en Copilot Chat `/mcp.<servidor>.present`, aquí `/mcp.front-team.present`.

Qué muestra el pase: una línea por worker con su id, rol y modelo, luego `N of 12 present`, luego `Absent:` con los ids que no respondieron. La columna del modelo viene del propio worker: informa el modelo con el que se construyó al registrarse en el hub, así que el pase no depende de que el modelo sepa su propio nombre. Sin conexión todas las líneas dicen `offline`. Con `ANTHROPIC_API_KEY` definida, cada rol corre su modelo (`po` en `anthropic:claude-opus-5-5`; `leader`, `specialist` y `qa` en `anthropic:claude-sonnet-5-5`; `junior`, `mid` y `senior` en `anthropic:claude-haiku-5-5`), salvo que `AGENT_MODEL` o `AGENT_MODEL_<ROLE>` lo cambie, así que el pase muestra si cada rol corre en el modelo configurado para él.

El pase se comprueba contra el equipo esperado, no solo contra los workers que el hub conoce. Un worker que cayó y cuyo lease de 15 segundos expiró salió del hub, y aun así recibe una línea: `absent: not registered (lease expired or never started)`, y la cuenta baja. Un worker registrado que falla, agota el tiempo o no responde nada también queda ausente. Un worker registrado que no es uno de los 12 aparece con `extra` y no cuenta en el total. Presente significa una respuesta no vacía: un modelo real que ignore la instrucción `Presente!` aun así cuenta como presente. Un buen pase indica que la integración del chat funciona de extremo a extremo: llamada a la herramienta, hub, agente de cada worker, modelo.

Salida real del equipo sin conexión por stdio (en el orden del elenco; las respuestas son las del guion, un modelo real redacta las suyas):

```
ROLL CALL
- po (po) [model offline] present: Presente! po (po), com a spec debaixo do braço
- leader (leader) [model offline] present: Presente! leader (leader), organizando a fila
- junior-1 (junior) [model offline] present: Presente! junior-1 (junior), de caderno aberto
- junior-2 (junior) [model offline] present: Presente! junior-2 (junior), de caderno aberto
- mid-1 (mid) [model offline] present: Presente! mid-1 (mid), no ritmo
- mid-2 (mid) [model offline] present: Presente! mid-2 (mid), no ritmo
- senior-1 (senior) [model offline] present: Presente! senior-1 (senior), sempre pronto
- senior-2 (senior) [model offline] present: Presente! senior-2 (senior), sempre pronto
- specialist-1 (specialist) [model offline] present: Presente! specialist-1 (specialist), de lupa na mão
- specialist-2 (specialist) [model offline] present: Presente! specialist-2 (specialist), de lupa na mão
- qa-1 (qa) [model offline] present: Presente! qa-1 (qa), testando até a chamada
- qa-2 (qa) [model offline] present: Presente! qa-2 (qa), testando até a chamada
12 of 12 present
```

Con un worker caído más tiempo que el lease (la misma ejecución de test, sin conexión), el pase termina así:

```
- qa-2 (qa) [model unknown] absent: not registered (lease expired or never started)
11 of 12 present
Absent: qa-2
```

### Notas de seguridad

- El servidor es un proceso stdio local iniciado por su editor, con los permisos de su usuario y el entorno que usted le dio. El chat es la raíz de confianza: lo que el chat pasa en `team_submit` (`backend_roots`, `backend_urls`, `app_url`) se convierte en el alcance de la solicitud.
- Las lecturas de archivos del PO están limitadas por `WORKSPACE_ROOT` (el directorio de trabajo del servidor, si no está definida) y por las raíces nombradas; los symlinks y `..` no pueden salir de ellas. Mantenga `WORKSPACE_ROOT` estrecha.
- El argumento `url` de `graph_run`, `graph_state` y `graph_resume` permite que el chat haga que la bridge llame a cualquier URL de Agent Server. La clave del manager solo se envía cuando `url` está vacío o es igual a la URL del propio manager; cualquier otra URL no recibe clave. `team_ask_worker` no tiene `url`: llega solo a los workers registrados en el hub, en loopback.
- En modo serve ningún rol tiene la herramienta `shell`: los desarrolladores devuelven el código como texto. La ejecución de demostración sin conexión conserva un shell endurecido (vea Shell en Cómo aplicarlo). El PO nunca tiene shell.
- `browser_test` ejecuta el comando que usted definió en `PLAYWRIGHT_CMD`, sin shell, con un entorno mínimo: sus claves de proveedor no le llegan, salvo que se listen en `PLAYWRIGHT_ENV_ALLOW`. La spec que ejecuta la escribe un modelo.
- En modo serve el manager exige una clave aleatoria por ejecución que solo tiene la bridge del propio proceso; una `MANAGER_API_KEY` definida por el operador se ignora. El hub y los servidores de los workers escuchan en `127.0.0.1` y no tienen autenticación, así que cualquier proceso local puede alcanzarlos.
- El contador de llamadas de la regla de evidencia vive en el hub (`POST /calls/{id}`, `GET /calls/{id}`), que escucha solo en loopback y no tiene autenticación: cualquier cliente local puede sumar llamadas a una solicitud, mientras que el modelo no lo alcanza. Una llamada tardía de una ejecución anterior en el mismo hilo aún puede contar después de que una nueva ejecución ponga el contador en cero.

## El PO mantiene los documentos del proyecto

El product owner es el único rol que puede escribir, y solo con `project_doc_write`: archivos markdown (`.md`, hasta 256 KB) bajo el directorio `project/` del workspace (`WORKSPACE_ROOT`, si no el directorio de trabajo), con `replace` o `append`. Cambie los directorios escribibles con `PO_DOCS_DIRS` (lista separada por comas, relativa al workspace). Todo lo demás se rechaza: otras extensiones, rutas fuera de esos directorios, `..` y symlinks que salen. El prompt del PO en el manager le indica mantener al día la documentación, el plan y los próximos pasos. El código fuente, y todos los demás roles, siguen siendo de solo lectura para él.

Uso de tokens: cada worker y el agente del propio manager pasan por el mismo middleware de compresión (turnos antiguos comprimidos en caveman, la nota Ponytail cuando las reglas lo piden, TOON para resultados JSON de herramientas).

## Panel de seguimiento

Una página web de solo lectura muestra cada pedido y tarea con estado, barra de progreso y tiempo de ejecución, y cada persona (worker) con su estado, el job actual, los jobs terminados y el tiempo ocupado. Se actualiza cada 2 segundos y muestra solo estados, tiempos y conteo de tokens, nunca prompts ni salidas. Los tokens se listan por persona (el manager cuenta como una), por tarea, por pedido y para todo el proyecto; las llamadas del modelo de conocimiento no se cuentan. Cada fila de persona también indica su carpeta de skill (con las referencias vendorizadas en el tooltip) y, mientras trabaja, qué está haciendo: la referencia que consulta (`rag_answer`, `cag_answer`, `knowledge_answer`, memoria del equipo), la herramienta que llama o el modelo.

Cada proyecto tiene su propia URL, `http://127.0.0.1:8088/p/<proyecto>/` (`/?project=<proyecto>` redirige allí), y la cabecera tiene un selector de proyecto. El primer equipo en arrancar es dueño del puerto y lista a los demás; un equipo que encuentra el puerto ocupado sirve en un puerto de loopback libre y se registra en el dueño (solo loopback), así una dirección cubre todos los proyectos. Un proyecto que deja de responder aparece como offline; si el dueño sale, los demás se registran de nuevo en el próximo equipo que ocupe el puerto.

- El modo `serve` (lo que inicia `team-mcp-run serve`) lo abre en `http://127.0.0.1:8088` (solo loopback).
- Demo y otras ejecuciones: define `DASHBOARD_ADDR=host:puerto` (el demo termina al acabar la ejecución).
- Para acceder desde otra máquina, usa `DASHBOARD_ADDR=0.0.0.0:8088` detrás de un proxy con autenticación; la página no tiene autenticación.

## Cómo probarlo

```bash
go test -race -count=1 ./examples/langgraph/full_team_front/...
go vet ./examples/langgraph/full_team_front/...
```

Una prueba: `go test -race -count=1 -run TestJointReviewNeedsBothApprovals ./examples/langgraph/full_team_front/internal/team`. Las pruebas, por paquete:

- `main` (`main_test.go`, `serve_test.go`): `TestAllowedProgramsWithLiveModelButNoKey`, `TestAllowedProgramsWithProviderKey`, `TestScriptedPonytailFollowsRule`, `TestOfflineRunReportsTOON`, `TestPOGetsNoShell`, `TestServeStackHasNoShell`, `TestDemoShellRejectsExpansionAndRedirects`, `TestShellEnvHasNoSecrets`, `TestDemoShellConfinesPaths`, `TestDemoShellRejectsFlagAndExpansionBypasses`, `TestDemoShellRejectsGlobAndQuoteBypasses`, `TestDemoShellRejectsStructuralGlobBypasses`, `TestDemoShellRejectsBareBracketGlobThroughSymlink`, `TestShellRefusesDotGlobs`, `TestOpenTasksOnlyOffline`, `TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`, `TestServeStopsOnContextCancel`, `TestServeCancelDuringStartupIsClean`, `TestServeRestoresGlobals`, `TestServePresentOverStdio`. Las pruebas de serve también comprueban que solo el protocolo llega a la salida estándar.
- `internal/team`: `TestBrainScenarioIsFrontEnd`, `TestGraphToolsSendKeyOnlyToManager`, `TestStartLocalWithHonoursCancel`, `TestManagerAPIKeyProtectsManager`, `TestOfflineRunPOUsesBackendTools`, `TestPresentToolRollCall`, `TestPresentAbsentWhenWorkerDown`, `TestPresentAbsentAfterLeaseExpired`, `TestPresentExtraWorkerNotCounted`, `TestPresentPromptListed`, `TestBrainRollCall`, `TestRegisterCarriesModel`, `TestQAPromptCarriesSpec`, `TestQAOutputTracesCriteria`, `TestDossierDropsUnsafeAppURL`, `TestSkillIncludesReferences`, `TestSkillUnknownRole`, `TestNoArchitectInSkills`, `TestSkillSectionsHaveNoFrontmatter`, `TestManagerSkillHasNoFences`, `TestReservationIsAtomic`, `TestPreferenceOrder`, `TestLeaseExpiryRequeues`, `TestJobFailsAfterMaxAttempts`, `TestKnowledgeAndMemoryOverMCP`, `TestNormalizeModel`, `TestTeamDeliversOffline`, `TestBridgeQueueAndAskUnknown`, `TestBridgeToolSetAndProxy`, `TestStartLocalWithMiddleware`, `TestCastHasTwelveWorkers`, `TestDefaultModels`, `TestPinnedJobWaitsForItsWorker`, `TestPinnedJobFallsBackWhenLeaseExpired`, `TestJointReviewNeedsBothApprovals`, `TestRejectedWorkReturnsToSameDeveloper`, `TestPlanParsesPreferField`, `TestThreadScopeFailsClosedOnAmbiguity`, `TestPlanValidatesPrefer`, `TestPlanNormalisesComplexity`, `TestTaskBlockedAfterMaxRounds`, `TestScopeRoundTripOverHTTP`, `TestSubmitStoresScope`, `TestToolSeesJobScope`, `TestScopeNeverResolvedByPrefix`, `TestScopeIdCollision`, `TestPOSpecWithoutEvidenceIsMarkedUnverified`, `TestVerifiedSpec`, `TestHubCountsCallsAndSetScopeResets`, `TestCallsRoundTripOverHTTP`, `TestEvidenceRuleWorksAcrossServices`.
- `internal/backend`: `TestReadRefusesOutsideRoots`, `TestReadWithinRoot`, `TestGrepStaysInsideRoot`, `TestGrepLimits`, `TestRootsResolveAgainstWorkspace`, `TestGetRefusesNonGetAndUnnamedHost`, `TestGetRefusesRedirectToOtherHost`, `TestGetRefusesLoopbackUnlessNamed`, `TestGetNamedLocalhost`, `TestGetBodyCapped`, `TestNoScopeMeansNoAccess`, `TestReportsGuardedCallsPerRequest`, `TestReportFailureDoesNotFailTheCall`.
- `internal/qa`: `TestBrowserTestNotRunWithoutCommand`, `TestBrowserTestNotRunWithoutAppURL`, `TestBrowserTestRejectsNonHTTPAppURL`, `TestBrowserTestRunsRunnerWithScopeURL`, `TestCommandRunnerUsesNoShell`, `TestCommandRunnerCapsOutput`, `TestCommandRunnerMinimalEnv`, `TestCommandRunnerTimeoutKillsProcessGroup`, `TestToolNotGivenToOtherRoles`.
- `internal/tasktrack`: `TestTaskFlowAllowsRework`, `TestIllegalMoveIsRejectedAndNotRecorded`, `TestWebhookSinksAndFilter`, `TestNotifierFailureDoesNotBlock`, `TestFromEnvAndEmailMessage`.
- `skills`: `TestEveryLockEntryIsEmbedded`, `TestVendoredSkillWithinBudget`, `TestEveryLockEntryHasLicense`, `TestEveryRoleDirHasOwnCopies`, `TestNoticesCoverLockedLicensesAndSources`, `TestPinnedFromIsPlausibleCommit`.

Las pruebas de serve que activan el equipo sin conexión (`TestServeListsTeamTools`, `TestServeSubmitCompletesOffline`, `TestServeStopsOnEOF`) se omiten cuando algún rol tiene especificación de modelo (hay una clave o `AGENT_MODEL*` definida).

## Cómo probarlo en modo de producción

1. Registre el servidor (vea arriba) con `ANTHROPIC_API_KEY` (u `OPENAI_API_KEY`). En el log del servidor en stderr, cada worker imprime `up on ... (model ...)`; compruebe que los modelos son los que definió (`AGENT_MODEL_<ROL>` sobrescribe los valores por defecto).
2. Envíe una demanda por el chat MCP con `team_submit`, con `backend_roots` y/o `backend_urls` apuntando a un backend real (y `app_url` para la ejecución en el navegador). Consulte con `team_status` hasta que `report` esté presente.
3. En el `history` de `team_status`, busque un rechazo de la revisión conjunta (`review returned by ... -> <desarrollador>`) y un rechazo del QA (`qa returned by ... -> <desarrollador>`). La línea siguiente de esa tarea debe mostrar al mismo desarrollador construyéndola de nuevo (`<tarea>: <desarrollador> -> review`). Los modelos no siempre rechazan; si ninguno rechazó, envíe una demanda con una laguna deliberada.
4. Compruebe la spec del PO: llame a `graph_state` con el `thread_id` y lea `values.spec`. Debe contener un bloque `EVIDENCE:` en el que cada endpoint coincida con el backend al que apuntó, y los ítems abiertos no deben contener `UNVERIFIED: no backend evidence`. Ese marcador significa que el PO no leyó el backend.
5. Pruebe una demanda que nombre un host al que el PO no debería llegar, y una URL que redirija a otro host. Ambas deben rechazarse en la salida de la herramienta.
6. Sin `PLAYWRIGHT_CMD`, o sin `app_url`, el resultado de `browser_test` del QA debe decir `NOT RUN` con el motivo. Con ambas definidas (y `PLAYWRIGHT_BROWSERS_PATH`, y Chrome instalado donde corre el servidor), debe mostrar la salida de Playwright.
7. Mantenga `WORKSPACE_ROOT` y las raíces de backend tan estrechas como el trabajo lo permita: las herramientas de backend leen archivos en el host que ejecuta el servidor. En modo serve ningún rol tiene shell. Si ejecuta la demostración con un modelo real, mantenga ajustada la lista de comandos permitidos del shell (`ls`, `grep` y `echo`); el shell no tiene sandbox.

## Véase también

- [full_team](../full_team/README.es-ES.md): el equipo copiado, con la capa de compresión documentada por completo
- [independent_engineering_team](../independent_engineering_team/README.es-ES.md): el mismo equipo dividido en servicios, con Docker compose, y los fragmentos de registro de cada cliente MCP
- [compress_shell_rtk](../compress_shell_rtk/README.es-ES.md), [compress_toon](../compress_toon/README.es-ES.md), [compress_ponytail](../compress_ponytail/README.es-ES.md)
