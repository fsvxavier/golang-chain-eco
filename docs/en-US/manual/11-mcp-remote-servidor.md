# 11. MCP, server and remote graphs

**English** | [Português](../../pt-BR/manual/11-mcp-remote-servidor.md) | [Español](../../es-ES/manual/11-mcp-remote-servidor.md)

## 11.1 MCP (Model Context Protocol)

The `mcp` package is an MCP client with **stdio** (child process) and **HTTP** (streamable HTTP) transports. It turns the tools of MCP servers into `tools.Tool`, usable in any agent. This gives access to thousands of integrations that already exist in the MCP ecosystem.

### Several servers at once

```go
import "github.com/fsvxavier/golang-chain-eco/mcp"

ts, closeAll, err := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"fs":   {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
	"docs": {URL: "https://mcp.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ..."}},
})
if err != nil {
	log.Fatal(err)
}
defer closeAll()

agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

- The tools get the `<server>_` prefix (`fs_read_file`, `docs_search`...).
- `ServerConfig` has two modes: stdio (`Command`, `Args`, `Env`) or HTTP (`URL`, `Headers`).
- `closeAll()` closes all sessions and child processes.

### Low-level use

```go
tr, err := mcp.Stdio(ctx, "npx", []string{"-y", "my-mcp-server"}, nil) // or mcp.HTTP(url, headers)
client, err := mcp.Connect(ctx, tr)
defer client.Close()

infos, err := client.ListTools(ctx)                     // []mcp.ToolInfo{Name, Description, InputSchema}
out, err := client.CallTool(ctx, "search", map[string]any{"q": "go"})
ts, err := client.Tools(ctx, "my")                      // as tools.Tool, with prefix
```

`mcp.NewPipeTransport(w, r)` creates a transport over any pipes (useful in tests). JSON-RPC errors arrive as `*mcp.RPCError`.

> **Warning (security):** stdio servers are **processes you start, with your permissions**. Run only trusted servers. The first stdio connection per process logs a warning (`slog`); `ServerConfig{Quiet: true}` silences it (in `LoadTools`). For HTTP servers, treat `Authorization` as a secret and prefer HTTPS.

Example: `examples/deepagents/mcp_docs_agent`.

### Serving your tools over MCP (`mcp/server`)

The reverse of the client: expose any `[]tools.Tool` to editors and other agents (VS Code, Cursor, Claude Code...).

```go
import "github.com/fsvxavier/golang-chain-eco/mcp/server"

s := server.New("my-tools", "1.0")
s.AddTool(tools.New("sum", "Adds two numbers", func(ctx context.Context, a struct{ A, B int }) (any, error) { return a.A + a.B, nil })...)
s.AddPrompt(server.Prompt{Name: "review", Description: "...", Get: func(ctx context.Context, args map[string]string) ([]server.PromptMessage, error) { ... }})
s.OnCall = func(name string, args map[string]any, d time.Duration, err error) { /* metrics */ }

s.ServeStdio(ctx, os.Stdin, os.Stdout) // editors
http.Handle("/mcp", s.Handler())        // HTTP (POST JSON-RPC)
```

A tool failure becomes a result with `isError` (the model reads it), never a protocol error. This project's `mcp.Client` talks to this server (there is a pipe-based test). See `knowledge.Toolkit` (chap. 20: `rag_answer`, `cag_answer`, `kag_query`, `knowledge_answer`) and `ciam` (chap. 18), which are served this way.

## 11.2 Agent Server client (`remote.Client`)

Connects to an Agent Server (LangGraph Platform or this project's `remote/server`):

```go
c := remote.NewClient("https://my-server.example.com")
// key via LANGGRAPH_API_KEY (or LANGSMITH_API_KEY) or c.APIKey; sent in the x-api-key header
c.Headers = map[string]string{"X-Tenant": "acme"} // extra headers
```

| Method | Does |
|---|---|
| `CreateThread(ctx, metadata)` | Creates a thread and returns the ID |
| `GetState(ctx, threadID)` | Current state of the thread (`ThreadState`) |
| `UpdateState(ctx, threadID, values, asNode)` | Edits the state |
| `CreateRun(ctx, threadID, req)` | Starts an asynchronous run |
| `GetRun`, `Join`, `CancelRun` | Query, wait for and cancel |
| `Wait(ctx, threadID, req)` | Creates and waits for the result |
| `Stream(ctx, threadID, req)` | Creates and returns SSE events (`StreamPart{Event, Data}`) |

`RunRequest` has `AssistantID`, `Input`, `Command` (resume, e.g. `{"resume": v}`), `Config`, `Metadata`, `StreamMode` (default `["values"]`) and `Multitask` (`reject` default, `interrupt`, `rollback`, `enqueue`). HTTP failures come back as `*remote.APIError`.

## 11.3 `remote.Graph`: a remote graph with the API of a local one

```go
c := remote.NewClient("https://my-server.example.com")
rg := &remote.Graph{Client: c, AssistantID: "my-agent"}

