package team

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/claudecli"
)

// CLICall is one claude CLI call of an agent, as the hub keeps it and the
// dashboard shows it.
type CLICall struct {
	ID         string     `json:"id"`
	Worker     string     `json:"worker"`
	Role       string     `json:"role"`
	Model      string     `json:"model"`
	Session    string     `json:"session,omitempty"` // Claude Code session; its conversation is readable when set
	StartedMS  int64      `json:"started_ms"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	CostUSD    float64    `json:"cost_usd,omitempty"` // list-price equivalent, not what the subscription is charged
	Tokens     DashTokens `json:"tokens"`
	Done       bool       `json:"done"`
	Error      string     `json:"error,omitempty"`
}

const maxCLICalls = 200

// AddCLI stores or updates (same id) a CLI call; only the newest maxCLICalls are kept.
func (h *Hub) AddCLI(c CLICall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cli {
		if h.cli[i].ID == c.ID {
			h.cli[i] = c
			return
		}
	}
	h.cli = append(h.cli, c)
	if len(h.cli) > maxCLICalls {
		h.cli = h.cli[len(h.cli)-maxCLICalls:]
	}
}

// CLICalls returns a copy of the kept calls, oldest first.
func (h *Hub) CLICalls() []CLICall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]CLICall{}, h.cli...)
}

// PostCLI reports a CLI call to the hub. Best effort: it is only shown on the dashboard.
func (c *HubClient) PostCLI(ctx context.Context, call CLICall) {
	_, _ = c.call(ctx, "POST", "/cli", call, nil)
}

// CLICalls fetches the hub's CLI calls.
func (c *HubClient) CLICalls(ctx context.Context) ([]CLICall, error) {
	var cs []CLICall
	_, err := c.call(ctx, "GET", "/cli", nil, &cs)
	return cs, err
}

// observeCLI makes the model, when it is the claude CLI provider, report each
// of its calls to the hub under id/role, and keep its sessions so the dashboard
// can show the conversation (CLAUDE_CLI_TRANSCRIPTS=off keeps nothing).
func observeCLI(m models.ChatModel, hub *HubClient, id, role string) {
	cc, ok := m.(*claudecli.Chat)
	if !ok {
		return
	}
	cc.Persist = os.Getenv("CLAUDE_CLI_TRANSCRIPTS") != "off"
	cc.Observer = func(e claudecli.Event) {
		call := CLICall{ID: e.ID, Worker: id, Role: role, Model: e.Model, Session: e.Session, StartedMS: e.StartedMS,
			DurationMS: e.DurationMS, CostUSD: e.CostUSD, Done: e.Done, Error: e.Err}
		call.Tokens.add(e.Usage)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		hub.PostCLI(ctx, call)
	}
	log.Printf("%s: model calls go through the claude CLI (subscription); transcripts %v", id, cc.Persist)
}
