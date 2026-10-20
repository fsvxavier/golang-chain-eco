# 11. MCP, servidor y grafos remotos

[English](../../en-US/manual/11-mcp-remote-servidor.md) | [Português](../../pt-BR/manual/11-mcp-remote-servidor.md) | **Español**

## 11.1 MCP (Model Context Protocol)

El paquete `mcp` es un cliente MCP con transportes **stdio** (proceso hijo) y **HTTP** (streamable HTTP). Convierte las tools de servidores MCP en `tools.Tool`, utilizables en cualquier agent. Eso da acceso a miles de integraciones que ya existen en el ecosistema MCP.

### Varios servidores a la vez

```go
import "github.com/fsvxavier/golang-chain-eco/mcp"

ts, closeAll, err := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"fs":   {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
	"docs": {URL: "https://mcp.ejemplo.com/mcp", Headers: map[string]string{"Authorization": "Bearer ..."}},
})
if err != nil {
	log.Fatal(err)
}
defer closeAll()

agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

- Las tools reciben el prefijo `<servidor>_` (`fs_read_file`, `docs_search`...).
- `ServerConfig` tiene dos modos: stdio (`Command`, `Args`, `Env`) o HTTP (`URL`, `Headers`).
- `closeAll()` cierra todas las sesiones y procesos hijos.

### Uso de bajo nivel

```go
tr, err := mcp.Stdio(ctx, "npx", []string{"-y", "mi-servidor-mcp"}, nil) // o mcp.HTTP(url, headers)
client, err := mcp.Connect(ctx, tr)
defer client.Close()

infos, err := client.ListTools(ctx)                     // []mcp.ToolInfo{Name, Description, InputSchema}
out, err := client.CallTool(ctx, "search", map[string]any{"q": "go"})
ts, err := client.Tools(ctx, "mi")                      // como tools.Tool, con prefijo
```

`mcp.NewPipeTransport(w, r)` crea un transporte sobre pipes cualesquiera (útil en pruebas). Los errores JSON-RPC llegan como `*mcp.RPCError`.

> **Atención (seguridad):** los servidores stdio son **procesos que inicias tú, con tus permisos**. Ejecuta solo servidores confiables. La primera conexión stdio por proceso registra un aviso (`slog`); `ServerConfig{Quiet: true}` lo silencia (en `LoadTools`). Para servidores HTTP, trata el `Authorization` como secreto y prefiere HTTPS.

Ejemplo: `examples/deepagents/mcp_docs_agent`.

### Servir tus tools por MCP (`mcp/server`)

Lo inverso del cliente: expón cualquier `[]tools.Tool` a editores y otros agents (VS Code, Cursor, Claude Code...).

```go
import "github.com/fsvxavier/golang-chain-eco/mcp/server"

s := server.New("mis-tools", "1.0")
s.AddTool(tools.New("suma", "Suma dos números", func(ctx context.Context, a struct{ A, B int }) (any, error) { return a.A + a.B, nil })...)
s.AddPrompt(server.Prompt{Name: "revisar", Description: "...", Get: func(ctx context.Context, args map[string]string) ([]server.PromptMessage, error) { ... }})
s.OnCall = func(name string, args map[string]any, d time.Duration, err error) { /* métricas */ }

s.ServeStdio(ctx, os.Stdin, os.Stdout) // editores
http.Handle("/mcp", s.Handler())        // HTTP (POST JSON-RPC)
```

Un fallo de tool se convierte en resultado con `isError` (el modelo lo lee), nunca en error de protocolo. El `mcp.Client` de este proyecto conversa con este servidor (hay una prueba por pipes). Mira `knowledge.Toolkit` (cap. 20: `rag_answer`, `cag_answer`, `kag_query`, `knowledge_answer`) y `ciam` (cap. 18), que se sirven así.

## 11.2 Cliente del Agent Server (`remote.Client`)

Se conecta a un Agent Server (LangGraph Platform o al `remote/server` de este proyecto):

```go
c := remote.NewClient("https://mi-servidor.ejemplo.com")
// clave por LANGGRAPH_API_KEY (o LANGSMITH_API_KEY) o c.APIKey; se envía en el header x-api-key
c.Headers = map[string]string{"X-Tenant": "acme"} // headers extra
```

| Método | Hace |
|---|---|
| `CreateThread(ctx, metadata)` | Crea un thread y devuelve el ID |
| `GetState(ctx, threadID)` | Estado actual del thread (`ThreadState`) |
| `UpdateState(ctx, threadID, values, asNode)` | Edita el estado |
| `CreateRun(ctx, threadID, req)` | Dispara una run asíncrona |
| `GetRun`, `Join`, `CancelRun` | Consultar, esperar y cancelar |
| `Wait(ctx, threadID, req)` | Crea y espera el resultado |
| `Stream(ctx, threadID, req)` | Crea y devuelve eventos SSE (`StreamPart{Event, Data}`) |

`RunRequest` tiene `AssistantID`, `Input`, `Command` (reanudar, p. ej. `{"resume": v}`), `Config`, `Metadata`, `StreamMode` (predeterminado `["values"]`) y `Multitask` (`reject` predeterminado, `interrupt`, `rollback`, `enqueue`). Los fallos HTTP llegan como `*remote.APIError`.

## 11.3 `remote.Graph`: un grafo remoto con la API de uno local

```go
c := remote.NewClient("https://mi-servidor.ejemplo.com")
rg := &remote.Graph{Client: c, AssistantID: "mi-agent"}

