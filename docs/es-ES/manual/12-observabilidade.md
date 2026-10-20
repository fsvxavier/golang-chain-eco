# 12. Observabilidad

[English](../../en-US/manual/12-observabilidade.md) | [Português](../../pt-BR/manual/12-observabilidade.md) | **Español**

Toda ejecución (chains, modelos, tools, retrievers, nodos de grafo) emite **eventos de callback**. Los handlers colocados en el `context.Context` reciben esos eventos, y los tracers (LangSmith, Langfuse, OpenTelemetry) son handlers.

## 12.1 Handlers

```go
import "github.com/fsvxavier/golang-chain-eco/core/callbacks"

ctx := callbacks.WithHandlers(context.Background(), h1, h2)
agent.Invoke(ctx, input) // h1 y h2 reciben todo lo que pasa dentro
```

`WithHandlers` **suma** a los handlers que ya estén en el contexto. Los handlers también pueden pasarse en `runnables.Config{Callbacks: ...}`.

Handlers listos:

| Handler | Uso |
|---|---|
| `callbacks.StdOut(os.Stderr)` | Imprime los eventos en la consola (depuración local) |
| `callbacks.Logger(slog.Default())` | Envía los eventos a `log/slog` |
| `callbacks.NewTracer()` | Registra el árbol de runs en memoria (`Roots()`, `JSON()`) |
| `callbacks.HandlerFunc(fn)` | Adapta una función |

## 12.2 Eventos

`callbacks.Event` trae `Kind`, `RunID`, `ParentRunID`, `Name`, `Tags`, `Metadata`, `Input`, `Output`, `Chunk` (token en streaming), `Err` y `Time`.

| Familia | Kinds |
|---|---|
| Chain / nodo | `ChainStart`, `ChainEnd`, `ChainError` |
| Modelo | `ModelStart`, `ModelNewToken`, `ModelEnd`, `ModelError` |
| Tool | `ToolStart`, `ToolEnd`, `ToolError` |
| Retriever | `RetrieverStart`, `RetrieverEnd`, `RetrieverError` |
| Personalizado | `Custom` |

## 12.3 Handler propio: métricas de tokens y latencia

```go
metrics := callbacks.HandlerFunc(func(ctx context.Context, e callbacks.Event) {
	switch e.Kind {
	case callbacks.ModelEnd:
		if msg, ok := e.Output.(messages.Message); ok && msg.Usage != nil {
			log.Printf("modelo=%s run=%s uso=%+v", e.Name, e.RunID, msg.Usage)
		}
	case callbacks.ToolError:
		log.Printf("tool %s falló: %v", e.Name, e.Err)
	}
})
ctx := callbacks.WithHandlers(ctx, metrics)
```

> Las implementaciones de `Handler` deben ser **seguras para uso concurrente**: los nodos de un grafo corren en goroutines.

## 12.4 Instrumentar tu código

Dentro de un nodo o tool, abre un run propio para que aparezca en el árbol:

```go
ctx, end := callbacks.Start(ctx, callbacks.ChainStart, "calcular-envio", input)
res, err := calcular(ctx, input)
end(res, err)
```

Eventos sueltos: `callbacks.Emit(ctx, callbacks.Event{Kind: callbacks.Custom, Name: "cache_hit"})`.

## 12.5 LangSmith

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/langsmith"

tr := langsmith.New(langsmith.Options{Project: "mi-proyecto"}) // LANGSMITH_API_KEY en el entorno
defer tr.Close()                                               // vacía la cola

ctx := callbacks.WithHandlers(context.Background(), tr)
agent.Invoke(ctx, input) // chains, modelos, tools y nodos aparecen como runs anidadas
```

| Opción | Predeterminado |
|---|---|
| `APIKey` | `$LANGSMITH_API_KEY` / `$LANGCHAIN_API_KEY` |
| `Endpoint` | `$LANGSMITH_ENDPOINT` o `https://api.smith.langchain.com` |
| `Project` | `$LANGSMITH_PROJECT` o `default` |
| `HTTP` | `*http.Client` personalizado (proxy, timeout) |
| `OnError` | Callback para fallos de envío |
| `Buffer` | Tamaño de la cola de eventos (1024) |

El envío es **asíncrono y de mejor esfuerzo**: los fallos de red nunca tiran el programa y, con la cola llena, los eventos se descartan (se informan en `OnError`). **Llama a `Close()`** antes de terminar el proceso; si no, se pierden los eventos pendientes.

## 12.6 Langfuse

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/langfuse"

tr := langfuse.New(langfuse.Options{}) // LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY, LANGFUSE_HOST
defer tr.Close()
ctx := callbacks.WithHandlers(ctx, tr)
```

`Host` predeterminado: `$LANGFUSE_HOST` o `https://cloud.langfuse.com`. También acepta `HTTP`, `OnError` y `Buffer`.

## 12.7 OpenTelemetry (OTLP/HTTP)

Exporta spans a Jaeger, Tempo, Honeycomb, Datadog, New Relic, Grafana y cualquier colector OTLP:

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/otlp"

ex := otlp.New(otlp.Options{
	Endpoint:    "http://localhost:4318", // envía a <Endpoint>/v1/traces
	Headers:     map[string]string{"x-honeycomb-team": os.Getenv("HONEYCOMB_KEY")},
	ServiceName: "mi-servicio",           // predeterminado: golang-chain-eco
})
defer ex.Close()
ctx = callbacks.WithHandlers(ctx, ex)
```

El `Endpoint` predeterminado viene de `$OTEL_EXPORTER_OTLP_ENDPOINT` o `http://localhost:4318`.

## 12.8 Varios destinos al mismo tiempo

```go
ctx := callbacks.WithHandlers(ctx, smith, otel, callbacks.Logger(slog.Default()))
```

## 12.9 Depuración local rápida

```go
ctx := callbacks.WithHandlers(ctx, callbacks.StdOut(os.Stderr))
```

o, para inspeccionar el árbol después:

```go
tr := callbacks.NewTracer()
agent.Invoke(callbacks.WithHandlers(ctx, tr), input)
fmt.Println(tr.JSON())
```

Para depurar la estructura de un grafo, usa `app.Mermaid()` y `app.GetStateHistory(ctx, n)` (capítulo 8).

## 12.10 Qué registrar en producción

- `RunID` y `thread_id` en todos los logs de una solicitud, para correlacionarlos.
- Uso de tokens (`Message.Usage`) por modelo y por tenant, para el costo. Con caché de prompt (CAG), suma también `CacheReadTokens` y `CacheCreationTokens`: las lecturas de caché suelen costar menos que la entrada normal, y las escrituras pueden costar más.
- Latencia y errores por tool.
- Evita registrar el contenido de prompts y respuestas si hay datos personales; combínalo con el middleware `PII` (capítulo 9) o filtra en el handler.

---

Anterior: [11. MCP, servidor y grafos remotos](11-mcp-remote-servidor.md) · Siguiente: [13. Integraciones](13-integracoes.md) · [Índice del manual](README.es-ES.md)
