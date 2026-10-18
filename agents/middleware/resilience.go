package middleware

import (
	"context"
	"errors"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

// ModelFallback tries alternative models when the primary fails
// (ModelFallbackMiddleware).
type ModelFallback struct{ Fallbacks []models.ChatModel }

func (ModelFallback) Name() string { return "model_fallback" }
func (m ModelFallback) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	out, err := next(ctx, req)
	if err == nil {
		return out, nil
	}
	errs := []error{err}
	for _, fb := range m.Fallbacks {
		r := *req
		r.Model = fb
		if out, err = next(ctx, &r); err == nil {
			return out, nil
		}
		errs = append(errs, err)
	}
	return messages.Message{}, errors.Join(errs...)
}

// ModelRetry retries failed model calls with backoff (ModelRetryMiddleware).
type ModelRetry struct{ Options runnables.RetryOptions }

func (ModelRetry) Name() string { return "model_retry" }
func (m ModelRetry) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	var out messages.Message
	err := runnables.Backoff(ctx, m.Options, func() (err error) { out, err = next(ctx, req); return })
	return out, err
}

// ToolRetry retries failing tool calls with backoff (ToolRetryMiddleware).
// When retries are exhausted the error is shown to the model.
type ToolRetry struct {
	Options runnables.RetryOptions
	// Tools restricts retries to these tool names (all when empty).
	Tools []string
}

func (ToolRetry) Name() string { return "tool_retry" }
func (m ToolRetry) WrapToolCall(ctx context.Context, c messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	if len(m.Tools) > 0 {
		found := false
		for _, t := range m.Tools {
			found = found || t == c.Name
		}
		if !found {
			return next(ctx, c)
		}
	}
	var out messages.Message
	err := runnables.Backoff(ctx, m.Options, func() error {
		var err error
		out, err = next(ctx, c)
		if err == nil && out.Status == "error" {
			err = errors.New(out.Content)
		}
		return err
	})
	if err != nil && out.Role == "" {
		out = messages.Tool("Tool failed: "+err.Error(), c.ID)
		out.Name, out.Status = c.Name, "error"
	}
	return out, nil
}
