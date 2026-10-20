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
