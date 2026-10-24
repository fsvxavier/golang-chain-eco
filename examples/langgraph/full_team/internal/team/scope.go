package team

import (
	"context"
	"errors"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
)

// Scope is what a demand allows: the directories where the PO may write
// markdown project documents (documentation, plan, next steps). It comes from
// team_submit and is stored per request; no scope means no access.
type Scope struct {
	DocRoots []string `json:"doc_roots,omitempty"`
}

// IsEmpty reports that the demand named nothing: consumers must treat it as no access.
func (s Scope) IsEmpty() bool { return len(s.DocRoots) == 0 }

// ErrNoScope is returned by HubClient.Scope when the request has none.
var ErrNoScope = errors.New("hub: no scope for this request")

// RequestScopeFromContext returns the id and scope of the request a worker tool
// is running for. The thread id comes from the run's config, which the worker
// sets from the job. The hub maps that exact thread to the request that owns a
// running job on it; any other thread (client-chosen, team_ask_worker) yields
// ErrNoScope and the zero Scope, with no prefix or other fallback.
func RequestScopeFromContext(ctx context.Context, hub *HubClient) (string, Scope, error) {
	id, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	if id == "" || hub == nil {
		return "", Scope{}, ErrNoScope
	}
	return hub.ThreadRequest(ctx, id)
}
