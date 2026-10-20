# 12. Observability

**English** | [Português](../../pt-BR/manual/12-observabilidade.md) | [Español](../../es-ES/manual/12-observabilidade.md)

Every execution (chains, models, tools, retrievers, graph nodes) emits **callback events**. Handlers placed in the `context.Context` receive these events, and the tracers (LangSmith, Langfuse, OpenTelemetry) are handlers.

## 12.1 Handlers

```go
import "github.com/fsvxavier/golang-chain-eco/core/callbacks"

ctx := callbacks.WithHandlers(context.Background(), h1, h2)
agent.Invoke(ctx, input) // h1 and h2 receive everything that happens inside
```

`WithHandlers` **adds** to the handlers already present in the context. Handlers can also be passed in `runnables.Config{Callbacks: ...}`.

Ready-made handlers:

| Handler | Use |
|---|---|
| `callbacks.StdOut(os.Stderr)` | Prints events to the console (local debugging) |
| `callbacks.Logger(slog.Default())` | Sends events to `log/slog` |
| `callbacks.NewTracer()` | Records the tree of runs in memory (`Roots()`, `JSON()`) |
| `callbacks.HandlerFunc(fn)` | Adapts a function |

## 12.2 Events

`callbacks.Event` carries `Kind`, `RunID`, `ParentRunID`, `Name`, `Tags`, `Metadata`, `Input`, `Output`, `Chunk` (streaming token), `Err` and `Time`.

| Family | Kinds |
|---|---|
| Chain / node | `ChainStart`, `ChainEnd`, `ChainError` |
| Model | `ModelStart`, `ModelNewToken`, `ModelEnd`, `ModelError` |
| Tool | `ToolStart`, `ToolEnd`, `ToolError` |
| Retriever | `RetrieverStart`, `RetrieverEnd`, `RetrieverError` |
| Custom | `Custom` |

## 12.3 Your own handler: token and latency metrics

```go
metrics := callbacks.HandlerFunc(func(ctx context.Context, e callbacks.Event) {
	switch e.Kind {
	case callbacks.ModelEnd:
		if msg, ok := e.Output.(messages.Message); ok && msg.Usage != nil {
			log.Printf("model=%s run=%s usage=%+v", e.Name, e.RunID, msg.Usage)
		}
	case callbacks.ToolError:
		log.Printf("tool %s failed: %v", e.Name, e.Err)
	}
})
ctx := callbacks.WithHandlers(ctx, metrics)
```

> Implementations of `Handler` must be **safe for concurrent use**: graph nodes run in goroutines.

## 12.4 Instrumenting your code

Inside a node or tool, open your own run so it shows up in the tree:

```go
ctx, end := callbacks.Start(ctx, callbacks.ChainStart, "calculate-shipping", input)
res, err := calculate(ctx, input)
end(res, err)
```

Standalone events: `callbacks.Emit(ctx, callbacks.Event{Kind: callbacks.Custom, Name: "cache_hit"})`.

## 12.5 LangSmith

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/langsmith"

tr := langsmith.New(langsmith.Options{Project: "my-project"}) // LANGSMITH_API_KEY in the environment
defer tr.Close()                                              // flushes the queue

ctx := callbacks.WithHandlers(context.Background(), tr)
agent.Invoke(ctx, input) // chains, models, tools and nodes appear as nested runs
```

| Option | Default |
|---|---|
| `APIKey` | `$LANGSMITH_API_KEY` / `$LANGCHAIN_API_KEY` |
| `Endpoint` | `$LANGSMITH_ENDPOINT` or `https://api.smith.langchain.com` |
| `Project` | `$LANGSMITH_PROJECT` or `default` |
| `HTTP` | Custom `*http.Client` (proxy, timeout) |
| `OnError` | Callback for send failures |
| `Buffer` | Size of the event queue (1024) |

Sending is **asynchronous and best-effort**: network failures never bring the program down and, with the queue full, events are dropped (reported in `OnError`). **Call `Close()`** before exiting the process, otherwise pending events are lost.

## 12.6 Langfuse

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/langfuse"

tr := langfuse.New(langfuse.Options{}) // LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY, LANGFUSE_HOST
defer tr.Close()
ctx := callbacks.WithHandlers(ctx, tr)
```

Default `Host`: `$LANGFUSE_HOST` or `https://cloud.langfuse.com`. Also accepts `HTTP`, `OnError` and `Buffer`.

## 12.7 OpenTelemetry (OTLP/HTTP)

Exports spans to Jaeger, Tempo, Honeycomb, Datadog, New Relic, Grafana and any OTLP collector:

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/otlp"

ex := otlp.New(otlp.Options{
	Endpoint:    "http://localhost:4318", // sends to <Endpoint>/v1/traces
	Headers:     map[string]string{"x-honeycomb-team": os.Getenv("HONEYCOMB_KEY")},
	ServiceName: "my-service",            // default: golang-chain-eco
})
defer ex.Close()
ctx = callbacks.WithHandlers(ctx, ex)
```

The default `Endpoint` comes from `$OTEL_EXPORTER_OTLP_ENDPOINT` or `http://localhost:4318`.

## 12.8 Several destinations at the same time

```go
ctx := callbacks.WithHandlers(ctx, smith, otel, callbacks.Logger(slog.Default()))
```

## 12.9 Quick local debugging

```go
ctx := callbacks.WithHandlers(ctx, callbacks.StdOut(os.Stderr))
```

or, to inspect the tree afterwards:

```go
tr := callbacks.NewTracer()
agent.Invoke(callbacks.WithHandlers(ctx, tr), input)
fmt.Println(tr.JSON())
```

To debug the structure of a graph, use `app.Mermaid()` and `app.GetStateHistory(ctx, n)` (chapter 8).

## 12.10 What to record in production

- `RunID` and `thread_id` in all the logs of a request, to correlate them.
- Token usage (`Message.Usage`) per model and per tenant, for cost. With prompt cache (CAG), also add `CacheReadTokens` and `CacheCreationTokens`: cache reads usually cost less than normal input, and writes may cost more.
- Latency and errors per tool.
- Avoid recording the content of prompts and responses if they contain personal data; combine with the `PII` middleware (chapter 9) or filter in the handler.

---

Previous: [11. MCP, server and remote graphs](11-mcp-remote-servidor.md) · Next: [13. Integrations](13-integracoes.md) · [Manual index](README.md)
