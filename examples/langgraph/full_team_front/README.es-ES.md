# full_team_front

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Una copia de [full_team](../full_team/README.es-ES.md) en la que todo el equipo trabaja en front-end y experiencia de usuario. Funciona como servidor MCP: VS Code (u otro chat con MCP) envía una demanda, y el equipo la convierte en código de pantallas revisado y en pruebas. La demanda pasa por el product owner, el equipo de ingeniería, el desarrollador, la revisión conjunta (especialista y tech lead) y QA. Cualquier problema que se encuentre en la revisión o en QA vuelve al desarrollador que escribió el código.

Sin conexión, funciona con modelos falsos y un backend falso, así que todo el flujo corre sin clave. Esta carpeta es autocontenida: el código del equipo se copió, no se importó, porque Go no permite importar el paquete `internal/` de otro árbol.

## Qué hace

- **Equipo y modelos.** Doce workers, un proceso cada uno cuando se ejecutan como servicios. Cada rol tiene un modelo; los valores por defecto aplican cuando solo está definida `ANTHROPIC_API_KEY`.

  | Worker | Cantidad | Modelo | Función |
  |---|---|---|---|
  | `product-owner` (`po`) | 1 | `claude-opus-5-5` | lee el backend, escribe la especificación |
  | `leader` (tech lead) y `manager` | 1 cada uno | `claude-sonnet-5-5` | planifica las tareas, las asigna, participa en la revisión |
  | `software-engineer-junior` | 2 | `claude-haiku-5-5` | escribe el código de las pantallas y las pruebas unitarias |
  | `software-engineer-mid` | 2 | `claude-haiku-5-5` | lo mismo |
  | `software-engineer-senior` | 2 | `claude-haiku-5-5` | lo mismo |
  | `specialist-reviewer` | 2 | `claude-sonnet-5-5` | revisa el código junto con el tech lead |
  | `qa-engineer` | 2 | `claude-sonnet-5-5` | escribe las pruebas automatizadas de las pantallas |

  Cambia el modelo de un rol con `AGENT_MODEL_<ROLE>`, por ejemplo `AGENT_MODEL_PO=anthropic:opus_5_5` o `AGENT_MODEL_QA=openai:gpt-4o-mini`. `AGENT_MODEL` define un modelo para todos los roles. Sin ninguna clave, la ejecución es sin conexión.

- **Flujo.** El PO escribe la especificación a partir de la demanda y del backend que lee. El leader planifica las tareas y propone un desarrollador preferido para cada una; la asignación tiene en cuenta la carga actual de cada worker. El desarrollador escribe el código de las pantallas y las pruebas unitarias. Después se ejecuta la **revisión conjunta**: el especialista y el leader ven la misma entrega, y la tarea solo pasa cuando ambos aprueban. Luego QA escribe las pruebas automatizadas de las pantallas, trazadas a la especificación del PO, y aprueba cuando cubren la especificación.

- **La corrección vuelve al mismo desarrollador.** Cuando la revisión conjunta o QA rechaza una tarea, el feedback va al desarrollador que la escribió, en el mismo hilo. Ese job de corrección queda fijado: espera a ese desarrollador en lugar de pasar a otro. Si el lease del desarrollador expiró, el job pasa a un desarrollador libre del mismo nivel. Una tarea rechazada en `MaxRounds` (3) rondas termina `blocked`.

- **El PO lee el backend nombrado en la demanda.** El PO tiene tres herramientas de solo lectura, y solo él las tiene:
  - `backend_read`: lee un archivo. La ruta debe estar bajo una raíz nombrada en la demanda y bajo `WORKSPACE_ROOT`. Se rechazan `..`, las rutas absolutas fuera de las raíces y los symlinks que salen de ellas. El tamaño está limitado.
  - `backend_grep`: busca bajo las mismas raíces.
  - `backend_get`: solo HTTP `GET`; no hay argumento de método. El host debe ser el host de una URL nombrada en la demanda. Solo http y https. Se rechaza una redirección a otro host. Los destinos loopback y link-local se rechazan, salvo que la demanda los nombre. El cuerpo está limitado y la petición tiene timeout.

  Las raíces y las URL solo vienen de los campos estructurados de `team_submit` (`backend_roots`, `backend_urls`), nunca de texto libre de la petición. `WORKSPACE_ROOT` es la raíz del workspace; por defecto es el directorio de trabajo. Una demanda sin raíces ni URL no da ningún acceso al PO.

