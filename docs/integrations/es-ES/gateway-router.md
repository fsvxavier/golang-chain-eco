# Usando el router con varios gateways

[English](../en-US/gateway-router.md) | [Português](../pt-BR/gateway-router.md) | **Español**

`router:<model>` envía cada llamada de modelo a una lista ordenada de targets y pasa al siguiente cuando un target falla con un error que vale la pena reintentar. Este manual muestra cómo conectar freellmapi, omniroute, bifrost, Anthropic (Claude) y OpenAI en un solo `.env`, cómo ejecutar cada modo y cómo usarlo en los ejemplos de equipo de ingeniería. La referencia de variables no se repite aquí: vea la [sección Router de PROVIDERS.md](../../es-ES/PROVIDERS.md#router) y el [capítulo 3.6.1 del manual](../../es-ES/manual/03-modelos-e-provedores.md).

Alcance: el router es una biblioteca Go que se usa con `chatmodels.Init("router:<model>")`. No es un servidor, así que VS Code, Cursor o una consola no pueden apuntar a él; esos clientes se apuntan a un gateway (vea [clients-mcp.md](clients-mcp.md#apuntando-un-cliente-a-un-gateway)).

## Los gateways

Cada gateway es un endpoint HTTP compatible con OpenAI, así que el router lo trata como un target con su nombre y lee `<NOMBRE>_BASE_URL` (y, opcionalmente, `<NOMBRE>_API_KEY`).

| Gateway | Base URL para el router | Clave | Fuente |
|---|---|---|---|
| freellmapi | `http://localhost:3001/v1` (puerto por defecto 3001) | la clave unificada que muestra su dashboard, empieza con `freellmapi-`; el modelo `auto` le deja elegir | https://github.com/tashfeenahmed/freellmapi |
| omniroute | `http://localhost:20128/v1` (puerto por defecto 20128) | la clave del Dashboard, Endpoints; modelo `auto`, `auto/<variante>` o `provider/model` | https://github.com/diegosouzapw/OmniRoute |
| bifrost | `http://localhost:8080/v1` (puerto por defecto 8080; `/v1/chat/completions` es la ruta documentada) | check bifrost docs para virtual keys (el quickstart no envía clave); modelo como `provider/model`, por ejemplo `openai/gpt-4o-mini` | https://docs.getbifrost.ai/quickstart/gateway/setting-up |
| Anthropic (nativo) | integrado | `ANTHROPIC_API_KEY` | - |
| OpenAI (nativo) | integrado | `OPENAI_API_KEY` | - |

Los puertos son los valores por defecto que documentan esos proyectos; si los cambió, use los suyos. El router añade las rutas OpenAI a la base URL, así que conserve el `/v1`.

Los nombres de modelo varían por gateway (`auto` en freellmapi, `provider/model` en bifrost). Para eso existe `ROUTER_MODEL_<NOMBRE>`.

## Un `.env` con los cinco targets

```bash
# .env
ROUTER_TARGETS=freellmapi,omniroute,bifrost,anthropic,openai
ROUTER_MODE=failover
ROUTER_TIMEOUT=60s

FREELLMAPI_BASE_URL=http://localhost:3001/v1
FREELLMAPI_API_KEY=freellmapi-...
ROUTER_MODEL_FREELLMAPI=auto

OMNIROUTE_BASE_URL=http://localhost:20128/v1
OMNIROUTE_API_KEY=...
ROUTER_MODEL_OMNIROUTE=auto

BIFROST_BASE_URL=http://localhost:8080/v1
# BIFROST_API_KEY=            # only if your bifrost requires one: check bifrost docs
ROUTER_MODEL_BIFROST=openai/gpt-4o-mini

ANTHROPIC_API_KEY=sk-ant-...
ROUTER_MODEL_ANTHROPIC=claude-sonnet-4-5
OPENAI_API_KEY=sk-...
ROUTER_MODEL_OPENAI=gpt-4o-mini
```

```go
m, err := chatmodels.Init("router:gpt-4o-mini") // the model after "router:" is used by every target without a ROUTER_MODEL_<NAME>
```

`Init` falla de inmediato, antes de cualquier llamada, cuando un target está incompleto: `router target "bifrost" needs $BIFROST_BASE_URL`, `router target "anthropic" needs $ANTHROPIC_API_KEY` o `router target "x" has no model; set ROUTER_MODEL_X or the model after router:`.

## Un ejemplo por modo

`ROUTER_MODE` solo decide qué target se prueba primero en cada llamada; los demás siguen en el orden de `ROUTER_TARGETS` si falla.

```bash
# failover (default): always start with freellmapi, fall back down the list
ROUTER_MODE=failover

# round_robin: rotate the first target on every call (the counter lives in one Init call)
ROUTER_MODE=round_robin

# random: a random first target on every call
ROUTER_MODE=random

# weighted: 70% of calls start with freellmapi, 30% with omniroute, bifrost never first
ROUTER_MODE=weighted
ROUTER_WEIGHTS=freellmapi:7,omniroute:3,bifrost:0,anthropic:0,openai:0
```

Un target ausente de `ROUTER_WEIGHTS` pesa 1; peso 0 significa nunca primero (aun así recibe la llamada cuando los anteriores fallan).

## Modelos por rol en el equipo de ingeniería

Dos ejemplos, dos formas distintas en que el entorno llega al código.

**`examples/langgraph/independent_engineering_team`** (hub, manager y un contenedor por worker). Cada rol lee `AGENT_MODEL_<ROL>`, luego `AGENT_MODEL`, luego los valores por defecto según la clave, y pasa el valor a `chatmodels.Init`, así que `router:` funciona. Roles: `MANAGER PO ARCHITECT LEADER JUNIOR MID SENIOR SPECIALIST QA KNOWLEDGE`. El `.env.example` del ejemplo se copia a `.env`, y cada servicio de `docker-compose.yml` tiene `env_file: [{ path: .env, required: false }]`, así que todos los contenedores ven el mismo archivo, incluidas las variables `ROUTER_*`, `*_BASE_URL` y las claves.

```bash
cp examples/langgraph/independent_engineering_team/.env.example examples/langgraph/independent_engineering_team/.env
# append to that .env: the block from the previous section, plus
AGENT_MODEL=router:gpt-4o-mini
AGENT_MODEL_PO=router:gpt-4o
AGENT_MODEL_ARCHITECT=router:gpt-4o
AGENT_MODEL_JUNIOR=router:gpt-4o-mini
AGENT_MODEL_QA=router:gpt-4o-mini
```

```bash
docker compose -f examples/langgraph/independent_engineering_team/docker-compose.yml up --build
```

Qué saber antes de usarlo:

- Un gateway en el host no es `localhost` dentro de un contenedor. Use `http://host.docker.internal:PORT/v1` (Docker Desktop; en Linux agregue `extra_hosts: ["host.docker.internal:host-gateway"]` a los servicios) o ejecute el gateway como otro servicio de compose. Revise su Docker; el ejemplo no configura esto.
- La cadena de modelo del rol pasa por el `normalize` del ejemplo, que cambia `_` por `-` en el modelo y la primera `/` por `:`. `router:gpt-4o-mini` es seguro. Un modelo como `router:openai/gpt-4o-mini` se convertiría en `router:openai:gpt-4o-mini`; ponga los nombres con `/` o `_` en `ROUTER_MODEL_<NOMBRE>`, que no se normaliza.
- Todos los roles comparten un único conjunto de variables `ROUTER_*`, así que comparten targets y modo. Para dar a un rol otro conjunto de targets harían falta procesos separados con entornos separados; el ejemplo no lo hace.
- `AGENT_MODEL_<ROL>=router:...` cuenta como definido, así que reemplaza el modelo offline de respuestas predefinidas; no hace falta `ANTHROPIC_API_KEY` si todos los targets del router son gateways.

**`examples/langgraph/engineering_team`** (un proceso). No lee `AGENT_MODEL_*`: usa `examples/internal/demo`, que elige `anthropic:claude-sonnet-4-5` cuando `ANTHROPIC_API_KEY` está definida, `openai:gpt-4o-mini` cuando `OPENAI_API_KEY` está definida, y si no el modelo de respuestas predefinidas. Un modelo atiende a todo el equipo, no hay mapeo por rol y el router no está conectado. Use `independent_engineering_team` para el router. (Para tener el router aquí habría que cambiar `demo.Smart`; es un cambio de código, no de configuración.)

La CLI `knowledge` y el hub también aceptan el router: `KNOWLEDGE_MODEL=router:gpt-4o-mini` (lo lee `cmd/knowledge`), `AGENT_MODEL_KNOWLEDGE` en el ejemplo independiente.

## Qué target respondió

Nada en el código lo informa cuando hay éxito. El router no escribe logs, no pone ningún campo en la respuesta y `ModelID()` siempre devuelve `router:<model>`. Lo que se puede observar:

- Cuando todos los targets fallan, el error lista cada uno: `router: all targets failed: freellmapi: <error>; omniroute: <error>; ...`.
- Los logs y dashboards de los propios gateways (freellmapi, omniroute y bifrost tienen dashboard o registro de peticiones; vea cómo en sus docs) muestran qué peticiones llegaron.
- Con `ROUTER_MODE=failover`, una llamada que llega al segundo target significa que el primero falló, pero el router no lo dice. Si necesita un registro por llamada, hoy el camino es el log de un gateway o una prueba de timeout/error contra un gateway falso.

## Qué pasa con el contexto en un cambio

- **Entre llamadas y en el failover.** Cada target recibe la misma lista completa de mensajes y opciones, así que el historial de la conversación nunca se pierde en un cambio: al siguiente target simplemente se le vuelve a enviar el historial completo. Las definiciones de herramientas viajan igual.
- **Streaming, fallo antes de cualquier texto.** El siguiente target recibe los mensajes originales; quien llama ve un stream normal.
- **Streaming, fallo después de algo de texto (sin tool call).** El siguiente target recibe los mensajes originales, el texto ya emitido como mensaje del asistente y un mensaje del usuario pidiendo que continúe. Quien llama ve una respuesta ininterrumpida. Qué tan fluida resulta depende del modelo; puede repetir o reformular.
- **Streaming, fallo después de una tool call parcial.** No se puede continuar: el error se devuelve a quien llama.
- **Timeout de inactividad.** En `Stream`, `ROUTER_TIMEOUT` es un timeout de inactividad (tiempo hasta el primer fragmento, luego el intervalo entre fragmentos), así que una respuesta larga que sigue fluyendo no se corta. En `Generate` limita la llamada completa.
- Distintos targets usan distintos modelos, así que el tono, la calidad del tool calling y el tamaño de contexto pueden cambiar tras un cambio. Mantenga modelos de la misma clase y compruebe que todos soportan tool calling si sus agentes usan herramientas.

## Solución de problemas

| Síntoma | Causa y solución |
|---|---|
| `401` o `403` devuelto de inmediato, sin failover | Son finales por diseño: el siguiente target no arreglaría una clave mala. Corrija la clave del target que cita el error. 400, 404 y otros 4xx también son finales. |
| `router: all targets failed: a: ...; b: ...` | El error de cada target se lista en orden. Típicos: conexión rechazada (gateway detenido o puerto equivocado), 429, 5xx. |
| `router target "bifrost" needs $BIFROST_BASE_URL` en `Init` | Un nombre de target sin proveedor nativo necesita `<NOMBRE>_BASE_URL`; el nombre pasa a mayúsculas y lo que no es alfanumérico se vuelve `_` (`my-gw` -> `MY_GW_BASE_URL`). |
| `set ROUTER_TARGETS to an ordered, comma-separated list` | `ROUTER_TARGETS` está vacío o no se exportó a este proceso (en Docker, revise el `env_file`). |
| `ROUTER_WEIGHTS: "x" is not in ROUTER_TARGETS` | Los pesos deben nombrar targets existentes. |
| Funciona en el host, falla en el contenedor | `localhost` dentro del contenedor es el propio contenedor. Vea la nota sobre `host.docker.internal` arriba. |
| Un gateway rechaza el nombre del modelo | Defina `ROUTER_MODEL_<NOMBRE>` para ese target (vea la tabla de gateways). |
| Un target muy lento retiene todas las llamadas | Defina `ROUTER_TIMEOUT` (por ejemplo `30s`); un timeout es reintentable, así que se prueba el siguiente target. |
