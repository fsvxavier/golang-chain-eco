// Package chatmodels resolves "provider:model" strings into chat models
// (init_chat_model), embedders (init_embeddings) and rerankers.
//
// Native providers: openai, azure_openai, anthropic, google_genai (alias
// google), google_vertexai, cohere, bedrock (alias bedrock_converse), ollama.
// Every entry of OpenAICompatible is served through the OpenAI client; set
// $<PROVIDER>_API_KEY and optionally $<PROVIDER>_BASE_URL.
package chatmodels

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/anthropic"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
	"github.com/fsvxavier/golang-chain-eco/providers/cohere"
	"github.com/fsvxavier/golang-chain-eco/providers/google"
	"github.com/fsvxavier/golang-chain-eco/providers/ollama"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

// OpenAICompatible maps provider names to their default OpenAI-style
// endpoints. Local servers (vllm, lmstudio, llamacpp, localai, litellm) need
// no key.
var OpenAICompatible = map[string]string{
	"groq":        "https://api.groq.com/openai/v1",
	"together":    "https://api.together.xyz/v1",
	"openrouter":  "https://openrouter.ai/api/v1",
	"deepseek":    "https://api.deepseek.com/v1",
	"xai":         "https://api.x.ai/v1",
	"mistralai":   "https://api.mistral.ai/v1",
	"fireworks":   "https://api.fireworks.ai/inference/v1",
	"perplexity":  "https://api.perplexity.ai",
	"cerebras":    "https://api.cerebras.ai/v1",
	"nvidia":      "https://integrate.api.nvidia.com/v1",
	"sambanova":   "https://api.sambanova.ai/v1",
	"nebius":      "https://api.studio.nebius.com/v1",
	"novita":      "https://api.novita.ai/v3/openai",
	"deepinfra":   "https://api.deepinfra.com/v1/openai",
	"hyperbolic":  "https://api.hyperbolic.xyz/v1",
	"huggingface": "https://router.huggingface.co/v1",
	"moonshot":    "https://api.moonshot.ai/v1",
	"zhipuai":     "https://open.bigmodel.cn/api/paas/v4",
	"dashscope":   "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
	"upstage":     "https://api.upstage.ai/v1",
	"writer":      "https://api.writer.com/v1",
	"ai21":        "https://api.ai21.com/studio/v1",
	"baseten":     "https://inference.baseten.co/v1",
	"github":      "https://models.github.ai/inference",
	"voyageai":    "https://api.voyageai.com/v1", // embeddings
	"jina":        "https://api.jina.ai/v1",      // embeddings
	"nomic":       "https://api-atlas.nomic.ai/v1",
	"vllm":        "http://localhost:8000/v1",
	"lmstudio":    "http://localhost:1234/v1",
	"llamacpp":    "http://localhost:8080/v1",
	"localai":     "http://localhost:8080/v1",
	"litellm":     "http://localhost:4000/v1",
}

