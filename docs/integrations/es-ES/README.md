# Manuales de integración

[English](../en-US/README.md) | [Português](../pt-BR/README.md) | **Español**

Cómo conectar este proyecto a las herramientas que ya usa: asistentes de código en el editor, consolas de terminal y los gateways de LLM que se sitúan delante de los modelos.

| Manual | Qué cubre |
|---|---|
| [gateway-router.md](gateway-router.md) | Usar `router:<model>` para repartir llamadas entre freellmapi, omniroute, bifrost, Anthropic y OpenAI desde un solo `.env`, incluidos los modelos por rol en los ejemplos de equipo de ingeniería. |
| [clients-mcp.md](clients-mcp.md) | Conectar `ciam mcp` y `knowledge mcp` (MCP por stdio) a VS Code con Copilot Chat, Claude Code, Kilo Code, Copilot CLI, Opencode y Cursor, y apuntar cada cliente a un gateway. |

Documentos de referencia en los que se apoyan estos manuales (no se repiten aquí): la [sección Router de PROVIDERS.md](../../es-ES/PROVIDERS.md#router), el [capítulo 3.6.1 del manual](../../es-ES/manual/03-modelos-e-provedores.md), [CIAM.md](../../es-ES/CIAM.md) y [KNOWLEDGE.md](../../es-ES/KNOWLEDGE.md).

## Qué funciona hoy y qué está planificado

| Capacidad | Estado |
|---|---|
| Código Go (y los ejemplos) llamando a varios gateways y proveedores con `router:<model>`, con los modos failover, round robin, random y weighted | Funciona hoy |
| Un `.env` que controla el router de todo el proceso; un modelo por rol en `independent_engineering_team` | Funciona hoy |
| `ciam mcp` y `knowledge mcp` como servidores MCP por stdio para cualquier cliente MCP | Funciona hoy (verificado ejecutando los servidores; vea las notas por cliente en [clients-mcp.md](clients-mcp.md) para lo que se confirmó o no) |
| `ciam init --editor vscode\|cursor\|claude` escribe la entrada MCP de VS Code, Cursor y Claude Code | Funciona hoy |
| Apuntar un editor o consola a un gateway (freellmapi, omniroute, bifrost) | Se hace con los ajustes de cada cliente, no con este proyecto; vea [clients-mcp.md](clients-mcp.md) |
| El router como servidor HTTP que los editores usarían como endpoint | No existe: el router es una biblioteca Go (`providers/router`), no un proceso proxy. Los clientes hablan con un gateway; el código Go habla con el router |
| Qué target respondió a una llamada, como línea de log o campo de la respuesta | No existe: el router solo lo informa cuando todos los targets fallaron (vea [gateway-router.md](gateway-router.md#qué-target-respondió)) |
| Controlar el equipo de ingeniería desde un chat por MCP | Planificado; todavía no existe un puente MCP (vea la última sección de [clients-mcp.md](clients-mcp.md)) |

## Hechos que vienen de otros productos

Los nombres de archivos de configuración de los clientes, los puertos de los gateways y los nombres de variables pertenecen a esos productos y cambian entre versiones. Cada hecho de ese tipo en estos manuales va acompañado de la URL de la que se leyó (leído el 2026-10-21). Cuando un hecho no se pudo confirmar, el manual dice "check <producto> docs" en lugar de adivinar.
