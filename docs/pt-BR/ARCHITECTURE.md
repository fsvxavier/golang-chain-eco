# Arquitetura

[English](../en-US/ARCHITECTURE.md) | **Português** | [Español](../es-ES/ARCHITECTURE.md)

## Camadas

```
deepagents ──► agents (create_agent + middleware) ──► langgraph/graph (Pregel)
                    │                                      │
                    └──► langgraph/prebuilt (ToolNode)     ├─ channels  (slots de estado + reducers)
                                                           ├─ checkpoint (Saver)
core (messages, models, tools, runnables, …)  ◄────────────┴─ store (memória de longo prazo)
```

## Runtime do LangGraph (`langgraph/graph`)

* **O estado** é um `map[string]any`; cada chave é um canal declarado em um `Schema`
  (`LastValue` por padrão, ou `graph.Messages()`, `graph.Append()`, `graph.Sum()`,
  qualquer reducer `channels.BinOp`). `graph.NewTyped[T]` é uma camada tipada opcional:
  o schema vem das tags da struct (`graph:"key,reducer=messages|append|sum"`),
  os nós recebem `T` e devolvem atualizações parciais verificadas contra as tags.
* **A execução** é bulk-síncrona: cada super-step roda todas as tasks agendadas
  em paralelo sobre o mesmo snapshot do estado, aplica as escritas pelos reducers
  na ordem das tasks e então faz o roteamento (arestas estáticas, barreiras `AddJoin`,
  routers condicionais, `Command{Goto,Send}`, fan-out com `Send`).
* **Os nós** devolvem `nil`, uma atualização de `State` ou um `Command`.
* **Persistência**: com `WithCheckpointer`, um checkpoint é gravado após a entrada e
  em cada passo. Os threads são selecionados com `runnables.WithConfig(ctx, graph.Thread(id))`;
  `GetState`, `GetStateHistory`, `UpdateState` (e `checkpoint_id` para fork /
  viagem no tempo) estão disponíveis. Os checkpoints são indexados por (thread, namespace): o grafo
  raiz usa `""`, um subgrafo executado pela task `node:idx` usa `parent|node:idx`.
  Durabilidade (`WithDurability` / `WithRunDurability`): `sync` (padrão, grava a cada passo),
  `async` (gravações em segundo plano, ordenadas, descarregadas antes de `Invoke` retornar), `exit` (grava
  apenas na pausa, no fim ou na falha).
* **Human-in-the-loop**: `graph.Interrupt(ctx, v)` pausa; a execução devolve o estado
  com `graph.Interrupts(state)`. Retome com `Invoke(ctx, graph.Command{Resume: x})`.
  O nó reexecuta desde o início, então os efeitos colaterais antes de `Interrupt` precisam ser
  idempotentes. `InterruptBefore/After` pausam de forma estática. Tasks irmãs já concluídas de um
  step interrompido não são reexecutadas.
* **Streaming**: `StreamEvents(ctx, in, ModeValues|ModeUpdates|ModeCustom|ModeMessages)`.
  Os nós emitem eventos customizados com `graph.StreamWriter(ctx)`. Com `graph.Subgraphs()`,
  os eventos de grafos aninhados são repassados com `Event.Namespace` preenchido.
* **Subgrafos**: invoque um grafo compilado dentro de um nó. Um `Interrupt` interno se propaga
  para a execução externa e retoma corretamente, com ou sem o checkpointer próprio do subgrafo
  (o checkpoint dele fica no seu namespace).

## Agents

`agents.Create` monta `before_agent → [before_model → model → after_model] ⇄ tools →
after_agent`. Um middleware implementa qualquer um de `BeforeAgent`, `BeforeModel`, `WrapModel`,
`AfterModel`, `WrapTool`, `AfterAgent`, além de `Tools()` e `StateSchema()` opcionais.
Os hooks redirecionam o fluxo definindo `jump_to` (`agents.JumpEnd|JumpTools|JumpModel`).
Embutidos: limites de chamadas de modelo/tool, fallback, retry, sumarização, edição de contexto,
lista de tarefas, remoção de PII, human-in-the-loop.

## Deep Agents

`deepagents.Create` = planejamento (`write_todos`) + tools de filesystem virtual
(`ls read_file write_file edit_file glob grep`, `execute` para backends de sandbox) +
subagents via `task` + sumarização + memória opcional (`AGENTS.md`), skills
(`SKILL.md`) e HITL. Backends: `State` (estado do grafo), `Store` (entre threads),
`Filesystem` (disco, confinado à raiz, com verificação de symlinks), `LocalShell` (adiciona `execute`,
**executa comandos no host — só para uso confiável**), `Composite` (roteia por prefixo).

