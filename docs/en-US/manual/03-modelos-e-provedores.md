# 3. Models and providers

**English** | [Português](../../pt-BR/manual/03-modelos-e-provedores.md) | [Español](../../es-ES/manual/03-modelos-e-provedores.md)

## 3.1 The `ChatModel` interface

Every provider implements `models.ChatModel`:

```go
type ChatModel interface {
	Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error)
	Stream(ctx context.Context, msgs []messages.Message, opts ...Option) iter.Seq2[messages.Message, error]
	ModelID() string // "provider:model"
}
```

Since the interface is the same for all of them, switching providers means changing the string passed to `Init`.

## 3.2 Creating a model: `chatmodels.Init`

```go
import (
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

m, err := chatmodels.Init("openai:gpt-4o-mini", models.WithTemperature(0))
m2 := chatmodels.MustInit("ollama:llama3.1")
m3 := chatmodels.MustInit("groq:llama-3.3-70b-versatile") // reads GROQ_API_KEY
```

- The string has the format `provider:model`.
- Without a prefix, `gpt-*`, `o1`, `o3` and `o4` go to OpenAI and `claude*` to Anthropic.
- Options passed to `Init` become the defaults for every call.
- `Init` returns a clear error for an unknown provider, a missing key or a per-tenant provider without `<PROVIDER>_BASE_URL`.
- `chatmodels.Providers()` lists the accepted prefixes.

## 3.3 Generating and streaming

```go
resp, err := m.Generate(ctx, []messages.Message{
	messages.System("Be brief."),
	messages.Human("Hi"),
})
fmt.Println(resp.Text())

for chunk, err := range m.Stream(ctx, msgs) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(chunk.Text())
}
```

The chunks from `Stream` are partial messages. To get the complete message from a stream, use `models.Collect(seq)`, or join them with `Message.Merge` and call `Finalize()`.

## 3.4 Per-call options

Passed to `Generate`/`Stream` or as defaults in `Init`/`models.Bind`:

| Option | Effect |
|---|---|
| `models.WithModel(name)` | Switches the model |
| `models.WithTemperature(t)` | Temperature |
| `models.WithTopP(p)` | Top-p |
| `models.WithMaxTokens(n)` | Output token limit |
| `models.WithStop(s...)` | Stop sequences |
| `models.WithTools(ts...)` | Makes tools available |
| `models.WithToolChoice(c)` | Forces or forbids tool use |
| `models.WithResponseFormat(rf)` | Requests JSON / JSON Schema |
| `models.WithExtra(k, v)` | Provider-specific parameter |

## 3.5 Available providers

**Native clients** (`providers/*`): `openai`, `azure_openai`, `anthropic`, `google_genai` (and `google_vertexai`), `cohere`, `bedrock`, `ollama`, `ibm`/`watsonx`, `replicate`.

| Provider | Prefix | Chat | Tools | Stream | Embeddings |
|---|---|---|---|---|---|
| OpenAI | `openai` | yes | yes | SSE | yes |
| Azure OpenAI | `azure_openai` | yes | yes | SSE | yes |
| Anthropic | `anthropic` | yes | yes | SSE | no |
| Google Gemini | `google_genai` / `google` | yes | yes | SSE | yes |
| Google Vertex AI | `google_vertexai` | yes | yes | SSE | no |
| Cohere | `cohere` | yes | yes | SSE | yes + rerank |
| AWS Bedrock (Converse) | `bedrock` | yes | yes | single response | no |
| Ollama | `ollama` | yes | yes | NDJSON | yes |
| IBM watsonx.ai | `ibm` / `watsonx` | yes | yes | SSE | yes |
| Replicate | `replicate` | text | no | single response | no |

> **Warning (Bedrock):** the binary stream `vnd.amazon.eventstream` is not decoded; `Stream` returns the full response at once.

**OpenAI-compatible**: dozens of services use the same client. The key comes from `$<PROVIDER>_API_KEY`:

`groq`, `together`, `openrouter`, `deepseek`, `xai`, `mistralai`, `fireworks`, `perplexity`, `cerebras`, `nvidia`, `sambanova`, `nebius`, `novita`, `deepinfra`, `hyperbolic`, `huggingface`, `moonshot`, `zhipuai`, `dashscope`, `upstage`, `writer`, `ai21`, `baseten`, `github`, `sarvam`, `telnyx`, and the local servers `vllm`, `lmstudio`, `llamacpp`, `localai`, `litellm`.

**Per tenant** (no default URL; set `$<PROVIDER>_BASE_URL`): `databricks`, `cloudflare`, `snowflake`, `runpod`, `truefoundry`, `kserve`, `rayserve` and third-party gateways.

The full matrix of LangChain's 504 providers is in [PROVIDERS.md](../PROVIDERS.md).

## 3.6 Your own endpoint (Azure, vLLM, LM Studio, proxies)

Any service compatible with the Chat Completions API can be used by building `openai.Chat` directly:

```go
import "github.com/fsvxavier/golang-chain-eco/providers/openai"

local := &openai.Chat{
	Model:   "qwen2.5",
	BaseURL: "http://localhost:8000/v1",
	APIKey:  "x",
}
resp, err := local.Generate(ctx, msgs)
```