ctx := runnables.WithConfig(ctx, graph.Thread("thread-1"))
out, err := rg.Invoke(ctx, graph.State{"messages": []map[string]any{{"role": "user", "content": "hola"}}})

for st, err := range rg.Stream(ctx, graph.Command{Resume: "sí"}) { // estados parciales; reanuda interrupts
	...
}
```

Diferencias a recordar: los mensajes viajan en JSON (`{"role","content"}`), y la ejecución ocurre en el servidor. Un `remote.Graph` puede usarse como `RemoteRunner` en `AsyncSubagents` (capítulo 10).

## 11.4 Servir tus grafos: `remote/server`

El paquete `remote/server` expone grafos compilados mediante la API de threads/runs, compatible con `remote.Client` y con clientes oficiales del Agent Server.

```go
import "github.com/fsvxavier/golang-chain-eco/remote/server"

srv := server.New(map[string]*graph.Compiled{
	"mi-agent": agent, // clave = assistant_id
})
srv.APIKey = os.Getenv("AGENT_SERVER_KEY") // si está definido, exige el header x-api-key

log.Fatal(http.ListenAndServe(":8123", srv))
```

Rutas:

| Método y ruta | Función |
|---|---|
| `GET /ok` | Health check |
| `POST /threads` | Crea un thread (`{"thread_id"}`) |
| `GET /threads/{id}/state` | Estado del thread |
| `POST /threads/{id}/runs` | Crea una run asíncrona |
| `POST /threads/{id}/runs/wait` | Crea y espera |
| `POST /threads/{id}/runs/stream` | Crea y transmite por SSE |
| `GET /threads/{id}/runs/{run}` | Estado de la run (`pending`, `running`, `success`, `error`, `interrupted`) |
| `GET /threads/{id}/runs/{run}/join` | Espera a que termine |
| `POST /threads/{id}/runs/{run}/cancel` | Cancela |

Para threads persistentes y HITL, compila los grafos con un checkpointer. Pon el servidor detrás de TLS y de un proxy con autenticación real en producción; `APIKey` es un mecanismo simple de clave compartida.

Ejemplo de extremo a extremo: `examples/deepagents/async_subagent_server`.

## 11.5 Protocolo AG-UI

`integrations/agui` sirve un grafo por SSE en el protocolo AG-UI, consumible por CopilotKit, assistant-ui y otros frontends.

```go
import "github.com/fsvxavier/golang-chain-eco/integrations/agui"

http.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any {
	return graph.State{"messages": lastUserMessage(in)} // convierte la solicitud en la entrada del grafo
}))
http.ListenAndServe(":8080", nil)
```

- `RunInput` trae `ThreadID`, `RunID`, `State`, `Messages` y `ForwardedProps`.
- El `ThreadID` se convierte en el `thread_id` del checkpoint (usa un checkpointer para conversaciones continuas).
- Los eventos del grafo (`messages`, `updates`, `values`) se traducen a eventos AG-UI: `RUN_STARTED`, `TEXT_MESSAGE_*`, `STEP_*`, `STATE_SNAPSHOT`, `RUN_FINISHED`/`RUN_ERROR`.
- Solo acepta `POST`.

Ejemplo: `examples/integrations/agui_server` (se ejecuta sin conexión e imprime los eventos).

Para la **API abierta del Agent Protocol** (agents, threads, runs, store y streaming tipado), mira el [capítulo 17](17-agent-protocol.md).

---

Anterior: [10. Deep Agents](10-deep-agents.md) · Siguiente: [12. Observabilidad](12-observabilidade.md) · [Índice del manual](README.md)
