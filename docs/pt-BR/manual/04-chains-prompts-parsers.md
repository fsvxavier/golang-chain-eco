# 4. Prompts, chains e parsers

[English](../../en-US/manual/04-chains-prompts-parsers.md) | **Português** | [Español](../../es-ES/manual/04-chains-prompts-parsers.md)

## 4.1 Templates de prompt (`core/prompts`)

### Template simples

```go
p := prompts.New("Explique {topic} para um {audience}.")
txt, err := p.Format(prompts.Values{"topic": "goroutines", "audience": "iniciante"})

p.InputVariables()                 // ["topic", "audience"]
p2 := p.Partial(prompts.Values{"audience": "iniciante"}) // fixa variáveis
```

`prompts.Values` é `map[string]any`. Variáveis usam chaves `{nome}`.

### Template de chat

`prompts.Chat` aceita pares `[2]string{papel, template}`, `MessageTemplate`, mensagens estáticas e placeholders:

```go
prompt := prompts.Chat(
	[2]string{"system", "Responda em uma frase."},
	prompts.Placeholder{Name: "history", Optional: true}, // injeta []messages.Message
	[2]string{"human", "Explique {topic}."},
)
msgs, err := prompt.FormatMessages(prompts.Values{"topic": "channels"})
```

Papéis aceitos: `system`, `human`/`user`, `ai`/`assistant`, `placeholder`. Helpers: `prompts.SystemTemplate`, `HumanTemplate`, `AITemplate`.

### Few-shot

`prompts.FewShot` (texto) e `prompts.FewShotChat` (mensagens) montam exemplos a partir de uma lista de `prompts.Example`. `prompts.LengthSelector` seleciona quantos exemplos cabem num limite de tamanho (`ExampleSelector`).

## 4.2 Compor com `Pipe`

`Pipe3` é o equivalente tipado de `prompt | modelo | parser`:

```go
chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
	prompt,                  // Values -> []Message
	models.AsRunnable(m),    // []Message -> Message
	outputparsers.Str{},     // Message -> string
)
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})
```

Os parâmetros de tipo são `[Entrada, T1, T2, Saída]`. `Pipe` junta 2 etapas, `Pipe3` 3, `Pipe4` 4; para mais, encadeie pipes.

### Streaming de uma chain

```go
for tok, err := range runnables.Stream[prompts.Values, string](ctx, chain, prompts.Values{"topic": "channels"}) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(tok)
}
```

### Lote

```go
outs, err := runnables.Batch(ctx, chain, []prompts.Values{{"topic": "a"}, {"topic": "b"}})
```

`Config.MaxConcurrency` limita o paralelismo.

## 4.3 Output parsers (`core/outputparsers`)

| Parser | Entrada → saída |
|---|---|
| `outputparsers.Str{}` | `Message` → `string` |
| `outputparsers.JSON{}` | `Message` → `any` (JSON decodificado; normalmente `map[string]any`) |
| `outputparsers.Struct[T]{}` | `Message` → `T` (decodifica JSON em struct) |
| `outputparsers.CommaList{}` | `Message` → `[]string` |
| `outputparsers.ToolCalls{}` | `Message` → chamadas de tool |
| `outputparsers.ToolArgs[T]{Name: "..."}` | `Message` → argumentos `T` de uma tool específica |

## 4.4 Combinadores (`core/runnables`)

| Função | Para quê |
|---|---|
| `Lambda(nome, fn)` | Transformar uma função Go em runnable |
| `Parallel(map)` | Rodar ramos em paralelo sobre a mesma entrada e juntar em um mapa |
| `Assign(map)` | Adicionar chaves calculadas a um mapa de entrada |
| `Pick(chaves...)` | Selecionar chaves de um mapa |
| `Passthrough[T]()` | Passar a entrada adiante |
| `Branch(default, cases...)` | Escolher um ramo por condição (`runnables.Case{When, Then}`) |
| `Map(r)` | Aplicar um runnable a cada item de uma lista |
| `WithRetry(r, opts)` | Repetir com backoff exponencial |
| `WithFallbacks(r, fb...)` | Tentar alternativas quando falha |
| `WithConfigured(r, cfg)` | Fixar uma `Config` |
| `Erase(r)` / `Sequence(...)` | Trabalhar com `Runnable[any, any]` |

