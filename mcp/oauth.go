package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// OAuth authenticates an MCP http server per the MCP authorization spec
// (2025-06-18): protected-resource and authorization-server discovery
// (RFC 9728, RFC 8414), dynamic client registration (RFC 7591), authorization
// code with PKCE and a loopback redirect, resource indicators (RFC 8707) and
// token refresh. Use one value per server.
//
// shortcut: public clients only (no client secret), loopback redirect only,
// no OpenID discovery fallback; add them when a server needs them.
type OAuth struct {
	// ClientID skips dynamic registration (for servers that give you one).
	ClientID string
	Scopes   []string
	// TokenFile keeps the tokens between runs (written 0600). Empty keeps them in memory.
	TokenFile string
	// Authorize shows authURL to the user; default prints it on stderr. The
	// browser is redirected back to a loopback listener started by login.
	Authorize func(ctx context.Context, authURL string) error
	HTTP      *http.Client

	mu     sync.Mutex
	loaded bool
	st     oauthState
}

type oauthState struct {
	ClientID      string    `json:"client_id"`
	TokenEndpoint string    `json:"token_endpoint"`
	Resource      string    `json:"resource"`
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	Expiry        time.Time `json:"expiry,omitempty"`
}

func (o *OAuth) hc() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

// token returns a usable access token, refreshing an expired one; "" means log in.
func (o *OAuth) token(ctx context.Context) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.loaded {
		o.loaded = true
		if b, err := os.ReadFile(o.TokenFile); err == nil {
			_ = json.Unmarshal(b, &o.st)
		}
	}
	if o.st.AccessToken != "" && (o.st.Expiry.IsZero() || time.Until(o.st.Expiry) > 30*time.Second) {
		return o.st.AccessToken
	}
	if o.st.RefreshToken != "" && o.refresh(ctx) == nil {
		return o.st.AccessToken
	}
	o.st.AccessToken = ""
	return ""
}

// Access returns a valid access token for serverURL, logging in if there is
// none. Use it when something other than the MCP http transport sends the token.
func (o *OAuth) Access(ctx context.Context, serverURL string) (string, error) {
	if t := o.token(ctx); t != "" {
		return t, nil
	}
	if err := o.login(ctx, serverURL, "", ""); err != nil {
		return "", err
	}
	return o.token(ctx), nil
}

var resourceMetadataRE = regexp.MustCompile(`resource_metadata="([^"]+)"`)

// login obtains a new token after the server rejected rejected. A token that
// changed meanwhile (another request logged in) is enough.
func (o *OAuth) login(ctx context.Context, serverURL, challenge, rejected string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.st.AccessToken != "" && o.st.AccessToken != rejected {
		return nil
	}
	if o.st.RefreshToken != "" && o.refresh(ctx) == nil {
		return nil
	}
	su, err := url.Parse(serverURL)
	if err != nil {
		return err
	}
	origin := su.Scheme + "://" + su.Host

	prmURL := origin + "/.well-known/oauth-protected-resource"
	if m := resourceMetadataRE.FindStringSubmatch(challenge); m != nil {
		prmURL = m[1]
	}
	issuer := origin // servers without protected-resource metadata are their own authorization server
	var prm struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if o.getJSON(ctx, prmURL, &prm) == nil && len(prm.AuthorizationServers) > 0 {
		issuer = prm.AuthorizationServers[0]
	}
	iu, err := url.Parse(issuer)
	if err != nil {
		return err
	}
	var as struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		RegistrationEndpoint  string `json:"registration_endpoint"`
	}
	if o.getJSON(ctx, iu.Scheme+"://"+iu.Host+"/.well-known/oauth-authorization-server"+strings.TrimRight(iu.Path, "/"), &as) != nil {
		// the spec's defaults for servers without metadata
		base := iu.Scheme + "://" + iu.Host
		as.AuthorizationEndpoint, as.TokenEndpoint, as.RegistrationEndpoint = base+"/authorize", base+"/token", base+"/register"
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	redirect := "http://" + ln.Addr().String() + "/callback"
	type result struct{ code, state, err string }
	done := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		select {
		case done <- result{q.Get("code"), q.Get("state"), q.Get("error")}:
		default:
		}
		fmt.Fprintln(w, "Authorized. You can close this tab.")
	})}
	go srv.Serve(ln)
	defer srv.Close()

	clientID := o.ClientID
	if clientID == "" {
		if clientID, err = o.register(ctx, as.RegistrationEndpoint, redirect); err != nil {
			return err
		}
	}
	verifier, state := randString(), randString()
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {serverURL},
	}
	if len(o.Scopes) > 0 {
		q.Set("scope", strings.Join(o.Scopes, " "))
	}
	authURL := as.AuthorizationEndpoint
	if strings.Contains(authURL, "?") {
		authURL += "&" + q.Encode()
	} else {
		authURL += "?" + q.Encode()
	}
	show := o.Authorize
	if show == nil {
		show = func(_ context.Context, u string) error {
			_, err := fmt.Fprintf(os.Stderr, "mcp: authorize %s at:\n%s\n", serverURL, u)
			return err
		}
	}
	if err := show(ctx, authURL); err != nil {
		return err
	}
	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		return errors.New("mcp: oauth: no authorization within 5 minutes")
	}
	if res.err != "" || res.code == "" || res.state != state {
		return fmt.Errorf("mcp: oauth authorization failed (error=%q, state match=%v)", res.err, res.state == state)
	}
	o.st = oauthState{ClientID: clientID, TokenEndpoint: as.TokenEndpoint, Resource: serverURL}
	return o.tokenRequest(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
}

func (o *OAuth) refresh(ctx context.Context) error {
	err := o.tokenRequest(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {o.st.RefreshToken}})
	if err != nil {
		o.st.RefreshToken = "" // revoked or expired: next time log in
	}
	return err
}

// tokenRequest posts to the token endpoint and stores the answer. Callers hold o.mu.
func (o *OAuth) tokenRequest(ctx context.Context, form url.Values) error {
	form.Set("client_id", o.st.ClientID)
	form.Set("resource", o.st.Resource)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.st.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := o.hc().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &tr) != nil || tr.AccessToken == "" {
		return fmt.Errorf("mcp: oauth token endpoint: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 512)])))
	}
	o.st.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" { // a refresh may omit it: keep the old one
		o.st.RefreshToken = tr.RefreshToken
	}
	o.st.Expiry = time.Time{}
	if tr.ExpiresIn > 0 {
		o.st.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	if o.TokenFile != "" {
		b, _ := json.Marshal(o.st)
		return os.WriteFile(o.TokenFile, b, 0o600)
	}
	return nil
}

func (o *OAuth) register(ctx context.Context, endpoint, redirect string) (string, error) {
	b, _ := json.Marshal(map[string]any{
		"client_name":                "golang-chain-eco",
		"redirect_uris":              []string{redirect},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(b)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.hc().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		ClientID string `json:"client_id"`
	}
	if resp.StatusCode >= 300 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil || out.ClientID == "" {
		return "", fmt.Errorf("mcp: oauth client registration failed (http %d); set OAuth.ClientID", resp.StatusCode)
	}
	return out.ClientID, nil
}

func (o *OAuth) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := o.hc().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("http " + resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func randString() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
