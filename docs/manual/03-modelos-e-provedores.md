# 3. Modelos e provedores

## 3.1 A interface `ChatModel`

Todo provedor implementa `models.ChatModel`:

```go
type ChatModel interface {
	Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error)
	Stream(ctx context.Context, msgs []messages.Message, opts ...Option) iter.Seq2[messages.Message, error]
	ModelID() string // "provedor:modelo"
}
```

Como a interface é a mesma para todos, trocar de provedor é trocar a string de `Init`.

## 3.2 Criar um modelo: `chatmodels.Init`

```go
import (
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

m, err := chatmodels.Init("openai:gpt-4o-mini", models.WithTemperature(0))
m2 := chatmodels.MustInit("ollama:llama3.1")
m3 := chatmodels.MustInit("groq:llama-3.3-70b-versatile") // lê GROQ_API_KEY
```

- A string tem o formato `provedor:modelo`.
- Sem prefixo, `gpt-*`, `o1`, `o3` e `o4` vão para OpenAI e `claude*` para Anthropic.
- Opções passadas a `Init` viram padrões de todas as chamadas.
- `Init` devolve erro claro para provedor desconhecido, chave ausente ou provedor por tenant sem `<PROVEDOR>_BASE_URL`.
- `chatmodels.Providers()` lista os prefixos aceitos.

## 3.3 Gerar e fazer streaming

```go
resp, err := m.Generate(ctx, []messages.Message{
	messages.System("Seja breve."),
	messages.Human("Oi"),
})
fmt.Println(resp.Text())

for chunk, err := range m.Stream(ctx, msgs) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(chunk.Text())
}
```

Os chunks de `Stream` são mensagens parciais. Para obter a mensagem completa de um stream, use `models.Collect(seq)`, ou junte com `Message.Merge` e chame `Finalize()`.

## 3.4 Opções por chamada

Passadas a `Generate`/`Stream` ou como padrões em `Init`/`models.Bind`:

| Opção | Efeito |
|---|---|
| `models.WithModel(nome)` | Troca o modelo |
| `models.WithTemperature(t)` | Temperatura |
| `models.WithTopP(p)` | Top-p |
| `models.WithMaxTokens(n)` | Limite de tokens de saída |
| `models.WithStop(s...)` | Sequências de parada |
| `models.WithTools(ts...)` | Disponibiliza tools |
| `models.WithToolChoice(c)` | Força ou proíbe uso de tool |
| `models.WithResponseFormat(rf)` | Pede JSON / JSON Schema |
| `models.WithExtra(k, v)` | Parâmetro específico do provedor |

## 3.5 Provedores disponíveis

**Clientes nativos** (`providers/*`): `openai`, `azure_openai`, `anthropic`, `google_genai` (e `google_vertexai`), `cohere`, `bedrock`, `ollama`, `ibm`/`watsonx`, `replicate`.

| Provedor | Prefixo | Chat | Tools | Stream | Embeddings |
|---|---|---|---|---|---|
| OpenAI | `openai` | sim | sim | SSE | sim |
| Azure OpenAI | `azure_openai` | sim | sim | SSE | sim |
| Anthropic | `anthropic` | sim | sim | SSE | não |
| Google Gemini | `google_genai` / `google` | sim | sim | SSE | sim |
| Google Vertex AI | `google_vertexai` | sim | sim | SSE | não |
| Cohere | `cohere` | sim | sim | SSE | sim + rerank |
| AWS Bedrock (Converse) | `bedrock` | sim | sim | resposta única | não |
| Ollama | `ollama` | sim | sim | NDJSON | sim |
| IBM watsonx.ai | `ibm` / `watsonx` | sim | sim | SSE | sim |
| Replicate | `replicate` | texto | não | resposta única | não |

> **Atenção (Bedrock):** o stream binário `vnd.amazon.eventstream` não é decodificado; `Stream` devolve a resposta completa de uma vez.

**Compatíveis com OpenAI**: dezenas de serviços usam o mesmo cliente. A chave vem de `$<PROVEDOR>_API_KEY`:

`groq`, `together`, `openrouter`, `deepseek`, `xai`, `mistralai`, `fireworks`, `perplexity`, `cerebras`, `nvidia`, `sambanova`, `nebius`, `novita`, `deepinfra`, `hyperbolic`, `huggingface`, `moonshot`, `zhipuai`, `dashscope`, `upstage`, `writer`, `ai21`, `baseten`, `github`, `sarvam`, `telnyx`, e servidores locais `vllm`, `lmstudio`, `llamacpp`, `localai`, `litellm`.

**Por tenant** (sem URL padrão; defina `$<PROVEDOR>_BASE_URL`): `databricks`, `cloudflare`, `snowflake`, `runpod`, `truefoundry`, `kserve`, `rayserve` e gateways de terceiros.