### Exemplo: chain de RAG com `Lambda`

```go
ragChain := runnables.Pipe4[string, prompts.Values, []messages.Message, messages.Message, string](
	runnables.Lambda("contexto", func(ctx context.Context, q string) (prompts.Values, error) {
		docs, err := retriever.Invoke(ctx, q)
		return prompts.Values{"question": q, "context": retrievers.FormatDocuments(docs)}, err
	}),
	prompts.Chat(
		[2]string{"system", "Responda só com base no contexto:\n{context}"},
		[2]string{"human", "{question}"},
	),
	models.AsRunnable(m),
	outputparsers.Str{},
)
answer, err := ragChain.Invoke(ctx, "Quem criou Go?")
```

O exemplo executável completo, com `Parallel`/`Assign`, está em `examples/langchain/rag_chain`.

## 4.5 Retry e fallbacks

```go
robust := runnables.WithRetry(chain, runnables.RetryOptions{
	MaxAttempts:     4,                      // tentativas totais (padrão 3)
	InitialInterval: 500 * time.Millisecond, // padrão 500ms
	BackoffFactor:   2,                      // padrão 2
	MaxInterval:     30 * time.Second,       // padrão 30s
	Jitter:          true,
	RetryIf: func(err error) bool { return !errors.Is(err, context.Canceled) },
})
safe := runnables.WithFallbacks(robust, backupChain)
```

`RetryIf` nulo significa "repetir qualquer erro". `runnables.Backoff(ctx, opts, fn)` aplica a mesma política a uma função qualquer.

## 4.6 Saída estruturada

Faz o modelo devolver uma struct Go:

```go
type Person struct {
	Name string `json:"name" description:"Nome completo"`
	Age  int    `json:"age"`
}

extract := models.Structured[Person](m, models.MethodTool) // ou models.MethodJSONSchema
p, err := extract.Invoke(ctx, []messages.Message{messages.Human("Ana Souza tem 31 anos.")})
// p == Person{Name: "Ana Souza", Age: 31}
```

- `MethodTool` (padrão): força uma chamada de tool cujos argumentos são a saída. Funciona em qualquer modelo com tools.
- `MethodJSONSchema`: usa o recurso nativo de JSON Schema do provedor.
- As tags `description` e `jsonschema` da struct enriquecem o schema (ver [capítulo 5](05-tools.md#52-schema-dos-argumentos)).

Para agents, use `agents.Options.ResponseFormat` (capítulo 9).

## 4.7 Histórico de chat (`core/chathistory`)

Memória de conversa para chains simples (sem LangGraph):

```go
store := &chathistory.Store{} // um chathistory.Memory por sessão; defina New para outro tipo
withMem := chathistory.WithHistory(models.AsRunnable(m), store.Get)

ctx := runnables.WithConfig(ctx, runnables.Config{
	Configurable: map[string]any{chathistory.SessionKey: "sessao-1"},
})
resp, err := withMem.Invoke(ctx, []messages.Message{messages.Human("Meu nome é Ana.")})
resp, err = withMem.Invoke(ctx, []messages.Message{messages.Human("Qual é o meu nome?")})
```

Implementações de `chathistory.History`: `chathistory.Memory` (em memória), `chathistory.File` (arquivo JSON) e `chathistory.Store` (um histórico por ID de sessão). Para agents e grafos, prefira checkpointers ([capítulo 8](08-persistencia-hitl-streaming.md)).

## 4.8 Utilidades de mensagens para controlar contexto

```go
trimmed := messages.Trim(msgs, messages.TrimOptions{
	MaxTokens:     4000,
	Strategy:      "last",        // mantém as mais recentes
	IncludeSystem: true,          // preserva o system inicial
	StartOn:       messages.RoleHuman,
})
only := messages.Filter(msgs, []messages.Role{messages.RoleHuman, messages.RoleAI}, nil)
merged := messages.MergeRuns(msgs)
```

A contagem padrão é aproximada (`ApproxTokens`); passe `Counter` com um contador exato se precisar.

---

Anterior: [3. Modelos e provedores](03-modelos-e-provedores.md) · Próximo: [5. Tools](05-tools.md) · [Índice do manual](README.pt-BR.md)
