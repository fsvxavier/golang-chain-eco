# 12. Observabilidade

[English](../../en-US/manual/12-observabilidade.md) | **Português** | [Español](../../es-ES/manual/12-observabilidade.md)

Toda execução (chains, modelos, tools, retrievers, nós de grafo) emite **eventos de callback**. Handlers colocados no `context.Context` recebem esses eventos, e os tracers (LangSmith, Langfuse, OpenTelemetry) são handlers.

## 12.1 Handlers

```go
import "github.com/fsvxavier/golang-chain-eco/core/callbacks"

ctx := callbacks.WithHandlers(context.Background(), h1, h2)
agent.Invoke(ctx, input) // h1 e h2 recebem tudo que acontece dentro
```

`WithHandlers` **soma** aos handlers já presentes no contexto. Handlers também podem ser passados em `runnables.Config{Callbacks: ...}`.

Handlers prontos:

| Handler | Uso |
|---|---|
| `callbacks.StdOut(os.Stderr)` | Imprime os eventos no console (depuração local) |
| `callbacks.Logger(slog.Default())` | Envia eventos ao `log/slog` |
| `callbacks.NewTracer()` | Registra a árvore de runs em memória (`Roots()`, `JSON()`) |
| `callbacks.HandlerFunc(fn)` | Adapta uma função |

## 12.2 Eventos

`callbacks.Event` traz `Kind`, `RunID`, `ParentRunID`, `Name`, `Tags`, `Metadata`, `Input`, `Output`, `Chunk` (token em streaming), `Err` e `Time`.

| Família | Kinds |
|---|---|
| Chain / nó | `ChainStart`, `ChainEnd`, `ChainError` |
| Modelo | `ModelStart`, `ModelNewToken`, `ModelEnd`, `ModelError` |
| Tool | `ToolStart`, `ToolEnd`, `ToolError` |
| Retriever | `RetrieverStart`, `RetrieverEnd`, `RetrieverError` |
| Customizado | `Custom` |

## 12.3 Handler próprio: métricas de tokens e latência

```go
metrics := callbacks.HandlerFunc(func(ctx context.Context, e callbacks.Event) {
	switch e.Kind {
	case callbacks.ModelEnd:
		if msg, ok := e.Output.(messages.Message); ok && msg.Usage != nil {
			log.Printf("modelo=%s run=%s uso=%+v", e.Name, e.RunID, msg.Usage)
		}
	case callbacks.ToolError:
		log.Printf("tool %s falhou: %v", e.Name, e.Err)
	}
})
ctx := callbacks.WithHandlers(ctx, metrics)
```

> Implementações de `Handler` devem ser **seguras para uso concorrente**: nós de um grafo rodam em goroutines.

## 12.4 Instrumentar seu código

Dentro de um nó ou tool, abra um run próprio para aparecer na árvore:

```go
ctx, end := callbacks.Start(ctx, callbacks.ChainStart, "calcular-frete", input)
res, err := calcular(ctx, input)
end(res, err)
```

Eventos avulsos: `callbacks.Emit(ctx, callbacks.Event{Kind: callbacks.Custom, Name: "cache_hit"})`.

## 12.5 LangSmith

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/langsmith"

tr := langsmith.New(langsmith.Options{Project: "meu-projeto"}) // LANGSMITH_API_KEY no ambiente
defer tr.Close()                                                // descarrega a fila

ctx := callbacks.WithHandlers(context.Background(), tr)
agent.Invoke(ctx, input) // chains, modelos, tools e nós aparecem como runs aninhadas
```

| Opção | Padrão |
|---|---|
| `APIKey` | `$LANGSMITH_API_KEY` / `$LANGCHAIN_API_KEY` |
| `Endpoint` | `$LANGSMITH_ENDPOINT` ou `https://api.smith.langchain.com` |
| `Project` | `$LANGSMITH_PROJECT` ou `default` |
| `HTTP` | `*http.Client` customizado (proxy, timeout) |
| `OnError` | Callback para falhas de envio |
| `Buffer` | Tamanho da fila de eventos (1024) |

O envio é **assíncrono e best-effort**: falhas de rede nunca derrubam o programa e, com a fila cheia, eventos são descartados (reportados em `OnError`). **Chame `Close()`** antes de encerrar o processo, senão eventos pendentes se perdem.

## 12.6 Langfuse

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/langfuse"

tr := langfuse.New(langfuse.Options{}) // LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY, LANGFUSE_HOST
defer tr.Close()
ctx := callbacks.WithHandlers(ctx, tr)
```

`Host` padrão: `$LANGFUSE_HOST` ou `https://cloud.langfuse.com`. Também aceita `HTTP`, `OnError` e `Buffer`.

## 12.7 OpenTelemetry (OTLP/HTTP)

Exporta spans para Jaeger, Tempo, Honeycomb, Datadog, New Relic, Grafana e qualquer coletor OTLP:

```go
import "github.com/fsvxavier/golang-chain-eco/tracing/otlp"

ex := otlp.New(otlp.Options{
	Endpoint:    "http://localhost:4318", // envia para <Endpoint>/v1/traces
	Headers:     map[string]string{"x-honeycomb-team": os.Getenv("HONEYCOMB_KEY")},
	ServiceName: "meu-servico",           // padrão: golang-chain-eco
})
defer ex.Close()
ctx = callbacks.WithHandlers(ctx, ex)
```

O `Endpoint` padrão vem de `$OTEL_EXPORTER_OTLP_ENDPOINT` ou `http://localhost:4318`.

## 12.8 Vários destinos ao mesmo tempo

```go
ctx := callbacks.WithHandlers(ctx, smith, otel, callbacks.Logger(slog.Default()))
```

## 12.9 Depuração local rápida

```go
ctx := callbacks.WithHandlers(ctx, callbacks.StdOut(os.Stderr))
```

ou, para inspecionar a árvore depois:

```go
tr := callbacks.NewTracer()
agent.Invoke(callbacks.WithHandlers(ctx, tr), input)
fmt.Println(tr.JSON())
```

Para depurar a estrutura de um grafo, use `app.Mermaid()` e `app.GetStateHistory(ctx, n)` (capítulo 8).

## 12.10 O que registrar em produção

- `RunID` e `thread_id` em todos os logs de uma requisição, para correlacionar.
- Uso de tokens (`Message.Usage`) por modelo e por tenant, para custo. Com cache de prompt (CAG), some também `CacheReadTokens` e `CacheCreationTokens`: leitura de cache costuma custar menos que entrada normal, e escrita pode custar mais.
- Latência e erros por tool.
- Evite registrar o conteúdo de prompts e respostas se houver dados pessoais; combine com o middleware `PII` (capítulo 9) ou filtre no handler.

---

Anterior: [11. MCP, servidor e graphs remotos](11-mcp-remote-servidor.md) · Próximo: [13. Integrações](13-integracoes.md) · [Índice do manual](README.md)