## Persistência e operação

* `checkpoint.Memory` para testes; `checkpoint/sqlsaver` para SQLite/PostgreSQL via
  `database/sql` (o driver fica por sua conta). Os valores são JSON: registre tipos de estado
  próprios com `checkpoint.Register` (mensagens, `graph.Send`, arquivos, todos, tasks
  assíncronas e avaliações de rubrica já vêm pré-registrados). Escalares Go (`int64`, `uint*`,
  `float32`, `time.Time`, `time.Duration`, `[]byte`) mantêm o tipo; números JSON sem tipo
  voltam como `int` ou `float64`. `checkpoint.Strict(true)` transforma gravar uma struct
  não registrada em erro, em vez de um `map[string]any` silencioso.
  `internal/sqlitetest` executa o saver contra SQLite real
  (`go test ./internal/sqlitetest/...`; o driver é uma dependência só de teste).
* **Cache de nós**: `graph.WithCachePolicy` em um nó + `graph.WithCache(graph.NewMemoryCache())`.
* **Nós adiados**: `graph.Deferred()` executa um nó apenas quando nenhuma outra task está pendente.
* **LangSmith**: `tracing/langsmith` é um `callbacks.Handler` que envia runs
  (`callbacks.WithHandlers(ctx, tracer)`), em modo best effort, com fila assíncrona; `Close()` descarrega.
* **Grafos remotos**: `remote.Graph` executa um assistant em um Agent Server (LangGraph
  Platform) atrás da mesma API `Invoke/Stream`; `remote.Client` expõe threads e runs.
* **Extras do Deep Agents**: `deepagents/middleware.Rubric` (loop com avaliador),
  `backends.WithPermissions` / `Options.Permissions` (permitir/negar por operação e
  glob de caminho, aplicados também aos subagents), `middleware.AsyncSubagents` com
  `LocalRunner` (goroutines) ou `RemoteRunner` (Agent Server) e as tools de tarefas
  `start/check/update/cancel/list`.

## Integrações (`integrations/`, `providers/`)

* **Um contrato pequeno por tipo de backend**, então um novo serviço são poucos métodos:
  `vectorstore.Backend` (`Upsert`, `Query`, `Delete`; `vectorstore.New(embedder, backend)` adiciona
  embedding, filtros e MMR), `checkpoint.Saver` (4 métodos), `store.Store`, `retrievers.Retriever`,
  `backends.Backend` / `backends.Executor` para o Deep Agents.
* **Router.** `providers/router` é um `ChatModel` sobre uma lista ordenada de destinos (provedores nativos ou gateways OpenAI-compatíveis) com failover em erros recuperáveis, entrada round-robin/random/ponderada e continuação de streaming; `chatmodels.Init("router:<model>")` o monta a partir das variáveis `ROUTER_*`.
* **Sem SDKs.** Serviços HTTP/JSON usam `net/http`; os protocolos de fio que não tinham forma HTTP são
  implementados diretamente com a stdlib: RESP (Redis), MongoDB OP_MSG + BSON + SCRAM-SHA-256, Bolt/PackStream
  (Memgraph), o WebSocket de exec do Kubernetes, autenticação Digest (Infinispan) e AWS SigV4 (`providers/bedrock.Sign`,
  reutilizado por DynamoDB, S3, Kendra, Lambda). Backends SQL recebem um `*sql.DB`; o driver é escolha de quem chama.
* **Uma suíte de conformidade** (`internal/conformance`: `Saver`, `Store`, `Vector`) roda contra todo backend,
  em memória ou real, para que todos se comportem como as implementações de referência. Testes que exigem um serviço real
  são controlados por variável de ambiente e se pulam sozinhos; os que precisam de um driver SQL ficam no pacote `internal/pgtest`
  (dependências de driver só de teste), para que o próprio código da biblioteca não importe nenhum pacote de terceiros.
* **`openai.Chat` também é a base de dialetos** da API Chat Completions. `Path`, `StreamPath`, `Query`,
  `TokenFunc` (token OAuth/IAM por requisição) e `Rewrite` (editar o corpo) permitem que o IBM watsonx.ai reutilize
  todo o pipeline de requisição/stream/tool-call em vez de duplicá-lo.
