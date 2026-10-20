# 2. Conceitos fundamentais

[English](../../en-US/manual/02-conceitos.md) | **Português** | [Español](../../es-ES/manual/02-conceitos.md)

## 2.1 Camadas

```
deepagents ──► agents (create_agent + middleware) ──► langgraph/graph (Pregel)
                    │                                      │
                    └──► langgraph/prebuilt (ToolNode)     ├─ channels  (slots de estado + reducers)
                                                           ├─ checkpoint (Saver)
core (messages, models, tools, runnables, …)  ◄────────────┴─ store (memória de longo prazo)
```

Cada camada usa a de baixo. Você pode ficar em qualquer nível:

- **core**: chains simples (`prompt | modelo | parser`), RAG, extração estruturada.
- **langgraph**: fluxos com estado, ramificações, loops, pausas e persistência.
- **agents**: loop pronto de "modelo decide, tool executa" com middleware.
- **deepagents**: agent com planejamento, arquivos e delegação.

## 2.2 Mensagens (`core/messages`)

Uma `messages.Message` representa uma mensagem de conversa. O tipo é único; o campo `Role` diferencia:

| Role | Construtor | Significado |
|---|---|---|
| `RoleSystem` | `messages.System(texto)` | Instruções ao modelo |
| `RoleHuman` | `messages.Human(texto)` | Fala do usuário |
| `RoleAI` | `messages.AI(texto, chamadas...)` | Resposta do modelo, opcionalmente com `ToolCalls` |
| `RoleTool` | `messages.Tool(texto, toolCallID)` | Resultado de uma tool |
| `RoleRemove` | `messages.Remove(id)` | Marca remoção de uma mensagem pelo reducer |

Campos relevantes: `ID`, `Content`, `Parts` (conteúdo multimodal), `ToolCalls`, `ToolCallID`, `Status` (`success`/`error`), `Artifact`, `Usage` (tokens), `ResponseMetadata`.

Funções úteis:

```go
msg.Text()                          // texto da mensagem
messages.Coerce(v)                  // string | Message | []Message -> []Message
messages.AddMessages(a, b)          // reducer: anexa, atualiza por ID, aplica Remove
messages.Filter(msgs, include, exclude)
messages.Trim(msgs, messages.TrimOptions{...})
messages.MergeRuns(msgs)            // junta mensagens consecutivas do mesmo papel
messages.ApproxTokens(msgs)         // contagem aproximada de tokens
messages.Last(msgs)                 // última mensagem
```

## 2.3 Runnable (`core/runnables`)

`Runnable[I, O]` é a abstração central: algo que recebe `I` e devolve `O`.

```go
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, in I) (O, error)
}
```

Prompts, modelos (via `models.AsRunnable`), parsers, chains e retrievers são runnables. Compõem-se com `Pipe`, `Pipe3`, `Pipe4`, `Parallel`, `Branch`, `WithRetry`, `WithFallbacks`. Capítulo 4.

Capacidades opcionais, detectadas por interface: `Streamer[I, O]` (streaming nativo) e `Batcher[I, O]` (lote). Use as funções genéricas `runnables.Stream` e `runnables.Batch`; elas usam a implementação nativa quando existe e caem para `Invoke` quando não.

## 2.4 Configuração por contexto

A configuração de execução viaja no `context.Context`:

```go
ctx = runnables.WithConfig(ctx, runnables.Config{
	RunName:        "minha-chain",
	Tags:           []string{"prod"},
	Metadata:       map[string]any{"user": "ana"},
	Callbacks:      []callbacks.Handler{tracer},
	MaxConcurrency: 4,
	RecursionLimit: 50,
	Configurable:   map[string]any{"thread_id": "usuario-42"},
})
```

`WithConfig` **mescla** com a configuração já presente. Atalho para threads de grafo: `graph.Thread("usuario-42")` devolve uma `Config` com `thread_id`.

## 2.5 Estado e canais (LangGraph)

O estado de um grafo é um `graph.State`, que é `map[string]any`. Cada chave é um **canal** com uma regra de atualização (reducer):

| Canal | Construtor | Comportamento |
|---|---|---|
| Último valor | `channels.LastValue()` (padrão) | A escrita substitui o valor |
| Mensagens | `graph.Messages()` | Reducer `AddMessages` |
| Acumular lista | `graph.Append()` | Concatena listas |
| Somar | `graph.Sum()` | Soma inteiros |
| Efêmero | `channels.Ephemeral(false)` | Vale só por um passo |
| Customizado | `channels.BinOp` | Qualquer função `(atual, novo) -> resultado` |

Cada nó recebe uma **cópia** do estado e devolve uma **atualização parcial**; o runtime aplica as atualizações pelos reducers. Mais no capítulo 7.

## 2.6 Execução em super-steps

O runtime é bulk-synchronous (estilo Pregel):

1. Em cada super-step, todos os nós agendados rodam **em paralelo** sobre o mesmo snapshot do estado.
2. As escritas são aplicadas pelos reducers, na ordem das tasks.
3. O runtime roteia para os próximos nós (arestas estáticas, junções, roteadores condicionais, `Command`, `Send`).
4. Com checkpointer, um checkpoint é salvo após a entrada e após cada passo.

Consequência prática: nós no mesmo passo não enxergam as escritas uns dos outros. Para somar resultados de ramos paralelos, declare um reducer (`Append`, `Sum` ou customizado) no canal que recebe as escritas.

## 2.7 Tipos de retorno de um nó

Um nó (`func(ctx, graph.State) (any, error)`) pode devolver:

- `nil`: nenhuma atualização.
- `graph.State{...}`: atualização parcial.
- `graph.Command{Update: ..., Goto: []string{...}}`: atualização e desvio de fluxo.
- `[]graph.Send{...}`: fan-out dinâmico (map-reduce).

## 2.8 Tools

Uma `tools.Tool` expõe nome, descrição, JSON Schema dos argumentos e uma função de execução. `tools.New[Args]` gera o schema por reflexão da struct `Args`. O modelo "chama" a tool pedindo `ToolCalls`; o agent executa e devolve uma mensagem `RoleTool`. Capítulo 5.

## 2.9 Callbacks

Qualquer execução emite eventos (início/fim/erro de chain, modelo, tool, nó). `callbacks.Handler` consome esses eventos para logs, tracing e métricas. Os handlers vão no contexto: `callbacks.WithHandlers(ctx, h1, h2)`. Capítulo 12.

## 2.10 Erros que você vai ver

| Erro | Quando |
|---|---|
| `*graph.GraphRecursionError` | O grafo passou do `RecursionLimit` (padrão 25 passos) |
| `*tools.Error` (via `tools.Errorf`) | Erro "do modelo": vira mensagem de tool e o loop continua |
| `*middleware.LimitError` | Limite de chamadas de modelo/tool atingido (com `ExitBehavior: "error"`) |
| `*middleware.PIIError` | PII detectado com estratégia `block` |
| `*mcp.RPCError` | Erro JSON-RPC devolvido por servidor MCP |

---

Anterior: [1. Introdução e instalação](01-introducao-e-instalacao.md) · Próximo: [3. Modelos e provedores](03-modelos-e-provedores.md) · [Índice do manual](README.pt-BR.md)
