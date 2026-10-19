package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// App authenticates as a GitHub App: it signs a JWT with the app's private key
// and exchanges it for short-lived installation tokens, cached per
// installation until shortly before they expire.
type App struct {
	AppID int64
	// Key is the app's PEM private key (PKCS#1 or PKCS#8).
	Key []byte
	// BaseURL defaults to https://api.github.com.
	BaseURL string
	// InstallationID, when non-zero, is used for every repository; otherwise
	// the installation is looked up per repository.
	InstallationID int64
	Now            func() time.Time

	mu     sync.Mutex
	key    *rsa.PrivateKey
	tokens map[int64]cachedToken
	client *Client
}

type cachedToken struct {
	token string
	exp   time.Time
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) priv() (*rsa.PrivateKey, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.key != nil {
		return a.key, nil
	}
	block, _ := pem.Decode(a.Key)
	if block == nil {
		return nil, errors.New("github: app key is not PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		a.key = k
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github: parsing app key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github: app key is not RSA")
	}
	a.key = rk
	return rk, nil
}

// JWT returns an app JWT valid for ~9 minutes (GitHub allows at most 10).
func (a *App) JWT() (string, error) {
	k, err := a.priv()
	if err != nil {
		return "", err
	}
	now := a.now()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." +
		enc(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": strconv.FormatInt(a.AppID, 10)})
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (a *App) api() *Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		a.client = NewClient(nil)
		if a.BaseURL != "" {
			a.client.BaseURL = a.BaseURL
		}
	}
	return a.client
}

type jwtAuth struct{ jwt string }

func (j jwtAuth) Token(context.Context, string, string) (string, error) { return j.jwt, nil }

// Token implements TokenSource with an installation token.
func (a *App) Token(ctx context.Context, owner, repo string) (string, error) {
	jwt, err := a.JWT()
	if err != nil {
		return "", err
	}
	c := *a.api()
	c.Auth = jwtAuth{jwt}
	id := a.InstallationID
	if id == 0 {
		if owner == "" || repo == "" {
			return "", errors.New("github: owner and repo are required to find the app installation")
		}
		var inst struct {
			ID int64 `json:"id"`
		}
		if _, _, err := c.do(ctx, owner, repo, "GET", "/repos/"+owner+"/"+repo+"/installation", reqOpts{}, &inst); err != nil {
			return "", fmt.Errorf("github app is not installed on %s/%s: %w", owner, repo, err)
		}
		id = inst.ID
	}
	a.mu.Lock()
	if t, ok := a.tokens[id]; ok && a.now().Before(t.exp.Add(-2*time.Minute)) {
		a.mu.Unlock()
		return t.token, nil
	}
	a.mu.Unlock()
	var res struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if _, _, err := c.do(ctx, owner, repo, "POST", "/app/installations/"+strconv.FormatInt(id, 10)+"/access_tokens", reqOpts{}, &res); err != nil {
		return "", err
	}
	a.mu.Lock()
	if a.tokens == nil {
		a.tokens = map[int64]cachedToken{}
	}
	a.tokens[id] = cachedToken{res.Token, res.ExpiresAt}
	a.mu.Unlock()
	return res.Token, nil
}

// VerifySignature checks the X-Hub-Signature-256 header of a webhook delivery
// in constant time.
func VerifySignature(secret, header string, body []byte) bool {
	return verifySig(secret, header, body)
}

func hasPrefixFold(s, p string) bool { return len(s) >= len(p) && strings.EqualFold(s[:len(p)], p) }
