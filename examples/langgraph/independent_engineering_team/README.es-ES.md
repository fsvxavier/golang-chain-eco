# independent_engineering_team

El flujo de `engineering_team` con cada agente como **servicio propio**: un proceso (o contenedor, o máquina) por agente, hablando por HTTP. Los workers suben y caen solos y toman jobs de una cola.

## Qué hace

| Servicio | Cant. | Función |
|---|---|---|
| `hub` | 1 | Registro con reserva atómica y leases, cola de jobs y un servidor MCP con RAG, CAG, KAG y la memoria del equipo. |
| `manager` | 1 | Software Engineer Manager. Recibe el pedido (API de Agent Server, `remote/server`), lo enruta, conduce el flujo y escribe el informe. Nunca escribe código (la ejecución falla si su respuesta trae un bloque de código). |
| `po`, `architect`, `leader` | 1 c/u | Especificación, riesgo y seguridad, plan de tareas con complejidad. |
| `junior-1`, `junior-2`, `mid-1`, `senior-1` | 4 | Implementan en paralelo. |
| `specialist-1`, `specialist-2` | 2 | Revisión: aprueba o devuelve. |
| `qa-1`, `qa-2` | 2 | QA: aprueba o devuelve. |

Cada worker es `cmd/worker` con `WORKER_ROLE`; su persona es `skills/<nombre>/SKILL.md`.

Cómo se hablan las piezas:

- **Pedido de entrada**: `remote.Graph` (cliente) -> `remote/server` del manager.
- **Trabajo de salida**: el manager envía un job al hub (`Prefer` = ids de workers o roles, en orden). El hub reserva un worker libre **atómicamente** (un lock; dos jobs nunca toman el mismo worker; el trabajo de alta complejidad nunca va a un junior) o deja el job en cola. El worker hace long-poll en `claim`, ejecuta y devuelve el resultado.
- **Agent Protocol**: cada worker sirve su agente por Agent Protocol (`agentprotocol.Server`). El job corre como un run en un hilo con el nombre de la tarea (`<pedido>-T3-dev`), así que la tarea devuelta vuelve al mismo ingeniero, que recuerda lo que escribió. Cualquier cliente Agent Protocol también puede llamar a un worker directamente.
- **Leases**: los workers envían heartbeat. Un worker callado por `HUB_LEASE_TTL` se descarta y su job vuelve al inicio de la cola (el job se abandona tras 3 entregas). El resultado tardío del worker descartado se rechaza (409): un job nunca se completa dos veces.
- **Contexto**: cada job lleva el dossier completo (pedido, spec, reglas del arquitecto) y el feedback recibido; el grafo del manager tiene checkpoint por pedido; la **memoria del equipo** (MCP `memory_put` / `memory_search`, por id del pedido) guarda la salida de cada agente para los siguientes.
- **Conocimiento vía MCP**: antes de responder, el worker consulta al hub con la técnica adecuada a su función y la añade al prompt en `## Knowledge`:

  | Función | Herramienta | Por qué |
  |---|---|---|
  | architect | `kag_query` | relaciones: qué protege qué, qué toca un pago |
  | ingenieros | `rag_answer` | recuperar estándares de código por similitud (vector + BM25) |
  | specialist, QA | `cag_answer` | checklists pequeños y estables, enteros en el contexto |
  | PO, leader | `knowledge_answer` | el enrutador elige RAG, CAG o KAG |

  Las mismas herramientas también se dan a los agentes, así que un modelo real puede llamarlas.

## Elige el modelo de cada agente

Define `AGENT_MODEL_<ROL>=proveedor:modelo` (roles: `MANAGER PO ARCHITECT LEADER JUNIOR MID SENIOR SPECIALIST QA KNOWLEDGE`). Los nombres de Anthropic pueden ser cortos: `anthropic:opus_5_5` es `anthropic:claude-opus-5-5`. Orden: `AGENT_MODEL_<ROL>`, luego `AGENT_MODEL`, luego valores por rol para la clave definida (Anthropic: Opus para PO y arquitecto, Sonnet para manager, leader, senior y specialist, Haiku para el resto; OpenAI: `gpt-4o-mini`), si no el modelo offline guionizado. Claves: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, ... mira `.env.example`. `KNOWLEDGE_EMBEDDINGS=proveedor:modelo` reemplaza los embeddings offline.

