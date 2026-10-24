package team

import (
	"context"
	"errors"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
)

// Scope is what a demand allows the PO's backend tools and the QA browser to
// reach: source roots, backend URLs and the front-end's own URL. It comes from
// team_submit and is stored per request; no scope means no access.
type Scope struct {
	Roots  []string `json:"roots,omitempty"`
	URLs   []string `json:"urls,omitempty"`
	AppURL string   `json:"app_url,omitempty"`
}

// IsEmpty reports that the demand named nothing. An empty Scope with a nil
// error means exactly that, and consumers must treat it as no access.
func (s Scope) IsEmpty() bool { return len(s.Roots) == 0 && len(s.URLs) == 0 && s.AppURL == "" }

// ErrNoScope is returned by HubClient.Scope when the request has none.
var ErrNoScope = errors.New("hub: no scope for this request")

// ScopeFromContext returns the scope of the request a worker tool is running
// for. The thread id comes from the run's config, which the worker sets from
// the job. The hub maps that exact thread to the request that owns a running
// job on it; any other thread (client-chosen, team_ask_worker) yields
// ErrNoScope and the zero Scope, with no prefix or other fallback.
func ScopeFromContext(ctx context.Context, hub *HubClient) (Scope, error) {
	_, s, err := RequestScopeFromContext(ctx, hub)
	return s, err
}

// RequestScopeFromContext is ScopeFromContext plus the id of the request that
// owns the job, resolved by the same exact thread match.
func RequestScopeFromContext(ctx context.Context, hub *HubClient) (string, Scope, error) {
	id, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	if id == "" || hub == nil {
		return "", Scope{}, ErrNoScope
	}
	return hub.ThreadRequest(ctx, id)
}
