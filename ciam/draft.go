package ciam

import (
	"context"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

func (m *Manager) generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	var opts []models.Option
	if maxTokens > 0 {
		opts = append(opts, models.WithMaxTokens(maxTokens))
	}
	out, err := m.Model.Generate(ctx, []messages.Message{messages.Human(prompt)}, opts...)
	return out.Text(), err
}
