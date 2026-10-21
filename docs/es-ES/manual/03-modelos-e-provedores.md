# 3. Modelos y proveedores

[English](../../en-US/manual/03-modelos-e-provedores.md) | [Português](../../pt-BR/manual/03-modelos-e-provedores.md) | **Español**

## 3.1 La interfaz `ChatModel`

Todo proveedor implementa `models.ChatModel`:

```go
type ChatModel interface {
	Generate(ctx context.Context, msgs []messages.Message, opts ...Option) (messages.Message, error)
	Stream(ctx context.Context, msgs []messages.Message, opts ...Option) iter.Seq2[messages.Message, error]
	ModelID() string // "proveedor:modelo"
}
```

Como la interfaz es la misma para todos, cambiar de proveedor es cambiar la cadena que se pasa a `Init`.

## 3.2 Crear un modelo: `chatmodels.Init`

```go
import (
	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

m, err := chatmodels.Init("openai:gpt-4o-mini", models.WithTemperature(0))
m2 := chatmodels.MustInit("ollama:llama3.1")
m3 := chatmodels.MustInit("groq:llama-3.3-70b-versatile") // lee GROQ_API_KEY
```

- La cadena tiene el formato `proveedor:modelo`.
- Sin prefijo, `gpt-*`, `o1`, `o3` y `o4` van a OpenAI y `claude*` a Anthropic.
- Las opciones pasadas a `Init` se convierten en los predeterminados de todas las llamadas.
- `Init` devuelve un error claro para un proveedor desconocido, una clave ausente o un proveedor por tenant sin `<PROVEEDOR>_BASE_URL`.
- `chatmodels.Providers()` lista los prefijos aceptados.

## 3.3 Generar y hacer streaming

```go
resp, err := m.Generate(ctx, []messages.Message{
	messages.System("Sé breve."),
	messages.Human("Hola"),
})
fmt.Println(resp.Text())

for chunk, err := range m.Stream(ctx, msgs) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(chunk.Text())
}
```

Los chunks de `Stream` son mensajes parciales. Para obtener el mensaje completo de un stream, usa `models.Collect(seq)`, o únelos con `Message.Merge` y llama a `Finalize()`.

## 3.4 Opciones por llamada

Se pasan a `Generate`/`Stream` o como predeterminados en `Init`/`models.Bind`:

| Opción | Efecto |
|---|---|
| `models.WithModel(nombre)` | Cambia el modelo |
| `models.WithTemperature(t)` | Temperatura |
| `models.WithTopP(p)` | Top-p |
| `models.WithMaxTokens(n)` | Límite de tokens de salida |
| `models.WithStop(s...)` | Secuencias de parada |
| `models.WithTools(ts...)` | Pone tools a disposición |
| `models.WithToolChoice(c)` | Fuerza o prohíbe el uso de tools |
| `models.WithResponseFormat(rf)` | Pide JSON / JSON Schema |
| `models.WithExtra(k, v)` | Parámetro específico del proveedor |

## 3.5 Proveedores disponibles

**Clientes nativos** (`providers/*`): `openai`, `azure_openai`, `anthropic`, `google_genai` (y `google_vertexai`), `cohere`, `bedrock`, `ollama`, `ibm`/`watsonx`, `replicate`.

| Proveedor | Prefijo | Chat | Tools | Stream | Embeddings |
|---|---|---|---|---|---|
| OpenAI | `openai` | sí | sí | SSE | sí |
| Azure OpenAI | `azure_openai` | sí | sí | SSE | sí |
| Anthropic | `anthropic` | sí | sí | SSE | no |
| Google Gemini | `google_genai` / `google` | sí | sí | SSE | sí |
| Google Vertex AI | `google_vertexai` | sí | sí | SSE | no |
| Cohere | `cohere` | sí | sí | SSE | sí + rerank |
| AWS Bedrock (Converse) | `bedrock` | sí | sí | respuesta única | no |
| Ollama | `ollama` | sí | sí | NDJSON | sí |
| IBM watsonx.ai | `ibm` / `watsonx` | sí | sí | SSE | sí |
| Replicate | `replicate` | texto | no | respuesta única | no |

> **Atención (Bedrock):** el stream binario `vnd.amazon.eventstream` no se decodifica; `Stream` devuelve la respuesta completa de una vez.

**Compatibles con OpenAI**: decenas de servicios usan el mismo cliente. La clave viene de `$<PROVEEDOR>_API_KEY`:

`groq`, `together`, `openrouter`, `deepseek`, `xai`, `mistralai`, `fireworks`, `perplexity`, `cerebras`, `nvidia`, `sambanova`, `nebius`, `novita`, `deepinfra`, `hyperbolic`, `huggingface`, `moonshot`, `zhipuai`, `dashscope`, `upstage`, `writer`, `ai21`, `baseten`, `github`, `sarvam`, `telnyx`, y los servidores locales `vllm`, `lmstudio`, `llamacpp`, `localai`, `litellm`.

**Por tenant** (sin URL predeterminada; define `$<PROVEEDOR>_BASE_URL`): `databricks`, `cloudflare`, `snowflake`, `runpod`, `truefoundry`, `kserve`, `rayserve` y gateways de terceros.

La matriz completa de los 504 proveedores de LangChain está en [PROVIDERS.md](../PROVIDERS.md).

## 3.6 Endpoint propio (Azure, vLLM, LM Studio, proxies)

Cualquier servicio compatible con la API Chat Completions se usa construyendo `openai.Chat` directamente:

