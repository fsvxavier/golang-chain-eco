# 16. Memória de longo prazo (LangMem)

[English](../../en-US/manual/16-memoria-langmem.md) | **Português** | [Español](../../es-ES/manual/16-memoria-langmem.md)

O pacote `langmem` é o port do [LangMem](https://github.com/langchain-ai/langmem): faz o agent **lembrar entre conversas**, aprender com feedback e caber em contextos longos. Este capítulo é o tutorial; a referência completa, o mapa Python → Go e as diferenças estão em [docs/pt-BR/LANGMEM.md](../LANGMEM.md).

Pré-requisitos: capítulos 5 (tools), 8 (store) e 9 (agents e middleware).

## 16.1 Escolha o tipo de memória

| Você quer que o agent… | Tipo | Use |
|---|---|---|
| lembre fatos e preferências do usuário | semântica (coleção) | `StoreManager` ou tools `ManageMemory`/`SearchMemory` |
| mantenha um cadastro estruturado e atual do usuário | semântica (perfil) | `StoreManager` com `WithInserts(false)` + `WithDefault` |
| reaproveite "como resolvi isso antes" | episódica | `StoreManager` + `EpisodeSchema` |
| melhore o próprio system prompt com feedback | procedural | `PromptOptimizer` |
| não estoure o contexto numa conversa longa | curto prazo | `SummarizeMessages` / `SummarizationMiddleware` |

E *quando* formar a memória:

- **Hot path**: o agent decide durante a conversa (tools). Imediato, mas custa latência e uma decisão a mais por turno.
- **Background**: um extrator reflete depois que a conversa esfria (`ReflectionExecutor`). Sem latência e com maior recall; a memória só existe depois do atraso.

## 16.2 Preparar store e namespace

```go
import (
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

// Qualquer store.Store serve (memory, sqlstore, redis, mongo, dynamodb).
// IndexedStore adiciona busca semântica; em produção troque embeddings.Fake por um embedder real.
st := langmem.NewIndexedStore(store.NewMemory(), embeddings.Fake{})

ns := langmem.Namespace{"memories", "{langgraph_user_id}"} // é o langmem.DefaultNamespace
ctx := langmem.WithConfigurable(ctx, "langgraph_user_id", "alice")
```

O namespace isola usuários: `alice` nunca enxerga o que `bob` gravou. Se a variável não estiver em `Configurable`, a chamada falha com `langmem.ErrConfiguration` em vez de gravar num lugar errado.

Para o `StoreManager` e as tools funcionarem **sem** passar o store, compile o grafo com ele (`Compile(graph.WithStore(st))`); o pacote usa `graph.StoreFrom(ctx)`.

## 16.3 Hot path: o agent cuida da própria memória

```go
cfg := langmem.ToolConfig{Namespace: ns}
agent, _ := agents.Create(agents.Options{
	Model:   model,
	Tools:   []tools.Tool{langmem.ManageMemory(cfg), langmem.SearchMemory(cfg)},
	Compile: []graph.CompileOption{graph.WithStore(st)},
})
out, _ := agent.Invoke(ctx, graph.State{"messages": "Lembre que eu odeio coentro"})
```

`manage_memory` cria/atualiza/apaga (o modelo recebe o `id` pelo `search_memory`); `search_memory` ranqueia por similaridade. Restrinja ações com `ToolConfig{Actions: []string{"create","update"}}` e personalize o texto de quando usar com `Instructions`. Exemplo: `go run ./examples/langmem/hot_path`.

Atalho com injeção automática das memórias no system prompt e tools de uma vez:

```go
mw := langmem.Middleware{Store: st, Inject: true, ExposeTools: true}
agent, _ := agents.Create(agents.Options{Model: model, Middleware: []agents.Middleware{mw}})
```

## 16.4 Background: extrair depois da conversa

```go
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st))

// direto (bloqueia):
puts, err := sm.Process(ctx, msgs)

// em background, com debounce:
ex := &langmem.ReflectionExecutor{Manager: sm}
fut, _ := ex.Submit(ctx, langmem.Payload{Messages: msgs},
	langmem.SubmitOptions{After: 30 * time.Minute, ThreadID: "t1"})
```

Três mensagens rápidas na mesma thread geram **uma** extração, com a conversa inteira. No desligamento chame `ex.Shutdown(true, false)` para não perder memórias pendentes. Para ligar tudo ao agent: `Middleware{Manager: sm, Executor: ex, Delay: time.Minute}`. Exemplo: `go run ./examples/langmem/background`.

O que o `StoreManager` faz a cada passada: busca memórias relacionadas → pede ao modelo para inserir/atualizar/apagar → grava só o que mudou. Ajustes úteis: `WithQueryLimit(n)`, `WithQueryModel(m)` (um LLM escreve as buscas), `WithPhases(...)` (passada extra de consolidação), `Payload{MaxSteps: 3}` (refino em várias rodadas).

## 16.5 Perfil e episódios

Um **perfil** é um documento só, com campos fixos, refinado a cada conversa:

```go
type UserProfile struct {
	Name          string   `json:"name"`
	PreferredName string   `json:"preferred_name"`
	Skills        []string `json:"skills"`
}
sm := langmem.NewStoreManager(model, ns, langmem.WithStore(st),
	langmem.WithSchemas(langmem.SchemaFor[UserProfile]("UserProfile", "Save the user's profile")),
	langmem.WithInserts(false), langmem.WithDeletes(false),
	langmem.WithDefault(langmem.Seed{Content: UserProfile{}}))
// ...
it, _ := sm.Get(ctx, langmem.DefaultKey)
p, _ := langmem.Decode[UserProfile](it.Value["content"])
```

**Episódios** guardam "situação → raciocínio → ação → resultado" para few-shot futuro: `WithSchemas(langmem.EpisodeSchema)` com instruções como "extraia exemplos de interações bem-sucedidas". Pode-se misturar tipos no mesmo manager: `WithSchemas(langmem.MemorySchema, langmem.EpisodeSchema)`. Exemplo: `go run ./examples/langmem/profile_episodic`.

Use `Manager` (sem store) quando quiser só a extração em memória: `NewManager(model).Extract(ctx, msgs, existing, 1)`.

## 16.6 Recuperar memórias

| Necessidade | Chamada |
|---|---|
| por chave | `sm.Get(ctx, key)` |
| por similaridade | `sm.Search(ctx, "pergunta", nil, 5)` ou `langmem.Query(ctx, st, ns, q, nil, 5)` |
| o modelo decide o que buscar | `langmem.MemorySearcher{Model: m, Store: st}.Search(ctx, msgs)` |
| no prompt, automático | `Middleware{Inject: true}` |
| resumo de uma conversa inteira | `langmem.ExtractThread[langmem.ThreadSummary](ctx, model, msgs, "")` |

## 16.7 Memória procedural: o prompt que aprende

```go
opt := langmem.NewPromptOptimizer(model, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 3})
novo, err := opt.Optimize(ctx, []langmem.Trajectory{{
	Messages: conversa,
	Feedback: map[string]any{"score": 0, "comment": "queria código, veio teoria"},
}}, langmem.Prompt{Name: "main", Prompt: "Você é um assistente prestativo."})
```

- `PromptMemory`: 1 chamada, barato. `Metaprompt`: reflete e reescreve. `Gradient`: separa diagnóstico de reescrita (o mais criterioso, o mais caro).
- Sem evidência de falha o prompt volta **igual**. Variáveis `{x}` do prompt original são preservadas (se o modelo as perder, vem erro).
- Vários prompts cooperando: `MultiPromptOptimizer` descobre quais causaram a falha (`Prompt.WhenToUpdate` ajuda) e reescreve só esses. Exemplo: `go run ./examples/langmem/prompt_optimizer`.

Fluxo recomendado: colete conversas com feedback (nota, comentário, edição humana) → otimize em lote, offline → revise a diferença → publique o novo prompt. Não otimize no caminho da requisição.

## 16.8 Memória de curto prazo: resumo incremental

```go
node := langmem.SummarizationNode{SummarizeOptions: langmem.SummarizeOptions{
	Model: modeloBarato, MaxTokens: 4000, MaxSummaryTokens: 256,
	// Counter: seu contador de tokens real (padrão: aproximação ~4 caracteres/token)
}}
schema := langmem.SummarizationSchema()   // canais "context" e "summarized_messages"
schema["messages"] = graph.Messages()
g := graph.NewStateGraph(schema).AddNode("summarize", node.Node()) // ... antes do nó do modelo
```

O nó escreve a visão pronta para o modelo em `summarized_messages` (o histórico completo fica em `messages`) e o `RunningSummary` em `context`, persistido pelo checkpointer, então cada resumo estende o anterior em vez de recomeçar. Dentro de `agents.Create`, use `Middleware: []agents.Middleware{langmem.SummarizationMiddleware{...}}`. Exemplo: `go run ./examples/langmem/summarization`.

Para limitar o tamanho do resumo de verdade, passe um modelo com `max_tokens` fixo (`models.Bind(m, models.WithMaxTokens(256))`): `MaxSummaryTokens` só reserva orçamento.

## 16.9 Testar sem rede

Roteirize o modelo com `models.Fake`. Para o extrator, devolva *tool calls* com o nome do schema (`"Memory"`, `"Episode"`, o seu) e, para atualizar, `json_doc_id`:

```go
fake := models.NewFake(messages.AI("", messages.ToolCall{
	Name: "Memory", Args: map[string]any{"content": "gosta de sushi"}}))
sm := langmem.NewStoreManager(fake, nil, langmem.WithStore(store.NewMemory()))
puts, _ := sm.Process(ctx, msgs)
```

`embeddings.Fake{}` dá similaridade por palavras em comum, suficiente para testar ranking. Os testes do pacote (`langmem/langmem_test.go`) são bons modelos.

## 16.10 Produção e cuidados

- **Privacidade**: memórias são dados pessoais. Namespace por usuário/organização, política de retenção e um caminho de apagamento (`sm.Delete`, `ManageMemory` com `delete`). Considere o middleware `PII` (cap. 9) antes de extrair.
- **Prompt injection**: o texto que o usuário (ou uma página web) coloca na conversa pode virar memória e depois ser injetado no prompt. Trate memórias como entrada **não confiável** (o `Inject` as coloca dentro de `<memories>`), restrinja `Actions` quando o agent não precisa apagar.
- **Custo**: cada passada é ≥ 1 chamada de LLM; use `Executor` com atraso, `WithQueryLimit` baixo e um modelo menor para extrair.
- **Escala da busca**: `IndexedStore` varre o prefixo (O(n) por busca). Para muitas memórias por usuário, indexe em um vector store dedicado (cap. 6/13) e implemente `SemanticSearcher` sobre ele.
- **Consistência**: dois extratores simultâneos no mesmo namespace podem sobrescrever o mesmo item (último vence); serialize por usuário/thread (o debounce do executor já ajuda).
- **Observabilidade**: as chamadas passam pelos callbacks de `core/callbacks` como qualquer modelo/tool (cap. 12).

---

Anterior: [15. Exemplos e receitas](15-exemplos-e-receitas.md) · Próximo: [17. Agent Protocol](17-agent-protocol.md) · [Índice do manual](README.md)
