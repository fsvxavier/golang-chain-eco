# 11. MCP, servidor e graphs remotos

## 11.1 MCP (Model Context Protocol)

O pacote `mcp` é um cliente MCP com transportes **stdio** (processo filho) e **HTTP** (streamable HTTP). Ele transforma as tools de servidores MCP em `tools.Tool`, usáveis em qualquer agent. Isso dá acesso a milhares de integrações já existentes no ecossistema MCP.

### Vários servidores de uma vez

```go
import "github.com/fsvxavier/golang-chain-eco/mcp"

ts, closeAll, err := mcp.LoadTools(ctx, map[string]mcp.ServerConfig{
	"fs":   {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}},
	"docs": {URL: "https://mcp.exemplo.com/mcp", Headers: map[string]string{"Authorization": "Bearer ..."}},
})
if err != nil {
	log.Fatal(err)
}
defer closeAll()

agent, _ := agents.Create(agents.Options{Model: m, Tools: ts})
```

- As tools ganham o prefixo `<servidor>_` (`fs_read_file`, `docs_search`...).
- `ServerConfig` tem dois modos: stdio (`Command`, `Args`, `Env`) ou HTTP (`URL`, `Headers`).
- `closeAll()` encerra todas as sessões e processos filhos.

### Uso de baixo nível

```go
tr, err := mcp.Stdio(ctx, "npx", []string{"-y", "meu-servidor-mcp"}, nil) // ou mcp.HTTP(url, headers)
client, err := mcp.Connect(ctx, tr)
defer client.Close()

infos, err := client.ListTools(ctx)                     // []mcp.ToolInfo{Name, Description, InputSchema}
out, err := client.CallTool(ctx, "search", map[string]any{"q": "go"})
ts, err := client.Tools(ctx, "meu")                     // como tools.Tool, com prefixo
```

`mcp.NewPipeTransport(w, r)` cria um transporte sobre pipes quaisquer (útil em testes). Erros JSON-RPC chegam como `*mcp.RPCError`.

> **Atenção (segurança):** servidores stdio são **processos iniciados por você, com as suas permissões**. Execute só servidores confiáveis. Para servidores HTTP, trate o `Authorization` como segredo e prefira HTTPS.

Exemplo: `examples/deepagents/mcp_docs_agent`.

## 11.2 Cliente do Agent Server (`remote.Client`)

Conecta a um Agent Server (LangGraph Platform ou ao `remote/server` deste projeto):

```go
c := remote.NewClient("https://meu-servidor.exemplo.com")
// chave via LANGGRAPH_API_KEY (ou LANGSMITH_API_KEY) ou c.APIKey; enviada no header x-api-key
c.Headers = map[string]string{"X-Tenant": "acme"} // headers extras
```

| Método | Faz |
|---|---|
| `CreateThread(ctx, metadata)` | Cria uma thread e devolve o ID |
| `GetState(ctx, threadID)` | Estado atual da thread (`ThreadState`) |
| `UpdateState(ctx, threadID, values, asNode)` | Edita o estado |
| `CreateRun(ctx, threadID, req)` | Dispara um run assíncrono |
| `GetRun`, `Join`, `CancelRun` | Consultar, aguardar e cancelar |
| `Wait(ctx, threadID, req)` | Cria e aguarda o resultado |
| `Stream(ctx, threadID, req)` | Cria e devolve eventos SSE (`StreamPart{Event, Data}`) |

`RunRequest` tem `AssistantID`, `Input`, `Command` (retomar, ex. `{"resume": v}`), `Config`, `Metadata`, `StreamMode` (padrão `["values"]`) e `Multitask` (`reject` padrão, `interrupt`, `rollback`, `enqueue`). Falhas HTTP voltam como `*remote.APIError`.

## 11.3 `remote.Graph`: um grafo remoto com a API de um local

```go
c := remote.NewClient("https://meu-servidor.exemplo.com")
rg := &remote.Graph{Client: c, AssistantID: "meu-agent"}

ctx := runnables.WithConfig(ctx, graph.Thread("thread-1"))
out, err := rg.Invoke(ctx, graph.State{"messages": []map[string]any{{"role": "user", "content": "oi"}}})

for st, err := range rg.Stream(ctx, graph.Command{Resume: "sim"}) { // estados parciais; retoma interrupts
	...
}
```

Diferenças a lembrar: as mensagens trafegam em JSON (`{"role","content"}`), e a execução acontece no servidor. Um `remote.Graph` pode ser usado como `RemoteRunner` em `AsyncSubagents` (capítulo 10).

## 11.4 Servir seus grafos: `remote/server`

O pacote `remote/server` expõe grafos compilados pela API de threads/runs, compatível com `remote.Client` e com clientes do Agent Server oficial.

```go
import "github.com/fsvxavier/golang-chain-eco/remote/server"

srv := server.New(map[string]*graph.Compiled{
	"meu-agent": agent, // chave = assistant_id
})
srv.APIKey = os.Getenv("AGENT_SERVER_KEY") // se definido, exige o header x-api-key

log.Fatal(http.ListenAndServe(":8123", srv))
```

Rotas:

| Método e caminho | Função |
|---|---|
| `GET /ok` | Health check |
| `POST /threads` | Cria thread (`{"thread_id"}`) |
| `GET /threads/{id}/state` | Estado da thread |
| `POST /threads/{id}/runs` | Cria run assíncrono |
| `POST /threads/{id}/runs/wait` | Cria e aguarda |
| `POST /threads/{id}/runs/stream` | Cria e transmite por SSE |
| `GET /threads/{id}/runs/{run}` | Status do run (`pending`, `running`, `success`, `error`, `interrupted`) |
| `GET /threads/{id}/runs/{run}/join` | Aguarda o término |
| `POST /threads/{id}/runs/{run}/cancel` | Cancela |

Para threads persistentes e HITL, compile os grafos com um checkpointer. Coloque o servidor atrás de TLS e de um proxy com autenticação real em produção; `APIKey` é um mecanismo simples de chave compartilhada.

Exemplo de ponta a ponta: `examples/deepagents/async_subagent_server`.

## 11.5 Protocolo AG-UI

`integrations/agui` serve um grafo por SSE no protocolo AG-UI, consumível por CopilotKit, assistant-ui e outros frontends.

```go
import "github.com/fsvxavier/golang-chain-eco/integrations/agui"

http.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any {
	return graph.State{"messages": lastUserMessage(in)} // converte a requisição na entrada do grafo
}))
http.ListenAndServe(":8080", nil)
```

- `RunInput` traz `ThreadID`, `RunID`, `State`, `Messages` e `ForwardedProps`.
- O `ThreadID` vira o `thread_id` do checkpoint (use um checkpointer para conversas contínuas).
- Os eventos do grafo (`messages`, `updates`, `values`) são traduzidos em eventos AG-UI: `RUN_STARTED`, `TEXT_MESSAGE_*`, `STEP_*`, `STATE_SNAPSHOT`, `RUN_FINISHED`/`RUN_ERROR`.
- Só aceita `POST`.

Exemplo: `examples/integrations/agui_server` (roda offline e imprime os eventos).

Próximo: [12. Observabilidade](12-observabilidade.md).