For Azure OpenAI there is `openai.Azure`. IBM watsonx.ai reuses the same client with `Path`, `StreamPath`, `Query`, `TokenFunc` (per-request token) and `Rewrite` (edits the body).

### 3.6.1 Router across gateways

To spread calls over several gateways and providers, use `router:<model>`. The environment decides the targets, the order and how the first one is picked:

```bash
export ROUTER_TARGETS=freellmapi,omniroute,bifrost,anthropic,openai
export ROUTER_MODE=failover
export FREELLMAPI_BASE_URL=http://localhost:PORT/v1
export OMNIROUTE_BASE_URL=http://localhost:PORT/v1
export BIFROST_BASE_URL=http://localhost:PORT/v1
export FREELLMAPI_API_KEY=...
export OMNIROUTE_API_KEY=...
export BIFROST_API_KEY=...
export ANTHROPIC_API_KEY=...
export OPENAI_API_KEY=...
export ROUTER_MODEL_ANTHROPIC=claude-sonnet-4-5
export ROUTER_TIMEOUT=60s
```

```go
m, err := chatmodels.Init("router:gpt-4o-mini") // same model on every target unless ROUTER_MODEL_<NAME> overrides it
resp, err := m.Generate(ctx, msgs)
```

Retry rules and streaming continuation are the same as in [PROVIDERS.md](../PROVIDERS.md#router).

## 3.7 Utilities that wrap any model

```go
cached  := models.WithCache(m, &models.MemoryCache{})   // repeated responses
bound   := models.BindTools(m, weatherTool)             // fixed tools
rl      := models.NewRateLimiter(5, 10)                 // token bucket: req/s, burst
limited := models.WithRateLimit(m, rl)                  // waits on the limiter before each call
bound2  := models.Bind(m, models.WithTemperature(0))    // default options
base    := models.Unwrap(bound2)                        // back to the original model
```

- `WithCache` uses the `models.Cache` interface (`Lookup`/`Update`); `MemoryCache` is the in-memory implementation. The key is derived from the messages and options.
- `RateLimiter.Wait(ctx)` blocks until there is permission or the context is cancelled.

## 3.8 Fake model for tests

`models.Fake` answers with predefined messages, without network. Ideal for unit tests and demos.

```go
fake := models.NewFake(
	messages.AI("", messages.ToolCall{ID: "1", Name: "get_weather", Args: map[string]any{"city": "Lisbon"}}),
	messages.AI("It is sunny in Lisbon."),
)
agent, _ := agents.Create(agents.Options{Model: fake, Tools: []tools.Tool{weather}})
```

Useful `Fake` fields: `Responses` (consumed in order), `Cycle` (restarts when it runs out), `Func` (a response computed from the messages) and `Calls` (a record of what the model received, for assertions).

## 3.9 Embeddings

```go
emb, err := chatmodels.InitEmbeddings("openai:text-embedding-3-small")
// or "cohere:embed-v4.0", "ollama:nomic-embed-text", "google_genai:...", "voyageai:...", "jina:..."

vecs, err := emb.EmbedDocuments(ctx, []string{"text 1", "text 2"})
q, err := emb.EmbedQuery(ctx, "question")
```

Utilities in `core/embeddings`:

- `embeddings.Fake{Dim: 64}`: deterministic embedder (hashed bag-of-words) for tests.
- `embeddings.NewCached(e)`: memoizes repeated embeddings.
- `embeddings.Cosine(a, b)` and `embeddings.Normalize(v)`.

## 3.10 Rerank

```go
rr, err := chatmodels.NewReranker("cohere:rerank-v3.5")
// or "jina:jina-reranker-v2-base-multilingual", "voyageai:rerank-2"
```

Use with `retrieval.WithRerank(retriever, rr)` (chapter 6).

## 3.11 Choosing a provider

| Need | Suggestion |
|---|---|
| Local development, no cost | `ollama:...` or `vllm`/`lmstudio` with `openai.Chat{BaseURL: ...}` |
| Automated tests | `models.Fake` |
| Lowest inference latency | `groq`, `cerebras`, `fireworks` |
| Many models with one key | `openrouter`, `together` |
| Corporate compliance on AWS/Azure | `bedrock`, `azure_openai` |
| Very long context | Check the model limits with the provider |

## 3.12 Token usage and prompt cache

`Message.Usage` carries `InputTokens`, `OutputTokens` and `TotalTokens`. `InputTokens` sums everything, including what came from the cache. Two extra fields show the prompt cache:
`CacheReadTokens` (read from the cache) and `CacheCreationTokens` (written to the cache). Anthropic reports both; OpenAI and compatible ones report `CacheReadTokens`; the others leave them at zero.
To take advantage of the cache with a large corpus, see CAG in [chapter 20](20-rag-cag-kag.md).

---

Previous: [2. Core concepts](02-conceitos.md) · Next: [4. Prompts, chains and parsers](04-chains-prompts-parsers.md) · [Manual index](README.md)
