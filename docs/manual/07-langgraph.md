# 7. LangGraph: StateGraph

Use `langgraph/graph` quando o fluxo tem estado, ramificações, loops ou pausas. Um grafo é composto por **nós** (funções Go) ligados por **arestas**, trabalhando sobre um **estado** compartilhado.

## 7.1 Anatomia de um grafo

```go
import (
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

schema := graph.Schema{
	"messages": graph.Messages(),      // acumula com o reducer add_messages
	"steps":    graph.Sum(),           // soma inteiros
	"log":      graph.Append(),        // concatena listas
	"topic":    channels.LastValue(),  // último valor (padrão para chaves não declaradas)
}

g := graph.NewStateGraph(schema).
	AddNode("research", func(ctx context.Context, s graph.State) (any, error) {
		return graph.State{"log": []any{"pesquisou " + s["topic"].(string)}, "steps": 1}, nil
	}).
	AddNode("write", writeNode).
	AddNode("review", reviewNode).
	AddEdge(graph.Start, "research").
	AddEdge("research", "write").
	AddConditionalEdges("write", func(ctx context.Context, s graph.State) (any, error) {
		if s["steps"].(int) > 3 {
			return graph.End, nil
		}
		return "review", nil
	}).
	AddEdge("review", "write")

app, err := g.Compile()
out, err := app.Invoke(ctx, graph.State{"topic": "Go generics"})
fmt.Println(app.Mermaid()) // diagrama Mermaid do grafo
```

Passos:

1. **Schema**: declara os canais e seus reducers.
2. **Nós**: `AddNode(nome, func(ctx, State) (any, error))`.
3. **Arestas**: `AddEdge(de, para)` (use `graph.Start` e `graph.End`), `AddConditionalEdges(de, router)`.
4. **Compile**: valida e devolve `*graph.Compiled`, que oferece `Invoke`, `Stream`, `StreamEvents`, `GetState`, `GetStateHistory`, `UpdateState`, `Mermaid` e `Nodes`.

Atalhos: `SetEntryPoint(n)` equivale a `AddEdge(Start, n)`; `SetFinishPoint(n)` a `AddEdge(n, End)`; `AddSequence(nomes, funcs)` encadeia nós em ordem.

## 7.2 Nós

Assinatura: `func(ctx context.Context, s graph.State) (any, error)`.

- O nó recebe uma **cópia** do estado; alterá-la não afeta o grafo.
- Devolva só o que mudou, como `graph.State{...}`. As atualizações passam pelos reducers do schema.
- Valores aceitos de retorno: `nil`, `graph.State`, `graph.Command`, `[]graph.Send`.
- Um erro retornado aborta a execução (use `graph.WithRetry` para tentar de novo).

### Opções de nó

```go
g.AddNode("fetch", fetchFn,
	graph.WithRetry(runnables.RetryOptions{MaxAttempts: 3}),
	graph.WithCachePolicy(graph.CachePolicy{TTL: time.Minute}),
	graph.Deferred(),
)
app, _ := g.Compile(graph.WithCache(graph.NewMemoryCache()))
```

| Opção | Efeito |
|---|---|
| `graph.WithRetry(opts)` | Repete o nó com backoff quando falha |
| `graph.WithCachePolicy(p)` | Memoriza o resultado por TTL (requer `Compile(graph.WithCache(...))`) |
| `graph.Deferred()` | O nó só roda quando não há outras tasks pendentes (útil para consolidar) |

## 7.3 Reducers e canais

Se duas escritas na mesma chave acontecem no mesmo passo, o reducer decide o resultado. Sem reducer, vale o último valor e escritas simultâneas podem conflitar. Para acumular resultados de ramos paralelos, use `Append()`, `Sum()`, `Messages()` ou um reducer próprio:

```go
maxCh := channels.BinOp(func(cur, upd any) (any, error) {
	if upd.(int) > cur.(int) {
		return upd, nil
	}
	return cur, nil
}, func() any { return 0 })

schema := graph.Schema{"best": maxCh}
```

`channels.Overwrite{Value: v}` força a substituição ignorando o reducer. Outros canais: `AnyValue`, `Topic(accumulate)`, `NamedBarrier(nomes...)`, `Ephemeral(guard)`.

### Estado tipado com struct (`graph.NewTyped`)

Se você prefere uma struct a `map[string]any`, `graph.NewTyped[T]()` monta o `Schema` a partir das tags `graph:"chave[,reducer=nome]"` de `T`. A API baixo nível continua a mesma; o tipado é só uma camada por cima (`langgraph/graph/typed.go`):

