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
		{"duplicate names", "router:m", map[string]string{"ROUTER_TARGETS": "omni,OMNI", "OMNI_BASE_URL": "http://x"}, "duplicate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
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
