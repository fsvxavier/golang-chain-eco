package team

import (
	"context"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// cachePrompt marks the system prompt as a cache breakpoint for Anthropic. The
// API caches everything before it, tools included (the order is tools, system,
// messages), so the role's skill and the MCP tool schemas, which are the same on
// every call of a role, are billed at the cache-read rate after the first call
// (5 minute TTL, renewed on each hit). Other providers get the request as is.
type cachePrompt struct{}

func (cachePrompt) Name() string { return "prompt_cache" }

func (cachePrompt) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	if req.System == "" {
		return next(ctx, req)
	}
	r := *req
	r.System = ""
	sys := messages.Message{Role: messages.RoleSystem, Parts: []messages.ContentPart{{
		Type: "text", Text: req.System, Extra: map[string]any{"cache_control": map[string]any{"type": "ephemeral"}},
	}}}
	r.Messages = append([]messages.Message{sys}, req.Messages...)
	return next(ctx, &r)
}

// cacheFor is the middleware to add for role's model: cachePrompt when the model
// is Anthropic's and PROMPT_CACHE is not "off", else none.
func cacheFor(role string) []agents.Middleware {
	spec := ModelSpec(role)
	if os.Getenv("PROMPT_CACHE") == "off" || !(strings.HasPrefix(spec, "anthropic:") || strings.HasPrefix(spec, "claude")) {
		return nil
	}
	return []agents.Middleware{cachePrompt{}}
}
