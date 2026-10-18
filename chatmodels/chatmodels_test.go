package chatmodels_test

import (
	"fmt"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/providers/anthropic"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
	"github.com/fsvxavier/golang-chain-eco/providers/cohere"
	"github.com/fsvxavier/golang-chain-eco/providers/google"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

func TestInit(t *testing.T) {
	t.Setenv("PERPLEXITY_API_KEY", "pk")
	t.Setenv("VLLM_BASE_URL", "http://gpu:9000/v1")
	cases := map[string]any{
		"claude-3-5-haiku-latest":  &anthropic.Chat{},
		"gpt-4o":                   &openai.Chat{},
		"gemini-2.0-flash":         &google.Chat{},
		"command-r-plus":           &cohere.Chat{},
		"azure_openai:my-dep":      &openai.Chat{},
		"bedrock:amazon.nova-pro":  &bedrock.Chat{},
		"google_vertexai:gemini-x": &google.Chat{},
		"perplexity:sonar":         &openai.Chat{},
	}
	for spec, want := range cases {
		m, err := chatmodels.Init(spec)
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		if got, w := typeName(m), typeName(want); got != w {
			t.Fatalf("%s: got %s want %s", spec, got, w)
		}
	}
	m, _ := chatmodels.Init("perplexity:sonar")
	if c := m.(*openai.Chat); c.APIKey != "pk" || c.BaseURL != "https://api.perplexity.ai" {
		t.Fatalf("%+v", c)
	}
	m, _ = chatmodels.Init("vllm:llama")
	if m.(*openai.Chat).BaseURL != "http://gpu:9000/v1" {
		t.Fatal("base url override")
	}
	if _, err := chatmodels.Init("nope:x"); err == nil {
		t.Fatal("unknown provider must fail")
	}
	for _, spec := range []string{"openai:text-embedding-3-small", "google_genai:gemini-embedding-001", "cohere:embed-v4.0", "voyageai:voyage-3", "jina:jina-embeddings-v3", "ollama:nomic-embed-text"} {
		if _, err := chatmodels.InitEmbeddings(spec); err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
	}
	if r, err := chatmodels.NewReranker("jina:jina-reranker-v2"); err != nil || r.Path != "/rerank" {
		t.Fatal(r, err)
	}
}

func typeName(v any) string { return fmt.Sprintf("%T", v) }

func TestTenantEndpoints(t *testing.T) {
	if _, err := chatmodels.Init("databricks:dbrx"); err == nil {
		t.Fatal("databricks without base url must fail")
	}
	t.Setenv("DATABRICKS_BASE_URL", "https://ws/serving-endpoints")
	m, err := chatmodels.Init("databricks:dbrx")
	if err != nil || m.(*openai.Chat).BaseURL != "https://ws/serving-endpoints" {
		t.Fatal(m, err)
	}
	if _, err := chatmodels.Init("sarvam:sarvam-m"); err != nil {
		t.Fatal(err)
	}
}
