# 4. Prompts, chains and parsers

**English** | [Português](../../pt-BR/manual/04-chains-prompts-parsers.md) | [Español](../../es-ES/manual/04-chains-prompts-parsers.md)

## 4.1 Prompt templates (`core/prompts`)

### Simple template

```go
p := prompts.New("Explain {topic} to a {audience}.")
txt, err := p.Format(prompts.Values{"topic": "goroutines", "audience": "beginner"})

p.InputVariables()                 // ["topic", "audience"]
p2 := p.Partial(prompts.Values{"audience": "beginner"}) // fixes variables
```

`prompts.Values` is `map[string]any`. Variables use `{name}` braces.

### Chat template

`prompts.Chat` accepts `[2]string{role, template}` pairs, `MessageTemplate`, static messages and placeholders:

```go
prompt := prompts.Chat(
	[2]string{"system", "Answer in one sentence."},
	prompts.Placeholder{Name: "history", Optional: true}, // injects []messages.Message
	[2]string{"human", "Explain {topic}."},
)
msgs, err := prompt.FormatMessages(prompts.Values{"topic": "channels"})
```

Accepted roles: `system`, `human`/`user`, `ai`/`assistant`, `placeholder`. Helpers: `prompts.SystemTemplate`, `HumanTemplate`, `AITemplate`.

### Few-shot

`prompts.FewShot` (text) and `prompts.FewShotChat` (messages) assemble examples from a list of `prompts.Example`. `prompts.LengthSelector` selects how many examples fit within a size limit (`ExampleSelector`).

## 4.2 Composing with `Pipe`

`Pipe3` is the typed equivalent of `prompt | model | parser`:

```go
chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](
	prompt,                  // Values -> []Message
	models.AsRunnable(m),    // []Message -> Message
	outputparsers.Str{},     // Message -> string
)
out, err := chain.Invoke(ctx, prompts.Values{"topic": "goroutines"})
```

The type parameters are `[Input, T1, T2, Output]`. `Pipe` joins 2 steps, `Pipe3` 3, `Pipe4` 4; for more, chain pipes together.

### Streaming a chain

```go
for tok, err := range runnables.Stream[prompts.Values, string](ctx, chain, prompts.Values{"topic": "channels"}) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(tok)
}
```

### Batch

```go
outs, err := runnables.Batch(ctx, chain, []prompts.Values{{"topic": "a"}, {"topic": "b"}})
```

`Config.MaxConcurrency` limits the parallelism.

## 4.3 Output parsers (`core/outputparsers`)

| Parser | Input → output |
|---|---|
| `outputparsers.Str{}` | `Message` → `string` |
| `outputparsers.JSON{}` | `Message` → `any` (decoded JSON; usually `map[string]any`) |
| `outputparsers.Struct[T]{}` | `Message` → `T` (decodes JSON into a struct) |
| `outputparsers.CommaList{}` | `Message` → `[]string` |
| `outputparsers.ToolCalls{}` | `Message` → tool calls |
| `outputparsers.ToolArgs[T]{Name: "..."}` | `Message` → `T` arguments of a specific tool |

## 4.4 Combinators (`core/runnables`)

| Function | Purpose |
|---|---|
| `Lambda(name, fn)` | Turn a Go function into a runnable |
| `Parallel(map)` | Run branches in parallel over the same input and join them into a map |
| `Assign(map)` | Add computed keys to an input map |
| `Pick(keys...)` | Select keys from a map |
| `Passthrough[T]()` | Pass the input along |
| `Branch(default, cases...)` | Choose a branch by condition (`runnables.Case{When, Then}`) |
| `Map(r)` | Apply a runnable to each item of a list |
| `WithRetry(r, opts)` | Repeat with exponential backoff |
| `WithFallbacks(r, fb...)` | Try alternatives when it fails |
| `WithConfigured(r, cfg)` | Fix a `Config` |
| `Erase(r)` / `Sequence(...)` | Work with `Runnable[any, any]` |