```go
import "github.com/fsvxavier/golang-chain-eco/providers/openai"

local := &openai.Chat{
	Model:   "qwen2.5",
	BaseURL: "http://localhost:8000/v1",
	APIKey:  "x",
}
resp, err := local.Generate(ctx, msgs)
```

Para Azure OpenAI existe `openai.Azure`. IBM watsonx.ai reutiliza el mismo cliente con `Path`, `StreamPath`, `Query`, `TokenFunc` (token por solicitud) y `Rewrite` (edita el cuerpo).

### 3.6.1 Router entre gateways

Para repartir llamadas entre varios gateways y proveedores, usa `router:<model>`. El entorno define los destinos, el orden y cómo se elige el primero:

```bash
export ROUTER_TARGETS=freellmapi,omniroute,bifrost,anthropic,openai
export ROUTER_MODE=failover
export FREELLMAPI_BASE_URL=http://localhost:PORT/v1
export OMNIROUTE_BASE_URL=http://localhost:PORT/v1
export BIFROST_BASE_URL=http://localhost:PORT/v1
export ANTHROPIC_API_KEY=...
export OPENAI_API_KEY=...
export ROUTER_MODEL_ANTHROPIC=claude-sonnet-4-5
export ROUTER_TIMEOUT=60s
```

```go
m, err := chatmodels.Init("router:gpt-4o-mini") // same model on every target unless ROUTER_MODEL_<NAME> overrides it
resp, err := m.Generate(ctx, msgs)
```

Las reglas de reintento y la continuación del streaming son las mismas de [PROVIDERS.md](../PROVIDERS.md#router).

## 3.7 Utilidades que envuelven cualquier modelo

```go
cached  := models.WithCache(m, &models.MemoryCache{})   // respuestas repetidas
bound   := models.BindTools(m, weatherTool)             // tools fijas
rl      := models.NewRateLimiter(5, 10)                 // token bucket: req/s, burst
limited := models.WithRateLimit(m, rl)                  // espera al limiter antes de cada llamada
bound2  := models.Bind(m, models.WithTemperature(0))    // opciones predeterminadas
base    := models.Unwrap(bound2)                        // vuelve al modelo original
```

- `WithCache` usa la interfaz `models.Cache` (`Lookup`/`Update`); `MemoryCache` es la implementación en memoria. La clave se deriva de los mensajes y las opciones.
- `RateLimiter.Wait(ctx)` bloquea hasta que haya permiso o el contexto se cancele.

## 3.8 Modelo fake para pruebas

`models.Fake` responde con mensajes predefinidos, sin red. Ideal para pruebas unitarias y demos.

```go
fake := models.NewFake(
	messages.AI("", messages.ToolCall{ID: "1", Name: "get_weather", Args: map[string]any{"city": "Lisboa"}}),
	messages.AI("Está soleado en Lisboa."),
)
agent, _ := agents.Create(agents.Options{Model: fake, Tools: []tools.Tool{weather}})
```

Campos útiles de `Fake`: `Responses` (se consumen en orden), `Cycle` (reinicia al terminarse), `Func` (respuesta calculada a partir de los mensajes) y `Calls` (registro de lo que recibió el modelo, para aserciones).

## 3.9 Embeddings

```go
emb, err := chatmodels.InitEmbeddings("openai:text-embedding-3-small")
// o "cohere:embed-v4.0", "ollama:nomic-embed-text", "google_genai:...", "voyageai:...", "jina:..."

vecs, err := emb.EmbedDocuments(ctx, []string{"texto 1", "texto 2"})
q, err := emb.EmbedQuery(ctx, "pregunta")
```

Utilidades en `core/embeddings`:

- `embeddings.Fake{Dim: 64}`: embedder determinístico (bag-of-words por hash) para pruebas.
- `embeddings.NewCached(e)`: memoriza embeddings repetidos.
- `embeddings.Cosine(a, b)` y `embeddings.Normalize(v)`.

## 3.10 Rerank

```go
rr, err := chatmodels.NewReranker("cohere:rerank-v3.5")
// o "jina:jina-reranker-v2-base-multilingual", "voyageai:rerank-2"
```

Úsalo con `retrieval.WithRerank(retriever, rr)` (capítulo 6).

## 3.11 Elegir un proveedor

| Necesidad | Sugerencia |
|---|---|
| Desarrollo local, sin costo | `ollama:...` o `vllm`/`lmstudio` con `openai.Chat{BaseURL: ...}` |
| Pruebas automatizadas | `models.Fake` |
| Menor latencia de inferencia | `groq`, `cerebras`, `fireworks` |
| Muchos modelos con una sola clave | `openrouter`, `together` |
| Cumplimiento corporativo en AWS/Azure | `bedrock`, `azure_openai` |
| Contexto muy largo | Consulta los límites del modelo en el proveedor |

## 3.12 Uso de tokens y caché de prompt

`Message.Usage` trae `InputTokens`, `OutputTokens` y `TotalTokens`. `InputTokens` suma todo, incluido lo que vino de la caché. Dos campos extra muestran la caché de prompt:
`CacheReadTokens` (leídos de la caché) y `CacheCreationTokens` (escritos en la caché). Anthropic informa los dos; OpenAI y compatibles informan `CacheReadTokens`; los demás dejan cero.
Para aprovechar la caché con un corpus grande, mira CAG en el [capítulo 20](20-rag-cag-kag.md).

---

Anterior: [2. Conceptos fundamentales](02-conceitos.md) · Siguiente: [4. Prompts, chains y parsers](04-chains-prompts-parsers.md) · [Índice del manual](README.es-ES.md)