- **La especificación.** El PO lista los endpoints que leyó, los payloads de petición y respuesta, los campos obligatorios, los errores y los estados de la interfaz (cargando, vacío, error). Todo payload descrito debe venir de algo que leyó. La especificación tiene un bloque `EVIDENCE:` (archivo y línea, o URL, para cada payload) y un bloque `OPEN ITEMS:` para lo que no pudo confirmar. El PO no se bloquea por los puntos abiertos y no inventa payloads. Si la demanda nombra un backend y el PO no llamó a ninguna herramienta de backend, o la especificación no tiene bloque `EVIDENCE:`, el manager añade `UNVERIFIED: no backend evidence` a los puntos abiertos.

- **QA escribe las pruebas.** QA escribe un conjunto de pruebas por tarea, para las pantallas que construyeron los desarrolladores, a partir de la especificación del PO. Cada prueba recibe el nombre del criterio o estado de interfaz que cubre, y una línea `TRACE:` vincula pruebas con criterios. QA rechaza al desarrollador cuando el código de la pantalla no puede cumplir un punto de la especificación (un estado incorrecto, un campo que falta, un selector o un problema de accesibilidad).

- **Ejecución opcional en el navegador (`browser_test`).** QA puede ejecutar su especificación de Playwright en Chrome. El comando lo fija el operador en `PLAYWRIGHT_CMD` (por ejemplo `npx playwright test --project=chromium`) y se ejecuta sin shell. Corre contra el `app_url` nombrado en la demanda. Si falta cualquiera de los dos, la herramienta informa `NOT RUN` con el motivo, y QA sigue escribiendo las pruebas. Sin conexión devuelve un informe con guion.

- **Skills por persona.** Cada rol tiene su propia persona (front-end y UX) y skills de referencia incorporadas (vendored) de repositorios públicos, en `skills/<role>/references/<name>/SKILL.md`. Se incrustan con `go:embed`, así que el ejemplo funciona en cualquier máquina. Cada skill incorporada aparece en `skills/skills.lock` con su origen, su licencia, el SHA-256 del archivo incrustado y si se recortó para caber en el presupuesto de tamaño. Una skill cuya licencia no permite la redistribución se descarta, no se parafrasea. Las skills están fijadas: nada las actualiza automáticamente.

