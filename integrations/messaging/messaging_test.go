package messaging_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/integrations/messaging"
)

func TestPlivoAndTelnyx(t *testing.T) {
	var path, auth string
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasPrefix(path, "/v1/Account") {
			w.WriteHeader(202)
			w.Write([]byte(`{"message_uuid":["uuid-1"],"api_id":"a"}`))
			return
		}
		w.Write([]byte(`{"data":{"id":"tx-9"}}`))
	}))
	defer srv.Close()
	ctx := context.Background()

	p := &messaging.Plivo{AuthID: "MA123", AuthToken: "tok", From: "+1555", BaseURL: srv.URL}
	id, err := p.Send(ctx, "+1999", "hello")
	if err != nil || id != "uuid-1" || path != "/v1/Account/MA123/Message/" || body["src"] != "+1555" || body["dst"] != "+1999" || !strings.HasPrefix(auth, "Basic ") {
		t.Fatal(id, err, path, body, auth)
	}
	tx := &messaging.Telnyx{APIKey: "tk", From: "+1555", BaseURL: srv.URL}
	if id, err = tx.Send(ctx, "+1999", "hi"); err != nil || id != "tx-9" || path != "/v2/messages" || body["to"] != "+1999" || auth != "Bearer tk" {
		t.Fatal(id, err, path, body, auth)
	}

	guarded := &messaging.Telnyx{APIKey: "tk", BaseURL: srv.URL, Allow: func(to string) bool { return to == "+1111" }}
	path = ""
	if _, err := guarded.Send(ctx, "+1999", "x"); err == nil || path != "" {
		t.Fatalf("disallowed number must not reach the API: err=%v path=%q", err, path)
	}
	tool := messaging.Tool("send_sms", tx)
	out, err := tool.Call(ctx, map[string]any{"to": "+1999", "text": "yo"})
	if err != nil || out != "sent, id tx-9" {
		t.Fatal(out, err)
	}
}
