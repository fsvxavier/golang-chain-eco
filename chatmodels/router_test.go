package chatmodels_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// gateway is an httptest OpenAI-style server that records what it was sent.
type gateway struct {
	mu    sync.Mutex
	model string
	auth  string
}

func (g *gateway) serve(t *testing.T, status int, text string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		g.mu.Lock()
		g.model, g.auth = req.Model, r.Header.Get("Authorization")
		g.mu.Unlock()
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"boom"}}`)
			return
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, text)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (g *gateway) seen() (model, auth string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.model, g.auth
}

// clearRouterEnv blanks every ROUTER_* variable (and per-target model
// override) the tests touch, so a developer's ambient env cannot leak in.
func clearRouterEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ROUTER_MODE", "ROUTER_WEIGHTS", "ROUTER_TIMEOUT", "ROUTER_TARGETS"} {
		t.Setenv(k, "")
	}
	for _, n := range []string{"OMNIROUTE", "FREELLMAPI", "OMNI", "ANTHROPIC", "OPENAI", "GROQ", "MY_GW", "GW_A", "GW_B"} {
		t.Setenv("ROUTER_MODEL_"+n, "")
	}
}

func TestRouterInitErrors(t *testing.T) {
	cases := []struct {
		name string
		spec string
		env  map[string]string
		want string
	}{
		{"targets empty", "router:m", map[string]string{"ROUTER_TARGETS": ""}, "ROUTER_TARGETS"},
		{"targets only commas", "router:m", map[string]string{"ROUTER_TARGETS": " , ,"}, "ROUTER_TARGETS"},
		{"bad mode", "router:m", map[string]string{"ROUTER_TARGETS": "omniroute", "OMNIROUTE_BASE_URL": "http://x", "ROUTER_MODE": "sticky"}, "ROUTER_MODE"},
		{"malformed weights", "router:m", map[string]string{"ROUTER_TARGETS": "omniroute", "OMNIROUTE_BASE_URL": "http://x", "ROUTER_WEIGHTS": "a:x"}, "ROUTER_WEIGHTS"},
		{"unknown weight name", "router:m", map[string]string{"ROUTER_TARGETS": "omniroute", "OMNIROUTE_BASE_URL": "http://x", "ROUTER_WEIGHTS": "ghost:2"}, "ghost"},
		{"bad timeout", "router:m", map[string]string{"ROUTER_TARGETS": "omniroute", "OMNIROUTE_BASE_URL": "http://x", "ROUTER_TIMEOUT": "soon"}, "ROUTER_TIMEOUT"},
		{"gateway without base url", "router:m", map[string]string{"ROUTER_TARGETS": "freellmapi"}, "FREELLMAPI_BASE_URL"},
		{"anthropic without key", "router:m", map[string]string{"ROUTER_TARGETS": "anthropic", "ANTHROPIC_API_KEY": ""}, "ANTHROPIC_API_KEY"},
		{"empty model", "router:", map[string]string{"ROUTER_TARGETS": "omniroute", "OMNIROUTE_BASE_URL": "http://x"}, "omniroute"},
		{"weights unknown name", "router:m", map[string]string{"ROUTER_TARGETS": "omniroute", "OMNIROUTE_BASE_URL": "http://x", "ROUTER_WEIGHTS": "omniroute:1,ghost:2"}, "ROUTER_WEIGHTS: \"ghost\""},
		{"weights duplicate name", "router:m", map[string]string{"ROUTER_TARGETS": "omniroute", "OMNIROUTE_BASE_URL": "http://x", "ROUTER_WEIGHTS": "omniroute:1,OMNIROUTE:2"}, "ROUTER_WEIGHTS: duplicate"},
		{"duplicate names", "router:m", map[string]string{"ROUTER_TARGETS": "omni,OMNI", "OMNI_BASE_URL": "http://x"}, "duplicate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearRouterEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			_, err := chatmodels.Init(c.spec)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestRouterFailoverBetweenGateways(t *testing.T) {
	clearRouterEnv(t)
	var omni, fl gateway
	omniSrv := omni.serve(t, http.StatusInternalServerError, "")
	flSrv := fl.serve(t, http.StatusOK, "from freellm")
	t.Setenv("ROUTER_TARGETS", "omniroute,freellmapi")
	t.Setenv("OMNIROUTE_BASE_URL", omniSrv.URL)
	t.Setenv("FREELLMAPI_BASE_URL", flSrv.URL)

	m, err := chatmodels.Init("router:m")
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "from freellm" {
		t.Fatalf("got %q", out.Content)
	}
	if got, _ := omni.seen(); got != "m" {
		t.Fatalf("first target got model %q", got)
	}
	if got, _ := fl.seen(); got != "m" {
		t.Fatalf("second target got model %q", got)
	}

	t.Setenv("ROUTER_MODEL_FREELLMAPI", "other")
	m, err = chatmodels.Init("router:m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := fl.seen(); got != "other" {
		t.Fatalf("ROUTER_MODEL_FREELLMAPI not applied: got %q", got)
	}
}

func TestRouterBearerKeys(t *testing.T) {
	clearRouterEnv(t)
	var omni, fl gateway
	omniSrv := omni.serve(t, http.StatusInternalServerError, "")
	flSrv := fl.serve(t, http.StatusOK, "ok")
	t.Setenv("ROUTER_TARGETS", "omniroute,freellmapi")
	t.Setenv("OMNIROUTE_BASE_URL", omniSrv.URL)
	t.Setenv("OMNIROUTE_API_KEY", "key-omni")
	t.Setenv("FREELLMAPI_BASE_URL", flSrv.URL)
	t.Setenv("FREELLMAPI_API_KEY", "key-fl")

	m, err := chatmodels.Init("router:m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")}); err != nil {
		t.Fatal(err)
	}
	if _, auth := omni.seen(); auth != "Bearer key-omni" {
		t.Fatalf("omniroute auth %q", auth)
	}
	if _, auth := fl.seen(); auth != "Bearer key-fl" {
		t.Fatalf("freellmapi auth %q", auth)
	}
}

func TestRouterModelID(t *testing.T) {
	clearRouterEnv(t)
	t.Setenv("ROUTER_TARGETS", "omniroute")
	t.Setenv("OMNIROUTE_BASE_URL", "http://x")
	m, err := chatmodels.Init("router:m")
	if err != nil {
		t.Fatal(err)
	}
	if id := m.ModelID(); id != "router:m" {
		t.Fatalf("ModelID %q", id)
	}
}

func TestRouterUnknownWeightNoDoublePrefix(t *testing.T) {
	clearRouterEnv(t)
	t.Setenv("ROUTER_TARGETS", "omniroute")
	t.Setenv("OMNIROUTE_BASE_URL", "http://x")
	t.Setenv("ROUTER_WEIGHTS", "ghost:2")
	_, err := chatmodels.Init("router:m")
	if err == nil || strings.Contains(err.Error(), "router: router:") || !strings.Contains(err.Error(), "ROUTER_WEIGHTS") || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("got %v", err)
	}
}

func TestRouterNativeTargets(t *testing.T) {
	clearRouterEnv(t)
	t.Setenv("ROUTER_TARGETS", "anthropic,openai")
	t.Setenv("ANTHROPIC_API_KEY", "k1")
	t.Setenv("OPENAI_API_KEY", "k2")
	m, err := chatmodels.Init("router:m")
	if err != nil {
		t.Fatal(err)
	}
	if id := m.ModelID(); id != "router:m" {
		t.Fatalf("ModelID %q", id)
	}
}

func TestRouterTableProviderUsesBaseURLOverride(t *testing.T) {
	clearRouterEnv(t)
	var g gateway
	srv := g.serve(t, http.StatusOK, "from groq")
	t.Setenv("ROUTER_TARGETS", "groq")
	t.Setenv("GROQ_API_KEY", "key-groq")
	t.Setenv("GROQ_BASE_URL", srv.URL)
	m, err := chatmodels.Init("router:llama")
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")})
	if err != nil || out.Content != "from groq" {
		t.Fatalf("got (%q, %v)", out.Content, err)
	}
	if model, auth := g.seen(); model != "llama" || auth != "Bearer key-groq" {
		t.Fatalf("server saw model %q auth %q", model, auth)
	}
}

func TestRouterNonAlphanumericTargetName(t *testing.T) {
	clearRouterEnv(t)
	var g gateway
	srv := g.serve(t, http.StatusOK, "from my-gw")
	t.Setenv("ROUTER_TARGETS", "my-gw")
	t.Setenv("MY_GW_BASE_URL", srv.URL)
	t.Setenv("MY_GW_API_KEY", "key-mygw")
	m, err := chatmodels.Init("router:m")
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")})
	if err != nil || out.Content != "from my-gw" {
		t.Fatalf("got (%q, %v)", out.Content, err)
	}
	if _, auth := g.seen(); auth != "Bearer key-mygw" {
		t.Fatalf("auth %q", auth)
	}
}

func TestRouterModesAndTimeoutInit(t *testing.T) {
	for _, env := range []map[string]string{
		{"ROUTER_MODE": "round_robin"},
		{"ROUTER_MODE": "weighted", "ROUTER_WEIGHTS": "gw-a:3,gw-b:1"},
		{"ROUTER_TIMEOUT": "2s"},
	} {
		clearRouterEnv(t)
		t.Setenv("ROUTER_TARGETS", "gw-a,gw-b")
		t.Setenv("GW_A_BASE_URL", "http://a")
		t.Setenv("GW_B_BASE_URL", "http://b")
		for k, v := range env {
			t.Setenv(k, v)
		}
		if _, err := chatmodels.Init("router:m"); err != nil {
			t.Errorf("%v: %v", env, err)
		}
	}
}

func TestRouterRoundRobinAlternates(t *testing.T) {
	clearRouterEnv(t)
	var a, b gateway
	t.Setenv("ROUTER_TARGETS", "gw-a,gw-b")
	t.Setenv("GW_A_BASE_URL", a.serve(t, http.StatusOK, "A").URL)
	t.Setenv("GW_B_BASE_URL", b.serve(t, http.StatusOK, "B").URL)
	t.Setenv("ROUTER_MODE", "round_robin")
	m, err := chatmodels.Init("router:m")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for range 2 {
		out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, out.Content)
	}
	if got[0] != "A" || got[1] != "B" {
		t.Fatalf("got %v, want [A B]", got)
	}
}