* **`backends.Remote` transforma "executar um comando de shell em algum lugar" em um backend completo do Deep Agents**
  (read, write, edit, ls, glob, grep, execute) com trechos de shell POSIX. `kubernetes.Sandbox` fornece o runner
  para um pod descartável; qualquer outro runner isolado (container, VM, sandbox em nuvem) se conecta da mesma forma.
* **Retrieval consciente de permissões** (`spicedb.FilterRetriever`, `permit.FilterRetriever`) verifica cada
  documento recuperado contra o motor de políticas e falha de forma fechada: um id ausente ou uma verificação
  que falha nunca vaza conteúdo.
* **Protocolo de frontend**: `agui.Handler` mapeia os eventos de stream do grafo (`messages`, `updates`, `values`) em
  eventos AG-UI via SSE (`RUN_STARTED`, `TEXT_MESSAGE_*`, `STEP_*`, `STATE_SNAPSHOT`, `RUN_FINISHED`/`RUN_ERROR`).
* **Níveis de verificação honestos** são registrados por integração em `docs/pt-BR/INTEGRATIONS.md` (serviço real
  versus teste de contrato) e a cobertura de provedores em `docs/pt-BR/PROVIDERS.md` / `docs/pt-BR/PROVIDERS-ANALISE.md`.

## Diferenças em relação às bibliotecas Python

* O estado é um `map[string]any`, não um modelo TypedDict/Pydantic; use `graph.NewTyped[T]`
  ou `tools.Decode` para trabalhar com structs.
* Não portado: Studio, build/deploy da CLI, os backends gerenciados de sandbox/context-hub do LangSmith e
  experimentos hospedados. Evals offline ficam em `evals/`; `langgraph/dev`
  serve grafos pelo Agent Protocol para desenvolvimento.

## langmem

`langmem/` fica ao lado de `agents/`: depende de `langgraph/store`, `langgraph/graph`, `core/{models,tools,embeddings}` e `agents` (para `Middleware`), e nada depende dele.

- **Tipos de memória são tools.** Cada `Schema` é oferecido ao modelo como uma tool (o equivalente em Go dos schemas Pydantic do LangMem entregues ao trustcall); atualizações repetem `json_doc_id`, remoções usam `RemoveDoc`. Isso mantém um único protocolo de extração para todo tipo de memória e todo provedor com tool calling.
- **Núcleo puro, casca com estado.** `Manager.Extract` recebe e devolve memórias; `StoreManager` o envolve com recuperação, seeding, fases e escritas mínimas (só itens alterados são gravados com `Put`).
- **Busca semântica sem mudar `store.Store`.** `IndexedStore` guarda o vetor dentro do valor do item, então funciona sobre todo backend existente (memory, sql, redis, mongo, dynamodb); `Query` degrada para ranking por palavras-chave quando o store não é um `SemanticSearcher`.
- **Namespaces são templates** resolvidos a partir de `runnables.Config.Configurable`; uma variável ausente é um erro, nunca um namespace compartilhado.
- **Resumos são incrementais e não destrutivos**: o modelo vê `summarized_messages`, `messages` continua completo, e `RunningSummary` viaja no estado com checkpoint.
- **A formação em background** é um timer com debounce por thread, não um worker de fila; veja docs/pt-BR/LANGMEM.md sobre a lacuna do executor remoto.

## knowledge, rag, cag, kag

- **knowledge** define `Answerer` (pergunta entra, resposta com fontes sai) e o `Router` que escolhe uma técnica por pergunta. Ele não importa nenhuma técnica, então qualquer `Answerer` pode ser roteado.
- **rag** compõe `core/retrievers` e `integrations/retrieval`: um `Indexer` incremental (hash de conteúdo por fonte) e um `Pipeline` que funde resultados vetoriais e BM25 com RRF.
- **cag** coloca o corpus inteiro em um prefixo de sistema estável em bytes e deixa o provedor fazer cache (`cache_control` para Anthropic, cache automático de prefixo nos demais); `messages.Usage` informa `CacheReadTokens` e `CacheCreationTokens`.
- **kag** mantém um grafo de conhecimento atrás da interface `GraphStore` (implementação de referência em memória, suíte de conformidade compartilhada). Um extrator com LLM o constrói; o `Engine` planeja os saltos, percorre o grafo e sintetiza a partir das triplas. Tipos de relação vindos do modelo são normalizados antes de chegar a uma linguagem de consulta. `integrations/graphstore` implementa `GraphStore` para Neo4j (Query API via HTTP) e Memgraph (Bolt); ambos validam rótulos e tipos de relação antes de interpolá-los no Cypher e são testados contra a mesma suíte de conformidade do store em memória.
- **knowledge.Toolkit** transforma os answerers em valores `tools.Tool` e os serve via `mcp/server`, seguindo o padrão de `ciam.Toolkit`; `cmd/knowledge` é a CLI por cima.