```bash
AGENT_MODEL_PO=anthropic:opus_5_5 AGENT_MODEL_JUNIOR=anthropic:haiku_5_5 AGENT_MODEL_SENIOR=openai:gpt-4o \
  ANTHROPIC_API_KEY=... OPENAI_API_KEY=... go run ./examples/langgraph/independent_engineering_team
```

## Cómo aplicarlo

- Edita una persona en `skills/<nombre>/SKILL.md`. Cada una define el formato de salida que lee el manager (`TASK|id|título|complejidad|desc`, `VERDICT:`, `RISK:`); cambia ambos a la vez.
- Cambia el conocimiento: `kb/*.md` alimenta el RAG (todos) y el CAG (los dos checklists); el grafo KAG es `triples` en `internal/team/knowledge.go`.
- Cambia el equipo: añade un worker en `docker-compose.yml` (`WORKER_ID`, `WORKER_ROLE`) y en `Cast` (`internal/team/local.go`). Nada más conoce el tamaño del equipo.
- Cambia el flujo: `internal/team/manager.go`.

## Ejecútalo

Un solo proceso, offline (cada servicio en su puerto loopback, HTTP real, modelo guionizado):

```bash
go run ./examples/langgraph/independent_engineering_team
go test -race ./examples/langgraph/independent_engineering_team/...   # reserva atómica, leases, conocimiento vía MCP, entrega completa
```

Servicios separados, en local o en Docker:

```bash
cp examples/langgraph/independent_engineering_team/.env.example examples/langgraph/independent_engineering_team/.env
F=examples/langgraph/independent_engineering_team/docker-compose.yml
docker compose -f $F up --build -d          # hub, manager y 11 workers; el orden no importa
docker compose -f $F run --rm client        # envía el pedido de ejemplo, imprime informe e historial
curl localhost:8000/state                   # workers, reservas, cola
docker compose -f $F stop senior-1          # tumba un worker a mitad: su job pasa a otro
docker compose -f $F down
```

¿Puertos del host ocupados? Usa `HUB_PORT` y `MANAGER_PORT`. Sin Docker: `go run ./examples/langgraph/independent_engineering_team/cmd/hub`, luego `cmd/manager` y un `cmd/worker` por rol (`WORKER_ID`, `WORKER_ROLE`, `HUB_URL`, `LISTEN`), luego `cmd/client`.

## Estado de las tareas y notificaciones

Toda tarea (y el pedido) tiene siempre uno de 13 estados: `received`, `in_specification`, `risk_sec_review`, `task_assignment`, `in_development`, `in_code_review`, `in_qa_testing`, `qa_approved`, `qa_rejected`, `review_approved`, `review_rejected`, `committed`, `completed`. El agente que tiene el trabajo se registra en cada movimiento.

```
request: received -> in_specification -> risk_sec_review -> task_assignment -> completed
task:    task_assignment -> in_development -> in_code_review -> review_approved -> in_qa_testing -> qa_approved -> committed -> completed
         in_code_review -> review_rejected -> in_development      in_qa_testing -> qa_rejected -> in_development
```

`examples/internal/tasktrack` valida cada movimiento contra esta tabla (una tarea no llega a `committed` sin revisión y QA; un movimiento ilegal se loguea y no se registra). Una tarea que agota las 3 rondas queda en el rechazo que la detuvo. Aquí `committed` significa que el código aprobado se registró como entrega de la tarea; una ejecución real hace commit antes en el sandbox.