```go
type OrderState struct {
	Messages []messages.Message `graph:"messages,reducer=messages"`
	Total    int                `graph:"total,reducer=sum"`
	Log      []string           `graph:"log,reducer=append"`
	Approved bool               `graph:"approved"`
	Items    int                `graph:"items"`
}

g := graph.NewTyped[OrderState]().
	AddNode("price", func(ctx context.Context, s OrderState) (graph.Update, error) {
		return graph.Update{"total": s.Items * 10, "log": []string{"precificado"}}, nil
	}).
	SetEntryPoint("price").SetFinishPoint("price")

c, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
out, interrupts, err := c.Invoke(ctx, OrderState{Items: 3}) // out é OrderState
out, _, err = c.Invoke(ctx, graph.Command{Resume: true})    // retomar a mesma thread
```

Regras:

- Só campos exportados com tag entram no estado. Campos sem tag são ignorados; `graph:"-"` ignora explicitamente.
- Reducers: `last` (padrão), `messages` (add_messages), `append` (concatena os elementos de cada update; campo slice), `sum` (campo numérico). Reducer desconhecido, chave duplicada, campo não exportado ou tipo incompatível com o reducer são erros de `Compile`.
- Um nó tipado recebe `T` e devolve um `graph.Update` (mapa parcial). Uma struct não consegue dizer "este campo não foi atualizado", porque o zero também é um valor válido; por isso o nó nomeia as chaves que escreve. Chave que não é tag de `T` falha o nó.
- `Invoke(ctx, in)` aceita `T` (escreve todos os campos com tag, zeros inclusive) ou `graph.Command` (resume ou atualização parcial). Devolve `(T, []graph.InterruptInfo, error)`.
- `Stream(ctx, in)` devolve `iter.Seq2[T, error]`. Para eventos completos (`StreamEvents`), `GetState` e o `Saver`, use `c.Unwrap()`, que devolve o `*graph.Compiled`.
- Se `T` (ou `*T`) tem `Validate() error`, ela roda na entrada de cada nó; erro retornado falha a execução.
- Os tipos de campo struct são registrados em `checkpoint.Register` com o nome `pacote.Tipo` quando `NewTyped` roda, então sobrevivem a `checkpoint.Strict(true)`. Structs anônimas não podem ser registradas.
- Arestas, arestas condicionais (o roteador recebe `T`) e `AddJoin` existem no builder tipado; o que faltar está em `g.Graph()`.

Exemplo offline completo: `go run ./examples/langgraph/typed_state`.

## 7.4 Roteamento condicional

O router devolve o nome do próximo nó, `graph.End`, ou uma lista de `graph.Send`:

```go
AddConditionalEdges("classify", func(ctx context.Context, s graph.State) (any, error) {
	switch s["kind"] {
	case "billing":
		return "billing_agent", nil
	case "tech":
		return "tech_agent", nil
	}
	return graph.End, nil
})
```

Com um mapa opcional, o router devolve rótulos e o mapa traduz para nós: `AddConditionalEdges("classify", router, map[string]string{"b": "billing_agent", "t": "tech_agent"})`.

## 7.5 Paralelismo e junção

```go
AddEdge("plan", "search_web").
AddEdge("plan", "search_db").          // search_web e search_db rodam no mesmo super-step
AddJoin([]string{"search_web", "search_db"}, "merge") // merge só roda quando os dois terminam
```

Sem `AddJoin`, o nó de destino rodaria assim que *qualquer* ramo terminasse. Use um reducer (`Append`) no canal onde os dois ramos escrevem.

## 7.6 Fan-out dinâmico com `Send` (map-reduce)

```go
AddConditionalEdges("plan", func(ctx context.Context, s graph.State) (any, error) {
	var sends []graph.Send
	for _, topic := range s["topics"].([]string) {
		sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"topic": topic}})
	}
	return sends, nil
})
```

Cada `Send` cria uma task de `worker` com o `Arg` como estado de entrada, em paralelo. Os resultados voltam ao estado principal pelos reducers (por exemplo `graph.Append()` em `"results"`). Exemplos: `examples/langgraph/map_reduce`, `llm_compiler`.

## 7.7 `Command`: atualizar e desviar no mesmo retorno

```go
return graph.Command{
	Update: graph.State{"status": "aprovado"},
	Goto:   []string{"publicar"},
}, nil
```

Também usado para retomar execuções pausadas: `app.Invoke(ctx, graph.Command{Resume: valor})` (capítulo 8).

