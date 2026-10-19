# Agent Protocol em Go (`agentprotocol/`)

Implementação do [Agent Protocol](https://github.com/langchain-ai/agent-protocol) (API v0.1.6 + *Agent Streaming Protocol*) sobre os grafos compilados deste projeto. Um grafo servido aqui pode ser operado por qualquer cliente do protocolo, inclusive o `agentprotocol.Client` deste pacote. Só stdlib.

Tutorial: [manual/17-agent-protocol.md](manual/17-agent-protocol.md). Exemplos: [`examples/agentprotocol/`](../examples/agentprotocol/).

O pacote `remote/server` (já existente) serve o dialeto da LangGraph Platform (`/threads/{id}/runs/...`). `agentprotocol` serve o protocolo aberto (`/runs`, `/threads`, `/agents`, `/store`, streaming por thread). Os dois coexistem; escolha pelo cliente que você precisa atender.

## O que o protocolo define

| Recurso | Para quê | Endpoints |
|---|---|---|
| **Agents** | introspecção: nome, capacidades, schemas JSON | `POST /agents/search`, `GET /agents/{id}`, `GET /agents/{id}/schemas` |
| **Threads** | conversas multi-turno com estado persistente | `POST /threads`, `POST /threads/search`, `GET/PATCH/DELETE /threads/{id}`, `GET /threads/{id}/history`, `POST /threads/{id}/copy` |
| **Runs** | execuções de agent (em background, esperando, ou em stream) | `POST /runs`, `/runs/wait`, `/runs/stream`, `/runs/search`, `GET/DELETE /runs/{id}`, `GET /runs/{id}/wait`, `GET /runs/{id}/stream`, `POST /runs/{id}/cancel` |
| **Store** | memória de longo prazo entre threads | `PUT/GET/DELETE /store/items`, `POST /store/items/search`, `POST /store/namespaces` |
| **Streaming** | eventos ordenados por thread, filtráveis, com replay | `POST /threads/{id}/stream` (SSE), `GET /threads/{id}/stream` (WebSocket), `POST /threads/{id}/commands` |

Todos os 27 endpoints do OpenAPI estão implementados.

## Servir

```go
srv, _ := agentprotocol.New(agentprotocol.Options{
    Agents: []agentprotocol.AgentSpec{{ID: "bot", Name: "Bot", Description: "...", Graph: compiled}},
    APIKey: os.Getenv("AP_KEY"),   // opcional: x-api-key ou Authorization: Bearer
    Store:  store.NewMemory(),      // /store/*; troque por sqlstore/redis/mongo
})
http.ListenAndServe(":8123", srv)
```

O grafo precisa de **checkpointer** para ter estado de thread (`graph.WithCheckpointer`). Sem ele, runs funcionam mas threads não guardam estado, e `history`/`state.*`/resume não têm o que ler.

`AgentSpec` aceita `InputSchema/OutputSchema/StateSchema/ConfigSchema`; sem eles o servidor responde um schema de chat (`messages`). Vários agents: o corpo da run diz `agent_id` (com um só agent registrado ele é o padrão).

## Modelo de execução

- **Thread** = identidade durável (estado no checkpointer) + registro em memória (metadados, status, buffer de eventos). Status: `idle`, `busy`, `interrupted`, `error`.
- **Run** em background numa goroutine; status `pending` até terminar, depois `success`, `error` ou `interrupted` (cancelada ou pausada por `Interrupt`).
- **Uma run ativa por thread**: outra `POST /runs` na mesma thread responde `409`. (Pelo streaming protocol, `run.start` durante uma run **enfileira** a entrada para rodar logo depois.)
- **Thread interrompida**: o próximo `run` com `input` **retoma** o grafo (`Command{Resume}`). O valor de retomada é `input.resume`, se existir; senão as `messages`, se for só isso; senão o objeto de `input` inteiro. Para responder por `interruptId` (com `update`/`goto`), use o comando `input.respond`.
- **Run sem `thread_id`** (stateless): thread efêmera, apagada (com o checkpoint) ao final.
- `on_completion: delete` apaga a run; `on_disconnect: cancel` cancela se o cliente sumir (padrão `continue`); `webhook` recebe `POST {run, values, messages}` ao terminar; `if_not_exists: create` cria a thread na hora (padrão `reject`).
- **Persistência após reinício**: um `GET /threads/{id}` reconstrói a thread a partir do checkpointer. Os registros de runs são gravados a cada mudança de status num `RunStore` (`Options.RunStore`): o padrão (`NewMemoryRunStore`) mantém só em memória, e `NewStoreRunStore(store)` os grava num `langgraph/store.Store` (namespace `["agentprotocol","runs",threadID]`), que sobrevive ao reinício quando o store é durável. Após reinício, `GET /runs/{id}` e `POST /runs/search` devolvem essas runs; `DELETE` as remove do store. Uma run que ainda estava `pending` quando o processo morreu é marcada como `error` (com a mensagem `server stopped before the run finished`) na inicialização, pois o protocolo não tem estado de falha por queda. O buffer de eventos (`GET /runs/{id}/stream`, streaming de thread) continua só em memória: após reinício, uma run restaurada não tem eventos para reenviar. Cancelar ou fazer rollback de uma run restaurada não atua no grafo (a run não está mais em execução).

### Mensagens

`Message{role, content, id, metadata}` com `content` string ou blocos. Entrada aceita `user|human`, `assistant|ai`, `system`, `tool`; blocos que não são texto viram o texto deles. A saída usa os papéis do LangChain (`human`, `ai`, `system`, `tool`) e `metadata.tool_calls` nas mensagens `ai`.

### Time travel

- `GET /threads/{id}/history?limit&before` — revisões, da mais nova para a mais velha (`before` = id de checkpoint).
- `PATCH /threads/{id}` com `checkpoint` + `values`/`messages` — **forka** a partir daquele checkpoint (vira a nova ponta).
- `POST /threads/{id}/copy` — cópia independente do histórico.
- `POST /runs/{id}/cancel?action=rollback` — cancela e restaura o estado de antes da run (fork para o checkpoint anterior; **os checkpoints da run não são apagados**).

## Streaming por thread

Canais: `values`, `updates`, `messages`, `tools`, `lifecycle`, `input`, `checkpoints`, `custom` (e `custom:*`); `tasks` é aceito mas nunca emitido.

| Canal | Eventos (`params.data.event`) |
|---|---|
| `lifecycle` | `started`, `running`, `completed`, `failed`, `interrupted` (+ `graphName`, `error`) |
| `messages` | `message-start` → `content-block-start` → `content-block-delta` (`text-delta`) → `content-block-finish` → … → `message-finish` (com `usage`); blocos `tool_call` finalizados |
| `tools` | `tool-started` (`toolCallId`, `toolName`, `input`), `tool-finished` (`output`), `tool-error` |
| `input` | método `input.requested` com `interruptId` e `payload` |
| `values` / `updates` | estado completo / `{node, values}` por nó |
| `checkpoints` | `{id, parentId, step, source}` a cada superpasso |

Garantias: cada evento tem `seq` monotônico e `eventId` (`evt_<seq>`); **mensagens nunca se intercalam** e todo `message-start` fecha com `message-finish`; o `toolCallId` de `tool-started/finished` é o **mesmo id** do bloco `tool_call` da mensagem, inclusive quando a ferramenta roda depois de uma aprovação humana (retomada); uma run retomada **não reenvia** mensagens já anunciadas. Modelos que não fazem streaming (só respondem inteiro) também geram os eventos de mensagem, a partir do `updates`.

**SSE**: `POST /threads/{id}/stream` com `{channels, namespaces?, depth?, since?}`. `since` (ou o header `Last-Event-ID`) reenvia do buffer circular (padrão 2000 eventos por thread) e segue ao vivo, sem perder nem duplicar. A thread é criada sob demanda se o stream abrir antes da primeira run. Comentário `: keepalive` a cada 15 s. Consumidor lento é desconectado (em vez de travar a run).

**Comandos** (`POST /threads/{id}/commands` ou no WebSocket): `run.start`, `input.respond` (por `interruptId`, com `update` e `goto`), `input.inject`, `state.get`, `state.listCheckpoints`, `state.fork`, `agent.getTree`. Respostas `{"type":"success","id","result","meta":{"appliedThroughSeq"}}` ou `{"type":"error","id","error": <código>}` com os códigos do protocolo (`invalid_argument`, `unknown_command`, `no_such_interrupt`, `no_such_checkpoint`, `no_such_subscription`, `not_supported`...).

**WebSocket** (`GET /threads/{id}/stream`): RFC 6455 mínimo — texto, fragmentação, ping/pong, close; sem extensões nem compressão. Comandos `subscription.subscribe/unsubscribe/reconnect` na mesma conexão; assinaturas persistem entre runs.

## Cliente

`agentprotocol.NewClient(url, apiKey)`: `SearchAgents`, `GetAgent`, `GetAgentSchemas`, `CreateThread`/`GetThread`/`SearchThreads`/`PatchThread`/`DeleteThread`/`CopyThread`/`ThreadHistory`, `CreateRun`/`CreateAndWait`/`WaitRun`/`GetRun`/`SearchRuns`/`CancelRun`/`DeleteRun`, `StreamRun` (SSE por `iter.Seq2`), `Events(thread, Filter)`, `Command(thread, method, params)`, `PutItem`/`GetItem`/`DeleteItem`/`SearchItems`/`ListNamespaces`. Erros HTTP viram `*StatusError{Status, Code, Message}`.

## Diferenças e limites (sem maquiagem)

- **Streaming de run (REST)**: `POST /runs/stream` emite `event: <modo>` (`metadata`, `values`, `updates`, `messages`, `custom`, `end`); no modo `messages` o dado é `{chunk, node}`. O formato exato do SDK Python da LangGraph Platform pode diferir.
- **Namespaces**: o streaming pede `graph.Subgraphs()`, então os eventos de subgrafos chegam com `namespace` = caminho do subgrafo (`["sub:0"]`, um elemento `nó:id` por nível; raiz = `[]`). Os filtros `namespaces` e `depth` do SSE valem sobre esse caminho, e `agent.getTree` monta `children` a partir dos namespaces vistos nas runs da thread. Limites: o `status` de cada filho é o da última run da thread (não há status por subgrafo), e o histórico de eventos segue o buffer circular da thread.
- **`run.start` com run ativa** enfileira (não injeta input no grafo em andamento: o runtime não suporta).
- **`state.fork`** cria **nova thread** com o histórico até o checkpoint, e roda se houver `input`.
- **Timestamps de checkpoint** são os do momento em que o servidor os viu (não são persistidos pelo checkpointer); checkpoints anteriores a um reinício usam a data de criação da thread.
- **Sem multi-processo**: o buffer de eventos e as runs em execução vivem no processo, e a recuperação marca como `error` todo registro `pending` do store na inicialização, o que assume um único processo por store. Várias réplicas sobre o mesmo store se atrapalhariam; use afinidade por thread ou um servidor por thread. Não há Redis nem outro backend distribuído aqui.
- **Retenção**: no máximo 5000 runs finalizadas por processo; ao estourar, metade das mais antigas é apagada também do `RunStore`. Runs de um processo anterior não entram nessa conta e não são podadas.
- **Sem métricas/limites de taxa**; `APIKey` é uma chave única. Coloque um gateway na frente para multi-tenant.
- `GET /store/items` aceita `namespace` repetido (`?namespace=a&namespace=b`) e o formato `a.b` dos SDKs; nomes de namespace com ponto precisam do primeiro formato.

## Testes

`go test -race ./agentprotocol/`: 100% offline (`httptest`, modelos *fake*). Cobrem introspecção, ciclo de vida de threads (history/patch/fork/copy/search/delete), runs (wait, background, stateless, 409, cancel, webhook, `on_completion`), SSE de run, store, API key, o protocolo de streaming ponta a ponta (ordem e fechamento de mensagens, `toolCallId`, filtro por canal, replay, interrupção + `input.respond`, comandos e códigos de erro), run enfileirada, **WebSocket** com cliente de baixo nível (fragmentação, subscribe/unsubscribe/reconnect) e estado que sobrevive a reinício.
