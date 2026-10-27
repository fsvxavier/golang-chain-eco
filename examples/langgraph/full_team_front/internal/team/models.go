package team

import (
	"fmt"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// Roles that own a model. MANAGER runs in the manager service, KNOWLEDGE in
// the hub; the rest are workers.
var Roles = []string{"manager", "po", "leader", "junior", "mid", "senior", "specialist", "qa", "knowledge"}

// anthropicDefaults are used when only ANTHROPIC_API_KEY is set: the heavy
// thinkers get the big model, the volume roles the small one.
var anthropicDefaults = map[string]string{
	"manager": "sonnet_5_5", "po": "opus_5_5", "leader": "sonnet_5_5",
	"junior": "haiku_5_5", "mid": "haiku_5_5", "senior": "haiku_5_5",
	"specialist": "sonnet_5_5", "qa": "sonnet_5_5", "knowledge": "haiku_5_5",
}

// normalize turns "anthropic:opus_5_5" or "anthropic/opus_5_5" into the
// "provider:model" string chatmodels understands ("anthropic:claude-opus-5-5").
func normalize(spec string) string {
	spec = strings.TrimSpace(spec)
	prov, model, ok := strings.Cut(strings.Replace(spec, "/", ":", 1), ":")
	if !ok {
		return spec
	}
	model = strings.ReplaceAll(model, "_", "-")
	if (prov == "anthropic" || prov == "claude-cli") && !strings.HasPrefix(model, "claude-") {
		model = "claude-" + model
	}
	return prov + ":" + model
}

// ModelSpec picks the model of a role, first match wins:
//
//	AGENT_MODEL_<ROLE>   e.g. AGENT_MODEL_PO=anthropic:opus_5_5
//	AGENT_MODEL          one model for every role
//	AGENT_BACKEND        "claude-cli": per-role defaults below through the Claude Code CLI, billed to the
//	                     Claude subscription (no API key; the CLI must be logged in)
//	ANTHROPIC_API_KEY    per-role defaults above (ANTHROPIC_AUTH_TOKEN, a gateway token, works too;
//	                     ANTHROPIC_BASE_URL points the Anthropic provider at the gateway)
//	OPENAI_API_KEY       openai:gpt-4o-mini
//
// AGENT_MCP=name=url,... gives the Claude models (anthropic, claude-cli) those OAuth-protected
// remote MCP servers (see mcpserver.go).
//
// An empty result means "run offline with the scripted model".
func ModelSpec(role string) string {
	if s := os.Getenv("AGENT_MODEL_" + strings.ToUpper(role)); s != "" {
		return normalize(s)
	}
	if s := os.Getenv("AGENT_MODEL"); s != "" {
		return normalize(s)
	}
	switch {
	case os.Getenv("AGENT_BACKEND") == "claude-cli":
		return normalize("claude-cli:" + anthropicDefaults[role])
	case os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_AUTH_TOKEN") != "":
		return normalize("anthropic:" + anthropicDefaults[role])
	case os.Getenv("OPENAI_API_KEY") != "":
		return "openai:gpt-4o-mini"
	}
	return ""
}

// NewModel builds the chat model of a role. fake answers when the spec is
// empty or "fake" (offline mode); it returns the label to print.
func NewModel(role string, fake func([]messages.Message) messages.Message) (models.ChatModel, string, error) {
	spec := ModelSpec(role)
	if spec == "" || spec == "fake" || spec == "fake:" {
		return &models.Fake{Name: "offline:" + role, Func: func(m []messages.Message, _ models.Options) (messages.Message, error) {
			return fake(m), nil
		}}, "offline", nil
	}
	m, err := chatmodels.Init(spec)
	if err != nil {
		return nil, "", fmt.Errorf("role %s: model %q: %w", role, spec, err)
	}
	if err := attachMCP(m); err != nil {
		return nil, "", fmt.Errorf("role %s: %w", role, err)
	}
	return m, spec, nil
}

// Embedder is KNOWLEDGE_EMBEDDINGS (provider:model) or the offline hashing embedder.
func Embedder() (embeddings.Embedder, error) {
	if spec := os.Getenv("KNOWLEDGE_EMBEDDINGS"); spec != "" {
		return chatmodels.InitEmbeddings(spec)
	}
	return embeddings.Fake{Dim: 256}, nil
}

// KnowledgeModel is the model behind RAG/CAG/KAG answers (role "knowledge").
func KnowledgeModel() (models.ChatModel, string, error) {
	if spec := ModelSpec("knowledge"); spec != "" && spec != "fake" {
		m, err := chatmodels.Init(spec)
		return m, spec, err
	}
	return offlineKnowledgeModel(), "offline", nil
}
