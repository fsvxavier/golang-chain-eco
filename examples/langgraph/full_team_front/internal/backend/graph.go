package backend

import (
	"context"
	"os"
	"slices"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
	"github.com/fsvxavier/golang-chain-eco/integrations/codegraph"
)

// graphTool is backend_graph, given to every role: a question answered from the
// code graph (CodeGraph or graphify) of a directory. The directories it may ask
// are the roots the demand named and the workspace itself (WORKSPACE_ROOT, the
// project the operator pointed the team at); the index must sit in the directory
// itself, so an index above it never widens the reach. The engines are in
// integrations/codegraph.
func graphTool(cfg Config, resolve func(context.Context) (string, team.Scope, error), count func(context.Context, string)) tools.Tool {
	return codegraph.Tool("backend_graph", func(ctx context.Context, root string) (string, error) {
		_, s, err := resolve(ctx)
		if err != nil {
			return "", tools.Errorf("no scope for this job (%v): access refused", err)
		}
		roots := append(slices.Clone(s.Roots), cfg.WorkspaceRoot)
		p, err := resolvePath(cfg.WorkspaceRoot, roots, root)
		if err != nil {
			return "", err
		}
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			return "", tools.Errorf("%q is not a directory", root)
		}
		return p, nil
	}, codegraph.Runner(cfg.Run), func(ctx context.Context) {
		if id, _, err := resolve(ctx); err == nil {
			count(ctx, id)
		}
	})
}
