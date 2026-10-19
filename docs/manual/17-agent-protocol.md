# 17. Agent Protocol: servir e consumir agents por uma API aberta

O pacote `agentprotocol` publica qualquer grafo compilado (`agents.Create`, `deepagents.Create`, um `StateGraph`) com o [Agent Protocol](https://github.com/langchain-ai/agent-protocol): uma API REST **independente de framework** para agentes em produção, mais um protocolo de **streaming por thread** (eventos tipados, filtros, replay, aprovação humana). Referência completa e limites: [docs/AGENT-PROTOCOL.md](../AGENT-PROTOCOL.md).

Pré-requisitos: capítulos 7 e 8 (grafos, checkpointer, interrupts) e 9 (agents).

## 17.1 Quando usar

| Você quer… | Use |
|---|---|
| uma UI/serviço externo conversando com seu agent por HTTP | `agentprotocol.Server` |
| a API no dialeto da LangGraph Platform (SDKs oficiais) | `remote/server` (cap. 11) |
| chamar um agent Go de outro processo Go | `agentprotocol.Client` |
| acompanhar tokens, ferramentas e aprovações ao vivo | canais de streaming (17.5) |

## 17.2 Servir um agent

```go
agent, _ := agents.Create(agents.Options{
	Model: model, Tools: tools,
	Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}, // estado das threads
})
srv, err := agentprotocol.New(agentprotocol.Options{
	Agents: []agentprotocol.AgentSpec{{ID: "bot", Name: "Bot", Description: "responde perguntas", Graph: agent}},
	APIKey: os.Getenv("AP_KEY"), // opcional
})
http.ListenAndServe(":8123", srv)
```

- **Sem checkpointer** o agent roda, mas threads não guardam estado (sem `history`, `state.get` nem retomada).
- Em produção troque `checkpoint.NewMemory()` por `sqlsaver`, Redis, Mongo ou DynamoDB (cap. 8 e 13) — e `Options.Store` pelo store de longo prazo correspondente.
- O servidor guarda threads/runs em memória, mas reconstrói uma thread pelo checkpointer ao primeiro acesso depois de um reinício.

## 17.3 Chamar pelo cliente

```go
c := agentprotocol.NewClient("http://localhost:8123", os.Getenv("AP_KEY"))

th, _ := c.CreateThread(ctx, agentprotocol.ThreadCreate{Metadata: map[string]any{"user": "ana"}})
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{
	ThreadID: th.ThreadID, AgentID: "bot",
	Messages: []agentprotocol.Message{{Role: "user", Content: "Qual o clima em Lisboa?"}},
})
fmt.Println(out.Run.Status, out.Messages[len(out.Messages)-1].Content)
```

Três formas de rodar:

| Forma | Chamada | Quando |
|---|---|---|
| esperar | `CreateAndWait` (`POST /runs/wait`) | resposta curta |
| background | `CreateRun` + `WaitRun`/`GetRun` | trabalho longo; `webhook` avisa ao fim |
| stream | `StreamRun` (`POST /runs/stream`) | UI incremental: `for ev, err := range c.StreamRun(ctx, req)` |

Sem `thread_id` a run é **stateless** (thread efêmera, apagada ao final). Com thread ocupada a resposta é `409`. `on_completion: "delete"`, `on_disconnect: "cancel"` e `if_not_exists: "create"` mudam o ciclo de vida.

## 17.4 Threads, histórico e time travel

```go
hist, _ := c.ThreadHistory(ctx, th.ThreadID, 20, "")            // mais novo primeiro
c.PatchThread(ctx, th.ThreadID, agentprotocol.ThreadPatch{
	Checkpoint: &agentprotocol.ThreadCheckpoint{CheckpointID: hist[3].Checkpoint.CheckpointID},
	Messages:   []agentprotocol.Message{{Role: "user", Content: "Na verdade, e no Porto?"}},
})                                                                // forka a partir daquele ponto
cp, _ := c.CopyThread(ctx, th.ThreadID)                           // cópia independente
found, _ := c.SearchThreads(ctx, agentprotocol.ThreadSearchRequest{Metadata: map[string]any{"user": "ana"}})
```

Cancelar: `c.CancelRun(ctx, id, "interrupt", true)` mantém o progresso; `"rollback"` volta o estado ao de antes da run.

## 17.5 Streaming por thread: eventos tipados

O protocolo de streaming é **centrado na thread**. Você abre um stream filtrado e dispara comandos; os eventos chegam com `seq` crescente.

```go
filter := agentprotocol.Filter{Channels: []string{"lifecycle", "messages", "tools", "input"}}
go c.Command(ctx, "t1", "run.start", map[string]any{"assistantId": "bot", "input": "oi"})
for ev, err := range c.Events(ctx, "t1", filter) {
	data := ev.Params["data"].(map[string]any)
	switch ev.Method {
	case "messages": // message-start, content-block-start/delta/finish, message-finish
	case "tools":    // tool-started, tool-finished, tool-error (toolCallId liga ao bloco da mensagem)
	case "lifecycle":// started, running, completed, failed, interrupted
	}
}
```

Reconectar sem perder nada: `filter.Since = ultimoSeq` (ou o header `Last-Event-ID`) reenvia do buffer e continua ao vivo. Exemplo completo: `go run ./examples/agentprotocol/human_in_the_loop`.

## 17.6 Aprovação humana

Um agent com `HumanInTheLoop` (cap. 9) ou `graph.Interrupt` (cap. 8) **pausa**: a thread vira `interrupted` e o canal `input` emite `input.requested` com `interruptId` e `payload`. Responda por:

```go
c.Command(ctx, "t1", "input.respond", map[string]any{
	"namespace": []string{}, "interruptId": id,
	"response": []map[string]any{{"type": "approve"}},   // o valor que o interrupt espera
	// opcionais: "update": {...estado...}, "goto": "no" ou {"node": "x", "input": {...}}
})
```

(`response` é entregue ao `Interrupt`; ids inválidos voltam como erro `no_such_interrupt`.) Pela API REST, mandar um novo `run` com `input` numa thread interrompida também a retoma.

Mensagens que chegam durante uma run em andamento (`run.start` ou `input.inject`) ficam **na fila** e rodam logo depois da atual.

## 17.7 WebSocket

`GET /threads/{id}/stream` com `Upgrade: websocket` troca comandos e eventos na mesma conexão (`subscription.subscribe`, `unsubscribe`, `reconnect` + `run.start`, `input.respond`, `state.*`). O servidor é um RFC 6455 mínimo (texto, fragmentação, ping/pong; sem compressão). Para o navegador use um cliente WebSocket qualquer e as mesmas mensagens JSON `{"id": 1, "method": "...", "params": {...}}`.

## 17.8 Memória de longo prazo (`/store`)

```go
c.PutItem(ctx, []string{"users", "ana"}, "prefs", map[string]any{"units": "metric"})
items, _ := c.SearchItems(ctx, agentprotocol.StoreSearchRequest{NamespacePrefix: []string{"users"}, Filter: map[string]any{"units": "metric"}})
```

É o mesmo `store.Store` dos grafos (`graph.WithStore`): passe-o em `Options.Store` e os nós do agent leem o que clientes escreveram.

## 17.9 Produção

- **Autenticação**: `APIKey` única; para multiusuário coloque um gateway na frente e propague a identidade em `config.configurable` (ex.: `user_id`), que chega às tools e ao `langmem` (cap. 16).
- **Réplicas**: buffer de eventos e registro de runs são por processo; use afinidade por `thread_id`.
- **Limites**: o corpo de requisição é limitado a 16 MB; consumidores de SSE lentos são desconectados em vez de travar a run.
- **Observabilidade**: runs passam pelos callbacks de `core/callbacks` (cap. 12); cada run carrega `run_id`, `thread_id` e `agent_id` na metadata.
- **O que falta**: evento `tasks` e buffer de eventos compartilhado entre réplicas. Subgrafos já aparecem como namespaces filhos e runs finalizadas persistem com `Options.RunStore = agentprotocol.NewStoreRunStore(st)` (ver o documento de referência).

Exemplos: `go run ./examples/agentprotocol/serve_and_call` e `.../human_in_the_loop`.

Próximo: [18. ciam](18-ciam-contexto.md).