ctx := runnables.WithConfig(ctx, graph.Thread("thread-1"))
out, err := rg.Invoke(ctx, graph.State{"messages": []map[string]any{{"role": "user", "content": "hi"}}})

for st, err := range rg.Stream(ctx, graph.Command{Resume: "yes"}) { // partial states; resumes interrupts
	...
}
```

Things to remember: messages travel as JSON (`{"role","content"}`), and execution happens on the server. A `remote.Graph` can be used as a `RemoteRunner` in `AsyncSubagents` (chapter 10).

## 11.4 Serving your graphs: `remote/server`

The `remote/server` package exposes compiled graphs through the threads/runs API, compatible with `remote.Client` and with official Agent Server clients.

```go
import "github.com/fsvxavier/golang-chain-eco/remote/server"

srv := server.New(map[string]*graph.Compiled{
	"my-agent": agent, // key = assistant_id
})
srv.APIKey = os.Getenv("AGENT_SERVER_KEY") // if set, requires the x-api-key header

log.Fatal(http.ListenAndServe(":8123", srv))
```

Routes:

| Method and path | Function |
|---|---|
| `GET /ok` | Health check |
| `POST /threads` | Creates a thread (`{"thread_id"}`) |
| `GET /threads/{id}/state` | Thread state |
| `POST /threads/{id}/runs` | Creates an asynchronous run |
| `POST /threads/{id}/runs/wait` | Creates and waits |
| `POST /threads/{id}/runs/stream` | Creates and streams over SSE |
| `GET /threads/{id}/runs/{run}` | Run status (`pending`, `running`, `success`, `error`, `interrupted`) |
| `GET /threads/{id}/runs/{run}/join` | Waits for completion |
| `POST /threads/{id}/runs/{run}/cancel` | Cancels |

For persistent threads and HITL, compile the graphs with a checkpointer. Put the server behind TLS and a proxy with real authentication in production; `APIKey` is a simple shared-key mechanism.

End-to-end example: `examples/deepagents/async_subagent_server`.

## 11.5 AG-UI protocol

`integrations/agui` serves a graph over SSE using the AG-UI protocol, consumable by CopilotKit, assistant-ui and other frontends.

```go
import "github.com/fsvxavier/golang-chain-eco/integrations/agui"

http.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any {
	return graph.State{"messages": lastUserMessage(in)} // converts the request into the graph input
}))
http.ListenAndServe(":8080", nil)
```

- `RunInput` carries `ThreadID`, `RunID`, `State`, `Messages` and `ForwardedProps`.
- The `ThreadID` becomes the checkpoint's `thread_id` (use a checkpointer for continuous conversations).
- The graph's events (`messages`, `updates`, `values`) are translated into AG-UI events: `RUN_STARTED`, `TEXT_MESSAGE_*`, `STEP_*`, `STATE_SNAPSHOT`, `RUN_FINISHED`/`RUN_ERROR`.
- Only accepts `POST`.

Example: `examples/integrations/agui_server` (runs offline and prints the events).

For the **open Agent Protocol API** (agents, threads, runs, store and typed streaming), see [chapter 17](17-agent-protocol.md).

---

Previous: [10. Deep Agents](10-deep-agents.md) · Next: [12. Observability](12-observabilidade.md) · [Manual index](README.md)
