# LangMem em Go (`langmem/`)

[English](../en-US/LANGMEM.md) | **Português** | [Español](../es-ES/LANGMEM.md)

Port do [LangMem](https://github.com/langchain-ai/langmem) (v0.0.30, MIT) integrado ao ecossistema deste projeto: usa o `langgraph/store`, o `agents.Create` com middleware, o `graph.StateGraph`, as tools e os chat models já existentes. Só stdlib.

Tutorial passo a passo: [manual/16-memoria-langmem.md](manual/16-memoria-langmem.md). Exemplos: [`examples/langmem/`](../../examples/langmem/).

## Modelo mental

Toda operação de memória do LangMem segue o mesmo padrão: **recebe conversas + estado atual da memória → pede a um LLM para expandir/consolidar → devolve o novo estado.** Três perguntas desenham o seu sistema: *o quê* lembrar, *quando* formar a memória e *onde* guardar.

| Tipo | Serve para | Representação | API |
|---|---|---|---|
| Semântica | fatos, preferências | coleção de itens ou **perfil** (1 documento) | `StoreManager`, `ManageMemory` |
| Episódica | experiências passadas, few-shot | coleção de `Episode` | `StoreManager` + `EpisodeSchema` |
| Procedural | como o agent se comporta | o próprio system prompt | `PromptOptimizer`, `MultiPromptOptimizer` |
| Curto prazo | caber no contexto | resumo incremental | `SummarizeMessages`, `SummarizationNode` |

| Quando formar | Latência | Como |
|---|---|---|
| **Hot path** (o agent decide, durante a conversa) | maior | tools `ManageMemory` + `SearchMemory` |
| **Background** (depois da conversa, com debounce) | nenhuma | `StoreManager` + `ReflectionExecutor` |

## Mapa Python → Go

| LangMem (Python) | Go (`langmem`) | Observação |
|---|---|---|
| `create_memory_manager` | `NewManager(model, opts...)` → `Extract(ctx, msgs, existing, maxSteps)` | função pura, sem store |
| `create_memory_store_manager` | `NewStoreManager(model, namespace, opts...)` → `Process` / `Invoke` | persiste só o que mudou |
| `create_manage_memory_tool` | `ManageMemory(ToolConfig)` | `Actions`, `Schema`, `Instructions`, `Name` |
| `create_search_memory_tool` | `SearchMemory(ToolConfig)` | `Artifact: true` = `content_and_artifact` |
| `create_memory_searcher` | `MemorySearcher{...}.Search` | LLM escreve as queries |
| `create_thread_extractor` | `ExtractThread[T]` | schema genérico (`ThreadSummary` padrão) |
| `create_prompt_optimizer(kind=...)` | `NewPromptOptimizer(model, kind, cfg)` | `Gradient`, `Metaprompt`, `PromptMemory` |
| `create_multi_prompt_optimizer` | `MultiPromptOptimizer` | classificador + otimização concorrente |
| `Prompt` (TypedDict) | `Prompt{Name, Prompt, UpdateInstructions, WhenToUpdate}` | |
| `AnnotatedTrajectory` | `Trajectory{Messages, Feedback}` | feedback: string, map ou nil |
| `summarize_messages` | `SummarizeMessages` | algoritmo portado fielmente |
| `SummarizationNode` | `SummarizationNode{...}.Node()` + `SummarizationSchema()` | |
| `RunningSummary` | `RunningSummary` | |
| `ReflectionExecutor` (local) | `ReflectionExecutor` | `Submit` → `*Future`; `Shutdown` |
| `NamespaceTemplate` | `Namespace.Resolve(ctx)` | variáveis vêm de `Configurable` |
| `InMemoryStore(index=...)` | `NewIndexedStore(store, embedder)` | veja "Diferenças" |
| `pydantic` schemas | `SchemaFor[T](name, desc)` | JSON Schema por reflexão de struct |
| — (receita dos guias) | `Middleware` | injeta memórias + tools + formação em background no `agents.Create` |

## API em detalhe

### Schemas e memórias

`Schema{Name, Description, Parameters}` descreve um *tipo* de memória; cada schema vira uma **tool** oferecida ao modelo (como no LangMem, onde os modelos Pydantic viram tools do trustcall). Um manager pode misturar tipos.

- `MemorySchema` (padrão): texto livre, guardado como `{"content": "..."}`.
- `EpisodeSchema` (`Episode{Observation, Thoughts, Action, Result}`), `TripleSchema` (`Triple{Subject, Predicate, Object, Context}`).
- `SchemaFor[T]("Nome", "descrição")` para os seus: `SchemaFor[UserProfile]("UserProfile", "Save the user's preferences")`.

`Extracted{ID, Kind, Content, Deleted}` é a memória que entra e sai do `Manager`. `Decode[T](content)` converte para a struct; `Text(content)` extrai o texto de uma nota; `Note(id, texto)` e `ExistingNotes(textos...)` constroem entradas.

### Manager

```go
m := langmem.NewManager(model,
    langmem.WithSchemas(langmem.MemorySchema, langmem.EpisodeSchema),
    langmem.WithInstructions("..."),     // padrão: langmem.DefaultInstructions
    langmem.WithInserts(true), langmem.WithUpdates(true), langmem.WithDeletes(false))
out, err := m.Extract(ctx, msgs, existing, 3) // maxSteps: até 3 rodadas
```

Protocolo com o modelo (igual ao LangMem): uma chamada de tool por memória nova; repetir `json_doc_id` atualiza; `RemoveDoc` apaga; a partir da 2ª rodada existe a tool `Done`. Entre rodadas o modelo recebe "Memory X inserted/updated." e a lista atual. `Extract` devolve a lista completa (memórias existentes mudadas ou não, novas, e as apagadas com `Deleted: true`). Conteúdo que não valida contra o schema é descartado e o erro volta ao modelo como resultado da tool.

Padrões: inserts e updates **ligados**, deletes **desligados** no `Manager`; deletes **ligados** no `StoreManager` (como no LangMem).

### StoreManager

```go
sm := langmem.NewStoreManager(model, langmem.Namespace{"memories", "{langgraph_user_id}"},
    langmem.WithStore(st),            // ou compile o grafo com graph.WithStore
    langmem.WithQueryLimit(5),
    langmem.WithQueryModel(qm),       // LLM escreve as buscas (senão: janelas dilatadas da conversa)
    langmem.WithDefault(langmem.Seed{Content: UserProfile{}}), // semente → perfil
    langmem.WithPhases(langmem.Phase{IncludeMessages: false}))  // consolidação extra
puts, err := sm.Invoke(ctx, langmem.Payload{Messages: msgs, MaxSteps: 2})
```

Fluxo de `Invoke`: resolve namespace → **recupera** memórias relevantes (janelas de 1, 2, 4… últimas mensagens; ou queries do `QueryModel`; completa com memórias não ranqueadas até `QueryLimit`) → semeia `default` se vazio → `Extract` → fases → grava **só o que mudou** → apaga o removido. Itens ficam como `{"kind": <schema>, "content": <conteúdo>}`. Também há `Search`, `Get`, `Put`, `Delete` no namespace resolvido.

**Perfil** = um único documento refinado no lugar: `WithInserts(false)`, `WithDeletes(false)`, `WithDefault(...)`; a chave é `langmem.DefaultKey` (`"default"`). **Coleção** = padrões.

### Tools (hot path)

- `manage_memory`: `{content, action: create|update|delete, id}`. `create` com `id` é erro; `update`/`delete` sem `id` é erro; ids são UUID; grava `{"content": ...}`.
- `search_memory`: `{query, limit=10, offset=0, filter}`; devolve JSON `[{namespace,key,value,created_at,updated_at,score}]`.
- Erros viram mensagens para o modelo (`tools.Error`), exceto falta de store/configuração (`ErrConfiguration`).

### Namespaces e store

`Namespace{"memories", "{langgraph_user_id}"}` (o padrão, `DefaultNamespace`) resolve `{variáveis}` a partir de `runnables.Config.Configurable` — use `langmem.WithConfigurable(ctx, "langgraph_user_id", "u1")` ou `Config{Configurable: ...}`. Variável ausente = erro `ErrConfiguration` listando as chaves disponíveis (nunca grava em namespace errado).

`store.Store` não tem busca semântica; `IndexedStore` a adiciona sobre **qualquer** backend (memory, sqlstore, redis, mongo, dynamodb): o vetor vai dentro do item (`_embedding`, escondido nos resultados) e a busca é um cosseno por varredura do prefixo. `Query(ctx, store, prefix, query, filter, limit)` usa busca semântica se o store implementa `SemanticSearcher`, senão ranking por palavras em comum, e lista tudo com query vazia.

### Otimização de prompts (memória procedural)

| Kind | Chamadas ao LLM | Como |
|---|---|---|
| `PromptMemory` | 1 | pede `GeneralResponse{logic, update_prompt, new_prompt}` |
| `Metaprompt` | 1 a `MaxReflectionSteps` | laço `think`/`critique` e, no fim, `OptimizedPromptOutput` |
| `Gradient` | 2 a `MaxReflectionSteps`+1 | laço até `recommend{warrants_adjustment, hypotheses, full_recommendations}`; se `false`, devolve o prompt intacto; senão uma chamada de reescrita |

Defaults: `MaxReflectionSteps` 5, `MinReflectionSteps` 1. Garantias: sem trajetórias → prompt inalterado, **zero** chamadas; as variáveis `{x}` do prompt original **têm** de sobreviver (senão erro); chaves soltas viram `{{ }}`; "No recommendations" mantém o original. O `MultiPromptOptimizer` pergunta a um classificador quais prompts (nomes validados) causaram a falha, usando `WhenToUpdate`, e otimiza só esses, em paralelo.

### Resumo (memória de curto prazo)

`SummarizeMessages(ctx, msgs, running, opts)` — algoritmo do LangMem: ao acumular `MaxTokensBeforeSummary` (padrão `MaxTokens`) resume do início até esse ponto, desde que o resto caiba em `MaxTokens − MaxSummaryTokens − system`; resultados de tool de uma chamada resumida vão junto; entrada maior que a janela do resumidor é cortada (começando em mensagem humana). Retorna `[system?, "Summary of the conversation so far: …", recentes…]` e um `RunningSummary` (ids resumidos + último id) para a próxima chamada ser incremental. Toda mensagem precisa de ID; `MaxSummaryTokens` precisa ser menor que `MaxTokens`.

`SummarizationNode` (nó de grafo): lê `messages`, escreve `summarized_messages` e `context.running_summary`; com `InputKey == OutputKey` sobrescreve a lista (`RemoveAll`). `SummarizationMiddleware`: o mesmo dentro do `agents.Create` — o modelo vê a visão resumida, o histórico guardado fica completo e o resumo é incremental (diferente de `agents/middleware.Summarization`, que reescreve o histórico).

### Background

```go
ex := &langmem.ReflectionExecutor{Manager: sm}
fut, _ := ex.Submit(ctx, langmem.Payload{Messages: msgs}, langmem.SubmitOptions{After: 30 * time.Minute})
...
ex.Shutdown(true, false) // executa pendentes agora e espera
```

Mesmo `thread_id` (de `Configurable` ou `SubmitOptions.ThreadID`) cancela o job pendente (`*Future` resolve com `ErrCancelled`) e reagenda com a conversa maior. O job herda os *valores* do contexto, não o cancelamento.

### Middleware

```go
langmem.Middleware{Store: st, Namespace: ns,
    Inject: true, Limit: 5,       // memórias relevantes no system prompt a cada chamada
    ExposeTools: true,            // manage_memory + search_memory
    Manager: sm, Executor: ex, Delay: time.Minute} // formação ao fim da execução (em background se Executor)
```

## Diferenças em relação ao LangMem Python

Deliberadas ou por limitação — sem maquiagem:

- **Atualização por substituição por padrão; patch com `WithPatchUpdates()`.** Sem a opção, o modelo reenvia o conteúdo completo com `json_doc_id` (funciona com qualquer modelo com tool calling; gasta mais tokens em documentos grandes). Com `langmem.WithPatchUpdates()`, a tool `PatchDoc{json_doc_id, planned_edits, patches[]}` aplica operações RFC 6902 sobre o conteúdo (ponteiros RFC 6901, pacote `internal/jsonpatch`), como o `trustcall` do LangMem. Um patch que falha volta ao modelo como resultado da tool para uma nova tentativa; se falhar de novo, o modelo reenvia o conteúdo completo com `json_doc_id` (substituição).
- **`ReflectionExecutor` remoto**: `langmem.NewRemoteReflectionExecutor(client, graphID)` roda o grafo de memória no servidor (`remote/server`) com `after_seconds` e cancelamento por `rollback` no mesmo thread. Diferenças: um run já iniciado também é revertido por um submit mais novo; o thread é obrigatório; `Future.Wait` não devolve `Put`s (as escritas acontecem no servidor); a API de store (`Search`) não foi portada, então a busca continua local.
- **Busca semântica fora do `store.Store`.** O `Store` do projeto é CRUD + filtro por igualdade. Por padrão `IndexedStore` faz cosseno por varredura (O(n) por busca). Com `ix.Index = vectorstore.NewHNSW()` a busca vira aproximada (HNSW em Go puro; recall@10 medido em teste). `Put` e `Delete` mantêm o índice em dia; `Reindex` indexa itens gravados antes. Limites: o índice é em memória e não é salvo junto com o store; escritas feitas direto no store de baixo não entram nele. Filtros: `$eq $ne $gt $gte $lt $lte $in` sobre campos do valor. Os operadores são avaliados em Go: a busca pede k×4 itens ao backend, filtra e cai na varredura completa se faltarem resultados. Para escala grande, use um vector store dedicado e adapte `SemanticSearcher`.
- **Sem variantes async**: tudo recebe `context.Context`; use goroutines.
- **Não portado:** `langmem.graphs.*` (grafos de serviço hospedados do LangGraph Platform), `prompts/_layers.MemoryLayer` (API interna, "not public" no upstream) e `graph_rag.py` (comentado no upstream).
- **Metaprompt:** se o modelo já devolve o prompt final numa rodada intermediária, o Go aceita; o Python ignora e continua o laço.
- **Recuperação:** completa as memórias vistas pelo extrator com itens não ranqueados até `QueryLimit`, para que um store só-lexical não faça o modelo duplicar memórias.
- **IDs de memória:** a própria chave do store (hex de 128 bits), não um `uuid5` derivado do namespace.
- **Ordem dos resultados de `Search`** é por score; empates, por chave.
- Os textos de prompt (`DefaultInstructions`, gradient, metaprompt, prompt_memory, resumo) são adaptados do LangMem, licença MIT.

## Testes

`go test -race ./langmem/` — 100% offline (e `IndexedStore` sobre SQLite real em `internal/sqlitetest`; Redis/Mongo/DynamoDB não foram exercitados com ele), com `models.Fake` roteirizado (tool calls) e `embeddings.Fake`. Cobre namespaces, busca semântica/lexical, permissões do manager, multi-step, ciclo de vida do store manager (inserir → atualizar → apagar, isolamento por usuário), perfil com semente, fases, query model, tools, searcher, otimizadores (3 tipos + multi + cura de variáveis), resumo incremental, debounce do executor e o middleware dentro de um `agents.Create`.
