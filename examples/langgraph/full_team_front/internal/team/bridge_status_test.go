package team

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/remote"
)

func TestTeamStatusSaysARunFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/state") {
			_, _ = w.Write([]byte(`{"values":{"status":"x"},"next":["po"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"run_id":"r1","status":"error","error":"job failed: claudecli: signal: killed"}`))
	}))
	defer srv.Close()
	b := &Bridge{Manager: remote.NewClient(srv.URL)}
	b.runs.Store("t1", "r1")
	for _, tl := range b.Tools() {
		if tl.Name() == "team_status" {
			out, err := tl.Call(context.Background(), map[string]any{"thread_id": "t1"})
			s, _ := out.(string)
			if err != nil || !strings.Contains(s, "signal: killed") || strings.Contains(s, `"po"`) {
				t.Fatalf("%v %v", err, out)
			}
			return
		}
	}
	t.Fatal("no team_status")
}