## agentprotocol, ciam, openswe

- **agentprotocol** serve grafos compilados. A unidade é o thread: um `hub` por thread mantém um ring buffer de eventos de streaming tipados (`seq` monotônico, replay por `since`), alimentado por um tradutor que mapeia eventos do grafo e callbacks nos canais do protocolo; handlers REST, SSE, comandos e um pequeno WebSocket RFC 6455 leem todos o mesmo hub. Os registros de thread ficam em memória e são reconstruídos a partir do checkpointer; os registros de run passam por um `RunStore` (memória por padrão, `NewStoreRunStore` sobre qualquer `store.Store` para sobreviver a reinícios). Eventos de subgrafos mantêm o namespace, então os filtros `namespaces`/`depth` e `agent.getTree` enxergam os grafos filhos. `graph.Compiled.Saver()` foi adicionado para que o servidor possa listar, copiar e apagar checkpoints.
- **ciam** guarda chunks, hashes de arquivos e memórias em qualquer `store.Store`; a busca carrega o corpus do projeto uma vez (estatísticas de BM25 + vetores) e funde coseno e BM25 com RRF (coseno por varredura, ou por um índice vetorial opcional como `vectorstore.NewHNSW`). `ciam api` serve as mesmas tools em HTTP simples. As memórias são memórias do `langmem`. As capacidades são primeiro valores `tools.Tool` e depois são servidas via `mcp/server`, então agents em processo e editores compartilham uma só implementação.
- **openswe** é composição: `deepagents` + um backend de sandbox por thread + middleware de guardas + tools do GitHub. As regras são impostas duas vezes (no prompt e no código). O estado durável (achados, vigilâncias, automações, vínculos PR<->thread, ids de sandbox) passa por `store.Store`, nunca por um banco próprio; o trabalho de um thread é serializado pelo `triggers.Dispatcher` (uma run ativa por thread, fila para o resto). Slack e Linear são adaptadores sobre o mesmo dispatcher; as respostas deles passam por wrappers `Notifier` em volta do runner.

## compress

`compress/` é um pacote folha (só biblioteca padrão). Ele responde quanto comprimir e quando; os agents aplicam a resposta.

- **Um decider, não um limiar.** Um `Decider` responde perguntas tipadas sobre um `State` montado a partir de um `Summary`, nunca sobre o histórico bruto, então um modelo de janela pequena pode decidir. `RuleDecider` implementa a mesma interface com limiares. `Guarded` envolve qualquer decider para que erros, timeouts e respostas de baixa confiança caiam nas regras, pergunta por pergunta.
- **Compressão é uma cópia.** `middleware.Compression` (`agents/middleware`) transforma a cópia por chamada em um `WrapModelCall`: Caveman nos turnos Human e AI antigos, a diretiva Ponytail e a diretiva de resposta concisa após a última mensagem do usuário, e resultados JSON de ferramentas reescritos em TOON quando estritamente menores. Chamadas de ferramentas nunca são tocadas, Caveman nunca toca em resultados de ferramentas, e o estado guardado nunca é reescrito.
- **Ponytail e TOON.** `compress.PonytailDirective` é um texto de regras (sem modelo, sem estado); `compress.TOONFromJSON` e `TOONShorter` codificam JSON (só biblioteca padrão, sem decodificador). Os dois são decididos pelo mesmo `Decider` que as outras alavancas. Veja [COMPRESSION.md](COMPRESSION.md).
- **Caminho do shell à parte.** `compress.RTK` decide antes de um comando rodar e chega ao `backends.LocalShell` pelo hook `Rewrite`, depois da checagem de `AllowedCommands` no comando original.
- **Módulos.** `HTTPDecider` fala com o servidor Laya por `net/http`. O decider laya-go nativo precisa de cgo e ONNX Runtime. O `go.mod` da raiz exige o laya-go, mas só a build tag `laya_native` o compila (`examples/langgraph/full_team/native` e `examples/langgraph/decider_laya_native`). Veja [COMPRESSION.md](COMPRESSION.md).