### Example: RAG chain with `Lambda`

```go
ragChain := runnables.Pipe4[string, prompts.Values, []messages.Message, messages.Message, string](
	runnables.Lambda("context", func(ctx context.Context, q string) (prompts.Values, error) {
		docs, err := retriever.Invoke(ctx, q)
		return prompts.Values{"question": q, "context": retrievers.FormatDocuments(docs)}, err
	}),
	prompts.Chat(
		[2]string{"system", "Answer only based on the context:\n{context}"},
		[2]string{"human", "{question}"},
	),
	models.AsRunnable(m),
	outputparsers.Str{},
)
answer, err := ragChain.Invoke(ctx, "Who created Go?")
```

The complete runnable example, with `Parallel`/`Assign`, is in `examples/langchain/rag_chain`.

## 4.5 Retry and fallbacks

```go
robust := runnables.WithRetry(chain, runnables.RetryOptions{
	MaxAttempts:     4,                      // total attempts (default 3)
	InitialInterval: 500 * time.Millisecond, // default 500ms
	BackoffFactor:   2,                      // default 2
	MaxInterval:     30 * time.Second,       // default 30s
	Jitter:          true,
	RetryIf: func(err error) bool { return !errors.Is(err, context.Canceled) },
})
safe := runnables.WithFallbacks(robust, backupChain)
```

A nil `RetryIf` means "retry any error". `runnables.Backoff(ctx, opts, fn)` applies the same policy to any function.

## 4.6 Structured output

Makes the model return a Go struct:

```go
type Person struct {
	Name string `json:"name" description:"Full name"`
	Age  int    `json:"age"`
}

extract := models.Structured[Person](m, models.MethodTool) // or models.MethodJSONSchema
p, err := extract.Invoke(ctx, []messages.Message{messages.Human("Ana Souza is 31 years old.")})
// p == Person{Name: "Ana Souza", Age: 31}
```

- `MethodTool` (default): forces a tool call whose arguments are the output. Works with any model that has tools.
- `MethodJSONSchema`: uses the provider's native JSON Schema feature.
- The `description` and `jsonschema` tags of the struct enrich the schema (see [chapter 5](05-tools.md#52-argument-schema)).

For agents, use `agents.Options.ResponseFormat` (chapter 9).

## 4.7 Chat history (`core/chathistory`)

Conversation memory for simple chains (without LangGraph):

```go
store := &chathistory.Store{} // one chathistory.Memory per session; set New for another type
withMem := chathistory.WithHistory(models.AsRunnable(m), store.Get)

ctx := runnables.WithConfig(ctx, runnables.Config{
	Configurable: map[string]any{chathistory.SessionKey: "session-1"},
})
resp, err := withMem.Invoke(ctx, []messages.Message{messages.Human("My name is Ana.")})
resp, err = withMem.Invoke(ctx, []messages.Message{messages.Human("What is my name?")})
```

Implementations of `chathistory.History`: `chathistory.Memory` (in memory), `chathistory.File` (JSON file) and `chathistory.Store` (one history per session ID). For agents and graphs, prefer checkpointers ([chapter 8](08-persistencia-hitl-streaming.md)).

## 4.8 Message utilities for controlling context

```go
trimmed := messages.Trim(msgs, messages.TrimOptions{
	MaxTokens:     4000,
	Strategy:      "last",        // keeps the most recent
	IncludeSystem: true,          // preserves the initial system message
	StartOn:       messages.RoleHuman,
})
only := messages.Filter(msgs, []messages.Role{messages.RoleHuman, messages.RoleAI}, nil)
merged := messages.MergeRuns(msgs)
```

The default count is approximate (`ApproxTokens`); pass `Counter` with an exact counter if you need one.

---

Previous: [3. Models and providers](03-modelos-e-provedores.md) · Next: [5. Tools](05-tools.md) · [Manual index](README.md)