// Providers lists every provider name Init understands.
func Providers() []string {
	names := []string{"openai", "azure_openai", "anthropic", "google_genai", "google", "google_vertexai", "cohere", "bedrock", "bedrock_converse", "ollama", "fake"}
	for k := range OpenAICompatible {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Init builds a chat model from "provider:model". Without a prefix the
// provider is inferred from the model name (gpt-*/o* → openai, claude-* →
// anthropic, gemini-* → google_genai, command* → cohere).
func Init(spec string, defaults ...models.Option) (models.ChatModel, error) {
	provider, model, ok := strings.Cut(spec, ":")
	if !ok {
		model = spec
		switch {
		case strings.HasPrefix(spec, "claude"):
			provider = "anthropic"
		case strings.HasPrefix(spec, "gpt"), strings.HasPrefix(spec, "o1"), strings.HasPrefix(spec, "o3"), strings.HasPrefix(spec, "o4"):
			provider = "openai"
		case strings.HasPrefix(spec, "gemini"):
			provider = "google_genai"
		case strings.HasPrefix(spec, "command"):
			provider = "cohere"
		default:
			return nil, fmt.Errorf("chatmodels: cannot infer provider for %q; use provider:model", spec)
		}
	}
	switch provider {
	case "openai":
		return openai.New(model, defaults...), nil
	case "azure_openai":
		c := openai.New(model, defaults...)
		c.Azure = &openai.Azure{Deployment: model}
		return c, nil
	case "anthropic":
		return anthropic.New(model, defaults...), nil
	case "google_genai", "google":
		return google.New(model, defaults...), nil
	case "google_vertexai":
		c := google.New(model, defaults...)
		c.Vertex = &google.Vertex{Token: envToken("GOOGLE_OAUTH_ACCESS_TOKEN")}
		return c, nil
	case "cohere":
		return cohere.New(model, defaults...), nil
	case "bedrock", "bedrock_converse":
		return bedrock.New(model, defaults...), nil
	case "ollama":
		return ollama.New(model, defaults...), nil
	case "fake":
		return &models.Fake{Name: spec}, nil
	}
	if base, ok := OpenAICompatible[provider]; ok {
		c := openai.New(model, defaults...)
		c.BaseURL, c.APIKey = baseURL(provider, base), envKey(provider)
		return c, nil
	}
	return nil, fmt.Errorf("chatmodels: unknown provider %q (known: %s)", provider, strings.Join(Providers(), ", "))
}

// MustInit is Init that panics on error.
func MustInit(spec string, defaults ...models.Option) models.ChatModel {
	m, err := Init(spec, defaults...)
	if err != nil {
		panic(err)
	}
	return m
}

// InitEmbeddings builds an embedder from "provider:model": openai,
// azure_openai, google_genai, cohere, ollama, fake, or any OpenAI-compatible
// provider that serves /embeddings (voyageai, jina, mistralai, together,
// fireworks, nomic, ...).
func InitEmbeddings(spec string) (embeddings.Embedder, error) {
	provider, model, _ := strings.Cut(spec, ":")
	switch provider {
	case "openai":
		return &openai.Embeddings{Model: model}, nil
	case "azure_openai":
		return &openai.Embeddings{Model: model, Chat: openai.Chat{Azure: &openai.Azure{Deployment: model}}}, nil
	case "google_genai", "google":
		return &google.Embeddings{Model: model}, nil
	case "cohere":
		return &cohere.Embeddings{Model: model}, nil
	case "ollama":
		return &ollama.Embeddings{Model: model}, nil
	case "fake":
		return embeddings.Fake{}, nil
	}
	if base, ok := OpenAICompatible[provider]; ok {
		return &openai.Embeddings{Model: model, Chat: openai.Chat{BaseURL: baseURL(provider, base), APIKey: envKey(provider)}}, nil
	}
	return nil, fmt.Errorf("chatmodels: unknown embeddings provider %q", provider)
}

// NewReranker builds a reranker for "cohere:<model>", "jina:<model>" or
// "voyageai:<model>".
func NewReranker(spec string) (*cohere.Reranker, error) {
	provider, model, _ := strings.Cut(spec, ":")
	switch provider {
	case "cohere":
		return &cohere.Reranker{Model: model}, nil
	case "jina", "voyageai":
		return &cohere.Reranker{Model: model, BaseURL: baseURL(provider, OpenAICompatible[provider]), APIKey: envKey(provider), Path: "/rerank"}, nil
	}
	return nil, fmt.Errorf("chatmodels: unknown reranker provider %q", provider)
}

func baseURL(provider, dflt string) string {
	if v := os.Getenv(strings.ToUpper(provider) + "_BASE_URL"); v != "" {
		return v
	}
	return dflt
}

func envKey(provider string) string {
	return os.Getenv(strings.ToUpper(provider) + "_API_KEY")
}

func envToken(name string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		if t := os.Getenv(name); t != "" {
			return t, nil
		}
		return "", fmt.Errorf("chatmodels: set $%s to an OAuth2 access token (e.g. `gcloud auth print-access-token`)", name)
	}
}
