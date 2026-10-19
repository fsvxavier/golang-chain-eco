package langmem

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
)

// ErrConfiguration is wrapped by errors caused by a missing store, namespace
// or Configurable value (errors.ConfigurationError).
var ErrConfiguration = errors.New("langmem: configuration error")

// Namespace is a store namespace whose parts may be templates such as
// "{langgraph_user_id}", filled at run time from runnables.Config.Configurable
// (NamespaceTemplate). This scopes memories per user, team or organisation.
type Namespace []string

// DefaultNamespace is LangMem's default: ("memories", "{langgraph_user_id}").
var DefaultNamespace = Namespace{"memories", "{langgraph_user_id}"}

var tmpl = regexp.MustCompile(`\{([^{}]+)\}`)

// Resolve substitutes every {name} from the run configuration. A missing
// variable is an error, so memories never land in a wrong shared namespace.
func (n Namespace) Resolve(ctx context.Context) ([]string, error) {
	cfg := runnables.GetConfig(ctx).Configurable
	out := make([]string, len(n))
	var err error
	for i, part := range n {
		out[i] = tmpl.ReplaceAllStringFunc(part, func(m string) string {
			name := m[1 : len(m)-1]
			v, ok := cfg[name]
			if !ok {
				keys := make([]string, 0, len(cfg))
				for k := range cfg {
					keys = append(keys, k)
				}
				slices.Sort(keys)
				err = fmt.Errorf("%w: missing key %q in Configurable (available: %v)", ErrConfiguration, name, keys)
				return m
			}
			return fmt.Sprint(v)
		})
	}
	return out, err
}

// WithConfigurable returns a context whose Configurable carries the given
// key/value pairs, e.g. WithConfigurable(ctx, "langgraph_user_id", "u1").
func WithConfigurable(ctx context.Context, kv ...string) context.Context {
	c := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		c[kv[i]] = kv[i+1]
	}
	return runnables.WithConfig(ctx, runnables.Config{Configurable: c})
}
