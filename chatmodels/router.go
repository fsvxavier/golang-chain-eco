package chatmodels

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
	"github.com/fsvxavier/golang-chain-eco/providers/router"
)

// newRouter builds "router:<model>" from the ROUTER_* variables: an ordered
// ROUTER_TARGETS list, each target a native provider or an OpenAI-compatible
// gateway, plus ROUTER_MODE, ROUTER_WEIGHTS and ROUTER_TIMEOUT.
func newRouter(model string, defaults ...models.Option) (models.ChatModel, error) {
	names := routerTargetNames()
	if len(names) == 0 {
		return nil, fmt.Errorf("chatmodels: set ROUTER_TARGETS to an ordered, comma-separated list of targets")
	}
	targets := make([]router.Target, 0, len(names))
	for _, name := range names {
		m, err := routerTarget(name, model, defaults)
		if err != nil {
			return nil, err
		}
		targets = append(targets, router.Target{Name: name, Model: m})
	}
	mode, err := routerMode()
	if err != nil {
		return nil, err
	}
	weights, err := routerWeights(names)
	if err != nil {
		return nil, err
	}
	timeout, err := routerTimeout()
	if err != nil {
		return nil, err
	}
	r, err := router.New(targets, router.Config{Model: model, Mode: mode, Weights: weights, Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("chatmodels: router: %w", err)
	}
	return r, nil
}

// routerTargetNames splits ROUTER_TARGETS into lower-cased names, dropping
// blanks. Duplicates are left for router.New to reject.
func routerTargetNames() []string {
	var names []string
	for _, n := range strings.Split(os.Getenv("ROUTER_TARGETS"), ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// routerTarget builds one target. The model is ROUTER_MODEL_<NAME> when set,
// else the model after "router:".
func routerTarget(name, model string, defaults []models.Option) (models.ChatModel, error) {
	env := envName(name)
	m := model
	if v := os.Getenv("ROUTER_MODEL_" + env); v != "" {
		m = v
	}
	if m == "" {
		return nil, fmt.Errorf("chatmodels: router target %q has no model; set ROUTER_MODEL_%s or the model after router:", name, env)
	}
	switch name {
	case "anthropic", "openai":
		if key := strings.ToUpper(name) + "_API_KEY"; os.Getenv(key) == "" {
			return nil, fmt.Errorf("chatmodels: router target %q needs $%s", name, key)
		}
		return Init(name+":"+m, defaults...)
	}
	if base := OpenAICompatible[name]; base != "" {
		return Init(name+":"+m, defaults...)
	}
	base := os.Getenv(env + "_BASE_URL")
	if base == "" {
		return nil, fmt.Errorf("chatmodels: router target %q needs $%s_BASE_URL", name, env)
	}
	c := openai.New(m, defaults...)
	c.BaseURL, c.APIKey = base, os.Getenv(env+"_API_KEY")
	return c, nil
}

// envName upper-cases a target name and maps every byte outside [A-Z0-9] to
// '_' (my-gw -> MY_GW), so any ROUTER_TARGETS entry yields a valid variable
// name; strings.ToUpper alone would leave "MY-GW_BASE_URL", which a shell
// cannot export. The baseURL and envKey helpers are not reused because they
// only upper-case, and must keep doing exactly that for the table providers.
func envName(name string) string {
	b := []byte(strings.ToUpper(name))
	for i, c := range b {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '_'
		}
	}
	return string(b)
}

func routerMode() (router.Mode, error) {
	switch m := router.Mode(os.Getenv("ROUTER_MODE")); m {
	case "", router.ModeFailover:
		return router.ModeFailover, nil
	case router.ModeRoundRobin, router.ModeRandom, router.ModeWeighted:
		return m, nil
	default:
		return "", fmt.Errorf("chatmodels: ROUTER_MODE %q: want failover, round_robin, random or weighted", string(m))
	}
}

// routerWeights parses "name:weight,..." (names lower-cased) and checks each
// name is one of targets, once. Unset means none.
func routerWeights(targets []string) (map[string]float64, error) {
	raw := strings.TrimSpace(os.Getenv("ROUTER_WEIGHTS"))
	if raw == "" {
		return nil, nil
	}
	weights := map[string]float64{}
	for _, part := range strings.Split(raw, ",") {
		name, val, ok := strings.Cut(strings.TrimSpace(part), ":")
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		name = strings.ToLower(strings.TrimSpace(name))
		if !ok || name == "" || err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("chatmodels: ROUTER_WEIGHTS %q: want name:weight pairs with weights >= 0", raw)
		}
		if !slices.Contains(targets, name) {
			return nil, fmt.Errorf("chatmodels: ROUTER_WEIGHTS: %q is not in ROUTER_TARGETS", name)
		}
		if _, dup := weights[name]; dup {
			return nil, fmt.Errorf("chatmodels: ROUTER_WEIGHTS: duplicate name %q", name)
		}
		weights[name] = f
	}
	return weights, nil
}

func routerTimeout() (time.Duration, error) {
	raw := os.Getenv("ROUTER_TIMEOUT")
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("chatmodels: ROUTER_TIMEOUT %q: want a duration such as 30s", raw)
	}
	return d, nil
}
