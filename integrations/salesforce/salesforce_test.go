package salesforce_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/salesforce"
)

func TestQuery(t *testing.T) {
	var auth, q string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/services/data/v60.0/query":
			q = r.URL.Query().Get("q")
			fmt.Fprint(w, `{"totalSize":3,"done":false,"nextRecordsUrl":"/services/data/v60.0/query/01g-2000","records":[{"attributes":{"type":"Account"},"Id":"1","Name":"A"},{"attributes":{"type":"Account"},"Id":"2","Name":"B"}]}`)
		case "/services/data/v60.0/query/01g-2000":
			fmt.Fprint(w, `{"totalSize":3,"done":true,"records":[{"attributes":{"type":"Account"},"Id":"3","Name":"C"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &salesforce.Client{InstanceURL: srv.URL, AccessToken: "sf-tok"}
	ctx := context.Background()
	recs, err := c.Query(ctx, "SELECT Id, Name FROM Account", 10)
	if err != nil || len(recs) != 3 || recs[2]["Name"] != "C" || recs[0]["attributes"] != nil || auth != "Bearer sf-tok" || q != "SELECT Id, Name FROM Account" {
		t.Fatalf("%+v %v %q", recs, err, q)
	}
	if recs, _ = c.Query(ctx, "select Id from Account", 2); len(recs) != 2 {
		t.Fatalf("max must cap: %d", len(recs))
	}
	if _, err := c.Query(ctx, "DELETE FROM Account", 1); err == nil {
		t.Fatal("non-SELECT must be rejected")
	}
	out, err := c.Tool(5).Call(ctx, map[string]any{"soql": "SELECT Id FROM Account"})
	if err != nil || !strings.Contains(out.(string), `"Name":"A"`) {
		t.Fatal(out, err)
	}
}
