// Package chatmodels resolves "provider:model" strings into chat models
// (init_chat_model) and embedders (init_embeddings).
package chatmodels

import (
	"fmt"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/anthropic"
	"github.com/fsvxavier/golang-chain-eco/providers/ollama"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

// openAICompatible maps provider prefixes to OpenAI-compatible endpoints.
var openAICompatible = map[string]string{
	"groq":       "https://api.groq.com/openai/v1",
	"together":   "https://api.together.xyz/v1",
	"openrouter": "https://openrouter.ai/api/v1",
	"deepseek":   "https://api.deepseek.com/v1",
	"xai":        "https://api.x.ai/v1",
	"mistralai":  "https://api.mistral.ai/v1",
	"fireworks":  "https://api.fireworks.ai/inference/v1",
}

// Init builds a chat model from "provider:model". Without a prefix the
// provider is inferred from the model name (gpt-*/o* → openai, claude-* →
// anthropic). OpenAI-compatible providers read their key from
// $<PROVIDER>_API_KEY.
func Init(spec string, defaults ...models.Option) (models.ChatModel, error) {
	provider, model, ok := strings.Cut(spec, ":")
	if !ok {
		model = spec
		switch {
		case strings.HasPrefix(spec, "claude"):
			provider = "anthropic"
		case strings.HasPrefix(spec, "gpt"), strings.HasPrefix(spec, "o1"), strings.HasPrefix(spec, "o3"), strings.HasPrefix(spec, "o4"):
			provider = "openai"
		default:
			return nil, fmt.Errorf("chatmodels: cannot infer provider for %q; use provider:model", spec)
		}
	}
	switch provider {
	case "openai":
		return openai.New(model, defaults...), nil
	case "anthropic":
		return anthropic.New(model, defaults...), nil
	case "ollama":
		return ollama.New(model, defaults...), nil
	case "fake":
		return &models.Fake{Name: spec}, nil
	}
	if base, ok := openAICompatible[provider]; ok {
		c := openai.New(model, defaults...)
		c.BaseURL = base
		c.APIKey = envKey(provider)
		return c, nil
	}
	return nil, fmt.Errorf("chatmodels: unknown provider %q", provider)
}

// MustInit is Init that panics on error.
func MustInit(spec string, defaults ...models.Option) models.ChatModel {
	m, err := Init(spec, defaults...)
	if err != nil {
		panic(err)
	}
	return m
}

// InitEmbeddings builds an embedder from "provider:model" (openai, ollama, fake).
func InitEmbeddings(spec string) (embeddings.Embedder, error) {
	provider, model, _ := strings.Cut(spec, ":")
	switch provider {
	case "openai":
		return &openai.Embeddings{Model: model}, nil
	case "ollama":
		return &ollama.Embeddings{Model: model}, nil
	case "fake":
		return embeddings.Fake{}, nil
	}
	return nil, fmt.Errorf("chatmodels: unknown embeddings provider %q", provider)
}

func envKey(provider string) string {
	return os.Getenv(strings.ToUpper(provider) + "_API_KEY")
}