Cada movimiento se anuncia por `NOTIFY` (por defecto `console`): `console`, `slack` (`SLACK_WEBHOOK_URL`), `email` (`SMTP_ADDR`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`, `MAIL_TO`) y `chat` (`CHAT_WEBHOOK_URL`, cualquier webhook que acepte `{"text": "...", "event": {...}}`), separados por coma. `NOTIFY_STATUSES=review_rejected,qa_rejected,completed` limita slack, email y chat a esos estados; la consola muestra todos. Un canal que falla se loguea y nunca bloquea el trabajo.

```bash
NOTIFY=console,slack SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... NOTIFY_STATUSES=qa_rejected,completed go run ./examples/langgraph/independent_engineering_team
```

## Pruébalo en modo producción

1. Pon claves reales en `.env` y ejecuta el cliente. La estructura se mantiene (todas las tareas `approved` o `blocked`); el texto y las rondas de revisión varían. Un error de parseo indica un modelo que ignoró el formato de una skill: refuerza esa skill.
2. Fija los modelos por agente como arriba y compara costo y calidad por rol.
3. Tumba un worker durante la ejecución y mira `curl localhost:8000/state`: tras `HUB_LEASE_TTL` el job cambia de manos y la ejecución termina.
4. El hub es una instancia con estado en memoria (nota `ponytail:` en `hub.go`). Para replicarlo, mantén las mismas transiciones como compare-and-swap en un store compartido. Cambia también `checkpoint.NewMemory()` por un saver durable y los stores en memoria por backends de `integrations/`.
5. Los ingenieros devuelven código como texto. Ejecútalo en un sandbox antes de la revisión (`kubernetes.Sandbox`, `examples/integrations/remote_sandbox`).
6. Define `MANAGER_API_KEY` y pon el hub en una red privada: hub y workers no tienen autenticación.

## Ver también

- `examples/langgraph/engineering_team`: el mismo flujo en un proceso
- [Manual, capítulo 7: LangGraph](../../../docs/es-ES/manual/07-langgraph.md), [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)

## Puente para chats (MCP)

`cmd/bridge` es un servidor MCP en stdio. Los chats y consolas lo inician solos y hablan con él por stdin/stdout, así que nada necesita escuchar en un puerto. Alcanza la stack en ejecución con tres variables.

### 1. Compila el servidor una vez

```bash
go build -o bin/bridge ./cmd/bridge
```

Usa un binario en lugar de `go run`: arranca al instante y no imprime nada más que el protocolo en stdout. Abajo, `/ruta/abs/a/bridge` es el archivo que acabas de compilar.

### 2. Variables

| Variable | Por defecto | Uso |
|---|---|---|
| `MANAGER_URL` | `http://localhost:8080` | manager (Agent Server API) |
| `MANAGER_API_KEY` | vacío | clave, cuando el manager define una |
| `HUB_URL` | `http://localhost:8000` | hub (cola, `GET /state`, memoria del equipo) |
| `BRIDGE_CIAM_CMD` | vacío | comando del servidor de contexto del proyecto, p. ej. `/abs/ruta/ciam mcp` |
| `BRIDGE_KNOWLEDGE_CMD` | vacío | comando del servidor de conocimiento, p. ej. `/abs/ruta/knowledge mcp --dir /abs/.knowledge` |

### 3. Herramientas

`team_submit` envía una demanda y devuelve el `thread_id` al instante; `team_status` muestra estados de las tareas, informe e historial; `team_queue` lista workers y cola; `team_ask_worker` pregunta a un worker vía Agent Protocol; `team_memory` lee la memoria del equipo.

Además del equipo, el mismo servidor ofrece:

- `graph_run`, `graph_state`, `graph_resume`: ejecutar cualquier grafo en cualquier Agent Server (`assistant_id`, `url` opcional, `wait`), leer estado e interrupciones de un thread y responder a una interrupción.
- las herramientas `ciam_*` (búsqueda en el proyecto, ADR/PRD/planes, memoria), cuando `BRIDGE_CIAM_CMD` está definido.
- las herramientas `knowledge_*` (RAG, CAG, KAG), cuando `BRIDGE_KNOWLEDGE_CMD` está definido. Ejecuta antes `knowledge index <path> --kind both`; sin índice ese servidor no arranca y el puente registra el error y sigue sin él.

Pon estas dos variables en el `env` del registro de abajo, con rutas absolutas (`go build -o bin/ciam ./cmd/ciam`, `go build -o bin/knowledge ./cmd/knowledge`).

### 4. Regístralo en cada aplicación

Pon los secretos (`MANAGER_API_KEY`) en el almacén de la propia herramienta o en el entorno del shell, no en un archivo versionado. Los formatos de configuración cambian entre versiones; si una clave se rechaza, revisa la documentación MCP de la herramienta.

#### GitHub Copilot (VS Code)

Requiere VS Code con Copilot Chat en modo Agent. Crea `.vscode/mcp.json` en el workspace (o ejecuta **MCP: Open User Configuration** para todos):

```json
{
  "inputs": [
    { "type": "promptString", "id": "manager-key", "description": "MANAGER_API_KEY", "password": true }
  ],
  "servers": {
    "team-bridge": {
      "type": "stdio",
      "command": "/ruta/abs/a/bridge",
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/ruta/abs/a/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/ruta/abs/a/knowledge mcp --dir /ruta/abs/a/.knowledge",
        "MANAGER_API_KEY": "${input:manager-key}"
      }
    }
  }
}
```

Abre el archivo y pulsa **Start** sobre el servidor (o ejecuta **MCP: List Servers**). En Copilot Chat elige **Agent**, abre el selector de herramientas y habilita `team-bridge`. La entrada `inputs` hace que VS Code pida la clave una vez y la mantenga fuera del archivo.

#### Claude Code

Línea de comandos (`--scope local` es el valor por defecto; `project` escribe `.mcp.json` para compartir con el equipo; `user` vale para todos los proyectos):

```bash
claude mcp add --transport stdio --scope project \
  --env MANAGER_URL=http://localhost:8080 --env HUB_URL=http://localhost:8000 \
  --env BRIDGE_CIAM_CMD="/ruta/abs/a/ciam mcp" \
  --env BRIDGE_KNOWLEDGE_CMD="/ruta/abs/a/knowledge mcp --dir /ruta/abs/a/.knowledge" \
  team-bridge -- /ruta/abs/a/bridge
```

O escribe `.mcp.json` en la raíz del proyecto a mano:

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/ruta/abs/a/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/ruta/abs/a/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/ruta/abs/a/knowledge mcp --dir /ruta/abs/a/.knowledge"
      }
    }
  }
}
```

Comprueba con `claude mcp list` o `/mcp` dentro de la sesión. Los servidores de ámbito proyecto piden aprobación la primera vez.

#### Cursor

Crea `.cursor/mcp.json` en el proyecto (o `~/.cursor/mcp.json` para todos los proyectos):

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/ruta/abs/a/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/ruta/abs/a/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/ruta/abs/a/knowledge mcp --dir /ruta/abs/a/.knowledge"
      }
    }
  }
}
```

