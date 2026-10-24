package team

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/remote"
)

// The manager key must reach the manager only, never a URL the chat names.
func TestGraphToolsSendKeyOnlyToManager(t *testing.T) {
	t.Setenv("LANGGRAPH_API_KEY", "env-key")
	t.Setenv("LANGSMITH_API_KEY", "env-key")
	got := map[string]string{}
	h := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got[name] = r.Header.Get("x-api-key")
			w.Write([]byte(`{}`))
		})
	}
	mgr, other := httptest.NewServer(h("manager")), httptest.NewServer(h("other"))
	defer mgr.Close()
	defer other.Close()
	m := remote.NewClient(mgr.URL)
	m.APIKey = "secret"
	b := &Bridge{Manager: m}
	for _, tl := range b.GraphTools() {
		if tl.Name() != "graph_state" {
			continue
		}
		for _, u := range []string{"", mgr.URL + "/", other.URL} {
			tl.Call(ctx(t), map[string]any{"thread_id": "t1", "url": u})
		}
	}
	if got["manager"] != "secret" {
		t.Errorf("manager got key %q", got["manager"])
	}
	if got["other"] != "" {
		t.Errorf("other host got key %q", got["other"])
	}
}
