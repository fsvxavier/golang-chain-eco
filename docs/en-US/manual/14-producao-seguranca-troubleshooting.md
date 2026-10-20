# 14. Production, security and troubleshooting

**English** | [Português](../../pt-BR/manual/14-producao-seguranca-troubleshooting.md) | [Español](../../es-ES/manual/14-producao-seguranca-troubleshooting.md)

## 14.1 Production checklist

**Persistence**

- [ ] Durable checkpointer (`sqlsaver`, Redis, MongoDB, DynamoDB, S3), never `checkpoint.NewMemory()`.
- [ ] `thread_id` derived from the authenticated user/session, not accepted raw from the client (prevents reading another user's conversation).
- [ ] Custom state types registered with `checkpoint.Register`.
- [ ] Retention policy: `saver.DeleteThread(ctx, id)` to delete old conversations.

**Cost and stability**

- [ ] `ModelCallLimit` and `ToolCallLimit` on the agent.
- [ ] Explicit `RecursionLimit` in `runnables.Config`.
- [ ] `ModelRetry`/`ToolRetry` and `ModelFallback` for transient errors.
- [ ] `Summarization` or `ContextEditing` for long conversations.
- [ ] `models.NewRateLimiter` per provider, respecting the account quotas.
- [ ] `context.WithTimeout` on every call coming from an HTTP request.

**Observability**

- [ ] An active tracer (LangSmith, Langfuse or OTLP) with `defer Close()`.
- [ ] The tracer's `OnError` connected to a log.
- [ ] Token usage and latency per model/tool in metrics.

**Security**

- [ ] Secrets in environment variables or a vault.
- [ ] Sensitive tools protected by `HumanInTheLoop`.
- [ ] `PII` active when there is personal data.
- [ ] No `LocalShell` in production; use `kubernetes.Sandbox` or an isolated runner.
- [ ] `Permissions` configured in Deep Agents with a real disk.

## 14.2 Shutdown and context

Every method takes a `context.Context`. Cancelling the context interrupts the provider call, the streaming and the graph. In HTTP servers, propagate `r.Context()`:

```go
func handler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	ctx = runnables.WithConfig(ctx, graph.Thread(userThread(r)))
	out, err := agent.Invoke(ctx, graph.State{"messages": readBody(r)})
	...
}
```

When the process shuts down: stop accepting requests, wait for the ones in progress, then call `Close()` on the tracers and close connections.

## 14.3 Concurrency

- `*graph.Compiled`, models and tools are safe for concurrent use and should be **created once** and reused across requests.
- Graph nodes run in goroutines: do not share mutable maps or slices between nodes without synchronization. The state handed to a node is a copy.
- `callbacks.Handler` must be safe for concurrency.
- Each thread (`thread_id`) must have **one execution at a time**. Two simultaneous calls on the same thread compete for the same checkpoint; serialize them in your code (a queue per thread) or use `Multitask` on the remote server.

## 14.4 Security

### Code execution

| Component | Risk | Mitigation |
|---|---|---|
| `backends.LocalShell` | Runs commands on the **host**, with no sandbox | Only in a trusted environment; `AllowedCommands` restricts the programs; in production use `kubernetes.Sandbox` or another isolated runner via `backends.Remote` |
| `mcp.Stdio` | Starts a process with your permissions | Only trusted servers; pin versions; restrict `Env` |
| `backends.Filesystem` | Disk access | Confined to `Root` (blocks `..` and escaping symlinks); set `Permissions` |

### Prompt injection

Content coming from web pages, documents, tool results or emails may contain malicious instructions. Principles:

1. Give the agent only the tools it needs.
2. Require human approval (`HumanInTheLoop`) for irreversible actions or ones with external effect (sending, paying, deleting, executing).
3. Restrict destinations and scopes (`messaging.Allow`, read-only SOQL, Kubernetes namespaces).
4. Apply permissions **at the data layer** (`spicedb.FilterRetriever`, `permit.FilterRetriever`), not trusting the prompt to "not reveal".
5. Treat the model's output as untrusted input before using it in SQL, shell or HTML.
6. Documents retrieved by RAG, CAG or KAG are also untrusted input: a passage may contain instructions. Filter by permission at retrieval time (`Question.Filter`, item 4) and never paste model text into queries: graph relation types and labels are validated (`^[A-Z][A-Z0-9_]*$`) before they reach Cypher.

### SSRF

`search.Fetcher` refuses loopback and private networks by default, even after resolving DNS. Do not enable `AllowPrivate` in code exposed to user input.

### Secrets

- Do not log `Authorization`, API keys or the body of prompts with personal data.
- Tracing and provider keys via the environment. `remote/server` uses a shared `APIKey`: put TLS and real authentication in front of it.

## 14.5 Performance

- Reuse the `http.Client` (`HTTP` in the provider and tracer options) for keep-alive.
- `models.WithCache` avoids repeating identical calls (useful in tests and evaluations).
- `embeddings.NewCached` avoids re-embedding the same texts.
- Use `Parallel`, parallel branches and `Send` for independent I/O (several searches, several documents).
- For vector volume, replace `vectorstores.Memory` (linear search) with a backend that has an index.
- Streaming (`ModeMessages`) improves perceived latency in chats.

## 14.6 Tests

```bash
go test -race ./...                                 # everything; tests for real services skip themselves (no network)
go test ./internal/sqlitetest/...                   # sqlsaver against real SQLite (modernc.org/sqlite driver)
PG_DSN=postgres://... go test ./internal/pgtest/... # PostgreSQL, MariaDB, CockroachDB, Oracle
```

Tests that need real services **skip themselves** when the environment variable is not set (`REDIS_ADDR`, `MONGO_ADDR`, `DYNAMODB_ENDPOINT`, `S3_ENDPOINT`, `SOLR_URL`, `CLICKHOUSE_URL`, `K8S_SERVER`...). To start the services, see [INTEGRATIONS.md](../INTEGRATIONS.md#testing-against-real-services).

For **your** tests:

- `models.NewFake(...)` with scripted responses; check `fake.Calls`.
- `embeddings.Fake{Dim: 64}` for deterministic RAG.
- `httptest.NewServer` to simulate providers (use `BaseURL`).
- Test the HITL flow: invoke, check `graph.Interrupts(out)`, resume with `graph.Command{Resume: ...}`.

## 14.7 Troubleshooting

| Symptom | Likely cause | Solution |
|---|---|---|
| `Init` returns a provider/key error | `<PROVIDER>_API_KEY` variable missing or wrong prefix | Check `chatmodels.Providers()` and the variables in chapter 1 |
| Error asking for `<PROVIDER>_BASE_URL` | Per-tenant provider (Databricks, Snowflake, gateways) | Set the variable with the endpoint |
| `*graph.GraphRecursionError` | Loop went past the limit (default 25) | Raise `RecursionLimit` if legitimate; otherwise fix the exit condition |
| The agent does not remember the conversation | No checkpointer or no `graph.Thread(id)` in the context | `Compile: graph.WithCheckpointer(...)` and `runnables.WithConfig(ctx, graph.Thread(id))` |
| `Interrupt` does not pause / does not resume | No checkpointer, or a different `thread_id` on resume | Use the same `thread_id`; send `graph.Command{Resume: ...}` |
| Side effect executed twice after resuming | The node re-executes from the beginning | Move the action after the `Interrupt` or make it idempotent |
| State comes back as `float64` or `map[string]any` instead of your type | JSON serialization of the checkpoint | `checkpoint.Register("name", Type{})` |
| Number comes back as `float64` (or `int`) instead of the expected type | Numbers without an explicit type come back as `int` or `float64`; Go scalars such as `int64` and `float32` are already preserved | Use the scalar type in the state, or `checkpoint.Register` for a custom type; `checkpoint.Strict(true)` flags unregistered structs |
| Writes from parallel branches are lost/conflict | Channel without a reducer | Use `graph.Append()`, `graph.Sum()` or `channels.BinOp` |
| Tool is never called | Vague description, confusing schema or model without tool support | Improve the name/description/`description` of the fields; test with another model |
| Tool called with invalid arguments | Permissive schema | Use `jsonschema:"enum=..."`, `minimum`, `required`; return `tools.Errorf` explaining |
| Single block response on Bedrock | Binary stream not decoded | Expected: `Stream` returns the full response |
| `human_in_the_loop` fails to pause | No checkpointer | Pass `Compile: []graph.CompileOption{graph.WithCheckpointer(s)}` |
| `execute` disappears in a Deep Agent | A `Deny` rule exists in `Permissions` | Intentional behavior: a shell would bypass the rules |
| Vector store returns nothing | Different embedder at ingestion and query time, or index not created yet | Use the same embedder/dimension; create the index first (Pinecone, Atlas) |
| Tracing events do not appear | `Close()` missing or full queue | `defer tr.Close()`; configure `OnError` and increase `Buffer` |
| `mongodb+srv://` does not work | Not supported | Pass `host:port` |
| Provider 429 rate limit | Quotas | `models.NewRateLimiter`, `ModelRetry` with backoff, `ModelFallback` |
| Context exceeds the model limit | Long history | `Summarization`, `ContextEditing` or `messages.Trim` |

## 14.8 Known limitations

- The state is `map[string]any`, not TypedDict/Pydantic. Use `tools.Decode` to bind into structs.
- In `sqlsaver`, Go scalars (`int64`, `float32`, `time.Time`, `[]byte`…) come back with their original type; numbers without an explicit type come back as `int` or `float64`; custom types require `checkpoint.Register`.
- In `graph.Interrupt`, the node re-executes from the beginning (as in Python LangGraph). Subgraphs propagate interrupts to the parent; when several nodes of a subgraph stop in the same super-step, the pauses appear and the answers are delivered in task order (without a checkpointer in the subgraph, that super-step runs in sequence during the resume).
- Not ported from the Python version: Studio, CLI build/deploy, LangSmith managed backends and hosted experiments. Offline evaluations: the `evals` package ([EVALS.md](../EVALS.md)); development server: `langgraph/dev` ([DEV.md](../DEV.md)).
- No native PDF parser (use `Unstructured` or convert first).
- RAG/CAG/KAG ([chapter 20](20-rag-cag-kag.md)): incremental indexing keeps hashes only in memory; CAG depends on the provider's cache (it expires and charges for the first write) and estimates tokens (~4 characters per token); the graph adapters score `FindEntities` in Go. Full list in [KNOWLEDGE.md](../KNOWLEDGE.md).
- `Flat` and `Annoy` are pure-Go equivalents of FAISS/Annoy. `LoadAnnoyFile` and `LoadFAISSFlat` read the native formats (Annoy angular/euclidean; FAISS flat and IDMap over flat), verified by tests against files generated by the official libraries (see [INTEGRATIONS.md](../INTEGRATIONS.md)).
- Paid SaaS/API integrations have only contract tests; validate them in your environment.
- 123 providers on LangChain's list depend on API documentation that is unavailable ([PROVIDERS-ANALISE.md](../PROVIDERS-ANALISE.md)).

## 14.9 Evolution and compatibility

The module requires Go 1.27+ and the production code does not import third-party dependencies (the drivers in `go.mod` are test-only), so updating means changing the version in `go.mod` (`go get github.com/fsvxavier/golang-chain-eco@latest`) and running `go build ./... && go test -race ./...`. Read the repository history before larger updates, since the API tracks the Python ecosystem, which evolves quickly.

---

Previous: [13. Integrations](13-integracoes.md) · Next: [15. Examples and recipes](15-exemplos-e-receitas.md) · [Manual index](README.md)
