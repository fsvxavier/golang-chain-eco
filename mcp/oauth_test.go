package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/mcp"
)

func TestOAuthLoginAndRefresh(t *testing.T) {
	var challenge string
	var logins, refreshes atomic.Int32
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{srv.URL}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token", "registration_endpoint": srv.URL + "/register",
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"client_id": "cid"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_id") != "cid" || r.Form.Get("resource") != srv.URL+"/mcp" {
			http.Error(w, "bad client/resource", 400)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				http.Error(w, "bad pkce", 400)
				return
			}
			logins.Add(1)
			// expires inside the 30s skew, so the next call must refresh
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at1", "refresh_token": "rt", "expires_in": 1})
		case "refresh_token":
			if r.Form.Get("refresh_token") != "rt" {
				http.Error(w, "bad refresh", 400)
				return
			}
			refreshes.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at2", "expires_in": 3600})
		}
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("Authorization") == "Bearer at1" && refreshes.Load() > 0 {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource"`)
			http.Error(w, "unauthorized", 401)
			return
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if res, ok := handle(req); ok {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": res})
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "tok.json")
	o := &mcp.OAuth{TokenFile: file, Authorize: func(ctx context.Context, authURL string) error {
		u, _ := url.Parse(authURL)
		q := u.Query()
		challenge = q.Get("code_challenge")
		if q.Get("client_id") != "cid" || q.Get("code_challenge_method") != "S256" || q.Get("resource") != srv.URL+"/mcp" {
			t.Errorf("bad authorize params: %v", q)
		}
		resp, err := http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + q.Get("state"))
		if err == nil {
			resp.Body.Close()
		}
		return err
	}}
	ctx := context.Background()
	c, err := mcp.Connect(ctx, mcp.HTTPOAuth(srv.URL+"/mcp", nil, o))
	if err != nil {
		t.Fatal(err)
	}
	ts, err := c.Tools(ctx, "srv")
	if err != nil || len(ts) != 1 {
		t.Fatalf("%v %v", ts, err)
	}
	if logins.Load() != 1 || refreshes.Load() == 0 {
		t.Fatalf("logins=%d refreshes=%d", logins.Load(), refreshes.Load())
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file: %v %v", fi, err)
	}
}
