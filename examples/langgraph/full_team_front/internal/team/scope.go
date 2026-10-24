package team

import (
	"context"
	"errors"
	"strings"

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

// ErrNoScope is returned by HubClient.Scope when the request has none.
var ErrNoScope = errors.New("hub: no scope for this request")

// ScopeFromContext returns the scope of the request a worker tool is running
// for. The id comes from the run's thread (runnables config), which the worker
// sets from the job, never from model text. The job thread is
// "<request id>-<name>" and request ids may hold dashes, so trailing segments
// are dropped until a stored scope matches; the manager stores one (possibly
// empty) for every request, so the longest match is the request itself.
func ScopeFromContext(ctx context.Context, hub *HubClient) (Scope, error) {
	id, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	for id != "" {
		s, err := hub.Scope(ctx, id)
		if !errors.Is(err, ErrNoScope) {
			return s, err
		}
		i := strings.LastIndex(id, "-")
		if i < 0 {
			break
		}
		id = id[:i]
	}
	return Scope{}, ErrNoScope
}