Abre **Settings → Cursor Settings → MCP**, activa el servidor y espera el punto verde. Cursor lista ahí las herramientas; el agente las usa en modo Agent.

#### Kilo Code

Kilo Code lee `mcpServers` de `.kilocode/mcp.json` en el proyecto (configuración global: **MCP Servers → Edit Global MCP** en el panel de Kilo). Ojo: las versiones nuevas de Kilo, basadas en Opencode, leen `kilo.json` con el mismo formato que Opencode abajo; no se verificó aquí, así que usa lo que muestre la pantalla MCP de tu versión.

```json
{
  "mcpServers": {
    "team-bridge": {
      "command": "/ruta/abs/a/bridge",
      "args": [],
      "env": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/ruta/abs/a/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/ruta/abs/a/knowledge mcp --dir /ruta/abs/a/.knowledge"
      },
      "alwaysAllow": ["team_status", "team_queue", "team_memory"],
      "disabled": false
    }
  }
}
```

Abre el icono **MCP Servers** en el panel de Kilo para ver estado y herramientas. `alwaysAllow` deja que las herramientas listadas se ejecuten sin confirmación; omítelo para aprobar cada llamada.

#### Opencode

Crea `opencode.json` en el proyecto (o `~/.config/opencode/opencode.json` para todos los proyectos):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "team-bridge": {
      "type": "local",
      "command": ["/ruta/abs/a/bridge"],
      "environment": {
        "MANAGER_URL": "http://localhost:8080",
        "HUB_URL": "http://localhost:8000",
        "BRIDGE_CIAM_CMD": "/ruta/abs/a/ciam mcp",
        "BRIDGE_KNOWLEDGE_CMD": "/ruta/abs/a/knowledge mcp --dir /ruta/abs/a/.knowledge"
      },
      "enabled": true
    }
  }
}
```

Ejecuta `opencode mcp list` para comprobar. En la TUI las herramientas aparecen como `team-bridge_team_submit` y así sucesivamente.

### 5. Úsalo

Pide en el chat, por ejemplo: *"Usa team_submit para enviar: permitir cupón de descuento en el checkout"*, luego *"muestra el team_status de ese thread"* o *"¿quién está en team_queue?"*. Levanta la stack antes (`docker compose up`, ver arriba).