## 7.8 Subgrafos

Um grafo compilado pode ser chamado dentro de um nó:

```go
inner, _ := innerGraph.Compile()
outer := graph.NewStateGraph(schema).
	AddNode("team", func(ctx context.Context, s graph.State) (any, error) {
		return inner.Invoke(ctx, graph.State{"messages": s["messages"]})
	})
```

Um `Interrupt` interno propaga para o grafo pai e retoma corretamente, com ou sem checkpointer próprio no subgrafo (o checkpoint dele fica no namespace `nó:índice`). Se vários nós do subgrafo param no mesmo superpasso, a primeira pausa (na ordem das tarefas) sobe ao pai, e cada `Command{Resume}` responde a próxima, sempre na mesma ordem. Para receber os eventos do subgrafo no stream do pai, use `graph.Subgraphs()`; cada evento vem com `Event.Namespace`. Exemplos: `subgraph`, `hierarchical_teams`.

## 7.9 Limite de passos

O padrão é 25 super-steps. Ao exceder, `Invoke` devolve `*graph.GraphRecursionError`. Ajuste por execução:

```go
ctx = runnables.WithConfig(ctx, runnables.Config{RecursionLimit: 50})
```

Loops legítimos (agents que usam muitas tools) podem precisar de limite maior; loops infinitos serão cortados.

## 7.10 Memória entre threads (store)

```go
app, _ := g.Compile(graph.WithStore(store.NewMemory()))

// dentro de um nó:
st := graph.StoreFrom(ctx)
st.Put(ctx, []string{"users", "ana"}, "prefs", map[string]any{"idioma": "pt-BR"})
item, _ := st.Get(ctx, []string{"users", "ana"}, "prefs")
```

O `store.Store` guarda memória de **longo prazo** compartilhada entre threads, com namespaces (`[]string`). Capítulo 8.

## 7.11 Prebuilt: ToolNode e agent ReAct mínimo

```go
agent, err := prebuilt.CreateReactAgent(prebuilt.ReactOptions{
	Model: m, Tools: []tools.Tool{search}, Prompt: "Seja objetivo.",
})
```

`prebuilt.NewToolNode(tools...)` executa em paralelo as `ToolCalls` da última mensagem de IA e `prebuilt.ToolsCondition` é o roteador padrão "tem tool call? vá para tools; senão, fim". Para um agent completo (middleware, limites, HITL), use `agents.Create` (capítulo 9). O exemplo `react_from_scratch` monta o loop à mão.

## 7.12 API funcional: `Entrypoint` e `Task`

Escreva o fluxo como código Go comum. Tasks são memoizadas no checkpoint, então uma retomada não repete trabalho já feito.

```go
wf := functional.New("relatorio", saver, func(ctx context.Context, tema string) (string, error) {
	dados, err := functional.Task(ctx, "coletar", func(ctx context.Context) (string, error) {
		return coletar(tema) // efeitos colaterais ficam dentro de tasks
	}).Result()
	if err != nil {
		return "", err
	}
	ok := graph.Interrupt(ctx, "Publicar?").(string) // human-in-the-loop
	if ok != "sim" {
		return "descartado", nil
	}
	return publicar(dados)
})

ctx := runnables.WithConfig(ctx, graph.Thread("wf-1"))
r, _ := wf.Invoke(ctx, "baterias") // r.Interrupt != nil: aguardando
r, _ = wf.Resume(ctx, "sim")       // "coletar" NÃO roda de novo
```

Quando usar: fluxos lineares com poucas ramificações e muita lógica de negócio, em que desenhar nós e arestas seria cerimônia. Use `StateGraph` quando quiser visualizar o fluxo (Mermaid), paralelizar ramos ou ter vários pontos de entrada.

## 7.13 Padrões prontos nos exemplos

| Padrão | Exemplo |
|---|---|
| ReAct (loop modelo/tools) | `react_from_scratch`, `react_agent_middleware` |
| Plan-and-execute, ReWOO, LLM Compiler | `plan_and_execute`, `rewoo`, `llm_compiler` |
| Reflexão (gerador + crítico) | `reflection`, `code_assistant` |
| Supervisor de agentes | `multi_agent_supervisor`, `hierarchical_teams` |
| RAG corretivo/adaptativo | `corrective_rag`, `self_rag`, `adaptive_rag` |
| Map-reduce | `map_reduce` |

Próximo: [8. Persistência, human-in-the-loop e streaming](08-persistencia-hitl-streaming.md).