A matriz completa dos 504 provedores da LangChain está em [PROVIDERS.md](../PROVIDERS.md).

## 3.6 Endpoint próprio (Azure, vLLM, LM Studio, proxies)

Qualquer serviço compatível com a API Chat Completions entra construindo `openai.Chat` diretamente:

```go
import "github.com/fsvxavier/golang-chain-eco/providers/openai"

local := &openai.Chat{
	Model:   "qwen2.5",
	BaseURL: "http://localhost:8000/v1",
	APIKey:  "x",
}
resp, err := local.Generate(ctx, msgs)
```

Para Azure OpenAI existe `openai.Azure`. O IBM watsonx.ai reutiliza o mesmo cliente com `Path`, `StreamPath`, `Query`, `TokenFunc` (token por requisição) e `Rewrite` (edita o corpo).

## 3.7 Utilitários que envolvem qualquer modelo

```go
cached  := models.WithCache(m, &models.MemoryCache{})   // respostas repetidas
bound   := models.BindTools(m, weatherTool)             // tools fixas
rl      := models.NewRateLimiter(5, 10)                 // token bucket: req/s, burst
limited := models.WithRateLimit(m, rl)                  // espera o limiter antes de cada chamada
bound2  := models.Bind(m, models.WithTemperature(0))    // opções padrão
base    := models.Unwrap(bound2)                        // volta ao modelo original
```

- `WithCache` usa a interface `models.Cache` (`Lookup`/`Update`); `MemoryCache` é a implementação em memória. A chave é derivada das mensagens e opções.
- `RateLimiter.Wait(ctx)` bloqueia até haver permissão ou o contexto ser cancelado.

## 3.8 Modelo fake para testes

`models.Fake` responde com mensagens pré-definidas, sem rede. Ideal para testes unitários e demos.

```go
fake := models.NewFake(
	messages.AI("", messages.ToolCall{ID: "1", Name: "get_weather", Args: map[string]any{"city": "Lisboa"}}),
	messages.AI("Está ensolarado em Lisboa."),
)
agent, _ := agents.Create(agents.Options{Model: fake, Tools: []tools.Tool{weather}})
```

Campos úteis de `Fake`: `Responses` (consumidas em ordem), `Cycle` (reinicia ao acabar), `Func` (resposta calculada a partir das mensagens) e `Calls` (registro do que o modelo recebeu, para asserções).

## 3.9 Embeddings

```go
emb, err := chatmodels.InitEmbeddings("openai:text-embedding-3-small")
// ou "cohere:embed-v4.0", "ollama:nomic-embed-text", "google_genai:...", "voyageai:...", "jina:..."

vecs, err := emb.EmbedDocuments(ctx, []string{"texto 1", "texto 2"})
q, err := emb.EmbedQuery(ctx, "pergunta")
```

Utilitários em `core/embeddings`:

- `embeddings.Fake{Dim: 64}`: embedder determinístico (bag-of-words por hash) para testes.
- `embeddings.NewCached(e)`: memoiza embeddings repetidos.
- `embeddings.Cosine(a, b)` e `embeddings.Normalize(v)`.

## 3.10 Rerank

```go
rr, err := chatmodels.NewReranker("cohere:rerank-v3.5")
// ou "jina:jina-reranker-v2-base-multilingual", "voyageai:rerank-2"
```

Use com `retrieval.WithRerank(retriever, rr)` (capítulo 6).

## 3.11 Escolhendo um provedor

| Necessidade | Sugestão |
|---|---|
| Desenvolvimento local, sem custo | `ollama:...` ou `vllm`/`lmstudio` com `openai.Chat{BaseURL: ...}` |
| Testes automatizados | `models.Fake` |
| Menor latência de inferência | `groq`, `cerebras`, `fireworks` |
| Muitos modelos com uma chave | `openrouter`, `together` |
| Compliance corporativo na AWS/Azure | `bedrock`, `azure_openai` |
| Contexto muito longo | Consulte os limites do modelo no provedor |

## 3.12 Uso de tokens e cache de prompt

`Message.Usage` traz `InputTokens`, `OutputTokens` e `TotalTokens`. `InputTokens` soma tudo, inclusive o que veio do cache. Dois campos extras mostram o cache de prompt:
`CacheReadTokens` (lidos do cache) e `CacheCreationTokens` (gravados no cache). O Anthropic informa os dois; OpenAI e compatíveis informam `CacheReadTokens`; os demais deixam zero.
Para aproveitar o cache com um corpus grande, veja o CAG no [capítulo 20](20-rag-cag-kag.md).

---

Anterior: [2. Conceitos fundamentais](02-conceitos.md) · Próximo: [4. Prompts, chains e parsers](04-chains-prompts-parsers.md) · [Índice do manual](README.md)