- **Compresión.** Todas las llamadas al modelo de cada worker pasan por la misma capa de compresión que `full_team` (caveman en los turnos antiguos, Ponytail, TOON, RTK para comandos de shell). No se describe de nuevo aquí: véase [full_team](../full_team/README.es-ES.md#qué-hace).

- **Sin conexión.** El PO con guion llama a `backend_read`, `backend_grep` y `backend_get` contra un backend falso incrustado: un pequeño documento OpenAPI, un árbol de handlers y un servidor `httptest`. Así las guardas se ejecutan de verdad. El QA con guion escribe sus pruebas a partir de la especificación sin conexión.

## Cómo aplicarlo

- **Alcance de la demanda.** Pasa el backend en la demanda, no en el texto de la petición: `backend_roots` para árboles de código fuente, `backend_urls` para la API que el PO puede llamar, `app_url` para el front-end en ejecución que QA puede abrir.
- **Roles y modelos.** `anthropicDefaults` y `Cast` en `internal/team` guardan los valores por defecto. Cámbialos ahí y mantén el pool de doce workers; las pruebas verifican ese número.
- **Herramientas de backend.** Los límites (tamaño y timeout) están en la configuración de las herramientas de backend. Mantén `WORKSPACE_ROOT` tan restringido como permita el trabajo.
- **Skills.** Para cambiar una persona, edita su `SKILL.md`. Para añadir una skill incorporada, véase más abajo.
- **Shell.** La herramienta de shell se copió de `full_team`. Con clave de proveedor, el modelo solo puede ejecutar `ls`, `grep` y `echo`. No añadas intérpretes ni `find`.

### Actualizar una skill incorporada

1. Descarga el candidato en un directorio temporal, no en la carpeta global de skills: `npx skills add <owner/repo@skill> -y` ejecutado dentro de un proyecto temporal. No uses `-g`.
2. Lee su `SKILL.md` y su licencia (el archivo o la licencia del repositorio). Si la licencia no permite la redistribución, descarta la skill.
3. Copia el archivo a `skills/<role>/references/<name>/SKILL.md`. Si supera el presupuesto (unos 8 KB por skill, 24 KB por rol), recórtalo y pon `trimmed` en `true` en su entrada de `skills/skills.lock`.
4. Actualiza la entrada en `skills/skills.lock`: `source`, `skill`, `installs`, `hash` (SHA-256 del nuevo archivo), `license`, `trimmed`.
5. Ejecuta las pruebas de abajo. Comprueban que cada entrada del lock tiene su archivo incrustado, que el hash coincide y los presupuestos de tamaño.

## Cómo ejecutarlo

Sin conexión, un proceso:

```bash
go run ./examples/langgraph/full_team_front
```

La ejecución muestra los servicios que inició y el informe de entrega. Inicia el hub, el manager y los workers en loopback y los detiene al final.

Con un proveedor real (los modelos son los valores por defecto de arriba):

```bash
ANTHROPIC_API_KEY=... go run ./examples/langgraph/full_team_front
OPENAI_API_KEY=...    go run ./examples/langgraph/full_team_front   # openai:gpt-4o-mini para todos los roles
```

## Usarlo como servidor MCP en VS Code

El equipo lo sirve `Bridge.MCPServer` en `internal/team/bridge.go`, por stdio. Expone estas herramientas:

- `team_submit`: envía una demanda. Los campos son `request` y, opcionalmente, `backend_roots`, `backend_urls` y `app_url`. Devuelve `thread_id` y `run_id` al instante.
- `team_status`: los estados de las tareas, el informe y el historial de traspasos de un `thread_id`.
- `team_queue`: los workers registrados y los jobs en cola.
- `team_ask_worker`: hace una pregunta a un worker, fuera del flujo de entrega.
- `team_memory`: recupera lo que el equipo registró para una petición.
- Las herramientas genéricas de graph: `graph_run`, `graph_state` y `graph_resume`, para cualquier graph en un Agent Server.

El bridge habla con un equipo que ya está en marcha: `MANAGER_URL` es la API Agent Server del manager y `HUB_URL` es el hub. El `go run` de un solo proceso de arriba inicia los servicios solo durante la ejecución, así que no puede ser el servidor al que se conecta VS Code.

Registra el bridge en `.vscode/mcp.json` (el formato de VS Code usa `servers`):

```json
{
  "servers": {
    "front-team": {
      "type": "stdio",
      "command": "/abs/path/to/bridge",
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000"
      }
    }
  }
}
```

Luego abre el archivo y haz clic en **Start** sobre el servidor. En Copilot Chat elige **Agent** y habilita `front-team` en el selector de herramientas.

Envía una demanda con los campos de alcance. Este es un ejemplo de llamada `team_submit` (los argumentos en JSON; las rutas y URL son de ejemplo):

```json
{
  "request": "Añade un campo de cupón en la pantalla de checkout. Muestra un estado de carga mientras se valida el cupón, un estado vacío antes de escribir nada y un mensaje de error para un código inválido.",
  "backend_roots": ["/abs/path/to/backend"],
  "backend_urls": ["https://api.example.com/v1"],
  "app_url": "http://localhost:3000/checkout"
}
```

Después llama a `team_status` con el `thread_id` devuelto hasta que el estado sea `completed` o `blocked`. Las variables `PLAYWRIGHT_CMD` y `WORKSPACE_ROOT` las leen los procesos de los workers, donde corren las herramientas, no el bridge.

## Cómo probarlo

```bash
go test -race -count=1 ./examples/langgraph/full_team_front/...
```

Las pruebas cubren el pool y los modelos, la corrección fijada, la revisión conjunta y la ruta de rechazo de vuelta al mismo desarrollador, el alcance de una demanda, las guardas del backend del PO, la regla de evidencia, las pruebas de QA y los rechazos de `browser_test`, los archivos de persona y skill, y la ejecución sin conexión de extremo a extremo.

## Cómo probarlo en modo de producción

1. Ejecuta con `ANTHROPIC_API_KEY` (o `OPENAI_API_KEY`) y comprueba que los modelos del informe son los que definiste. `AGENT_MODEL_<ROLE>` reemplaza los valores por defecto.
2. Envía una demanda con `backend_roots` o `backend_urls`. Comprueba que la especificación tiene un bloque `EVIDENCE:` y que cada endpoint coincide con el backend al que apuntaste. Una especificación con `UNVERIFIED` en los puntos abiertos significa que el PO no leyó el backend.
3. Prueba una demanda que nombra un host que el PO no debería alcanzar, y una URL que redirige a otro host. Ambas deben rechazarse en la salida de la herramienta.
4. Ejecuta QA con `PLAYWRIGHT_CMD` y `app_url` definidos en el host de los workers, y comprueba que el informe muestra el resultado de Playwright, no `NOT RUN`. Confirma que Chrome está instalado donde corren los workers.
5. Mantén `WORKSPACE_ROOT` y las raíces del backend tan estrechas como permita el trabajo. Las herramientas de backend leen archivos en el host que ejecuta el worker del PO.
6. Mantén `AllowedCommands` ajustado: la herramienta de shell corre en el host, sin sandbox.

## Véase también

- [full_team](../full_team/README.es-ES.md): el equipo copiado, con la capa de compresión documentada completa
- [independent_engineering_team](../independent_engineering_team/README.es-ES.md): el mismo equipo dividido en servicios, con Docker compose
- [compress_shell_rtk](../compress_shell_rtk/README.es-ES.md), [compress_toon](../compress_toon/README.es-ES.md), [compress_ponytail](../compress_ponytail/README.es-ES.md)
