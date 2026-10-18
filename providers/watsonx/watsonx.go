// Package watsonx is the IBM watsonx.ai integration (langchain-ibm): chat
// models through the text/chat and text/chat_stream routes and embeddings,
// authenticated with an IBM Cloud API key exchanged for an IAM bearer token.
//
//	m := watsonx.New("ibm/granite-3-8b-instruct", watsonx.Config{})
//
// $WATSONX_URL (default https://us-south.ml.cloud.ibm.com) and
// $WATSONX_API_KEY configure the endpoint and credentials.
package watsonx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

const apiVersion = "2024-05-31"

// Auth exchanges an IBM Cloud API key for IAM access tokens and caches them.
type Auth struct {
	APIKey string // default $WATSONX_API_KEY
	IAMURL string // default https://iam.cloud.ibm.com/identity/token
	HTTP   *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// Token returns a valid bearer token, refreshing it a minute before expiry.
func (a *Auth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Until(a.expiry) > time.Minute {
		return a.token, nil
	}
	key := a.APIKey
	if key == "" {
		key = os.Getenv("WATSONX_API_KEY")
	}
	if key == "" {
		return "", errors.New("watsonx: set $WATSONX_API_KEY (an IBM Cloud API key)")
	}
	iam := a.IAMURL
	if iam == "" {
		iam = "https://iam.cloud.ibm.com/identity/token"
	}
	form := url.Values{"grant_type": {"urn:ibm:params:oauth:grant-type:apikey"}, "apikey": {key}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, iam, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	hc := a.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil || out.AccessToken == "" {
		return "", fmt.Errorf("watsonx: IAM token request failed (http %d)", resp.StatusCode)
	}
	a.token, a.expiry = out.AccessToken, time.Now().Add(time.Duration(out.ExpiresIn)*time.Second)
	return a.token, nil
}

func baseURL(u string) string {
	if u == "" {
		u = os.Getenv("WATSONX_URL")
	}
	if u == "" {
		u = "https://us-south.ml.cloud.ibm.com"
	}
	return strings.TrimRight(u, "/")
}

func projectID(p string) string {
	if p == "" {
		return os.Getenv("WATSONX_PROJECT_ID")
	}
	return p
}

// Config selects the watsonx.ai instance; every field falls back to its
// environment variable ($WATSONX_URL, $WATSONX_PROJECT_ID, $WATSONX_API_KEY).
type Config struct {
	URL       string
	ProjectID string
	Auth      *Auth
}

// New returns a watsonx.ai chat model: the shared chat client pointed at the
// text/chat routes, with watsonx's request dialect.
func New(model string, cfg Config, defaults ...models.Option) *openai.Chat {
	if cfg.Auth == nil {
		cfg.Auth = &Auth{}
	}
	c := openai.New(model, defaults...)
	c.ID = "watsonx:" + model
	c.BaseURL = baseURL(cfg.URL)
	c.Path, c.StreamPath, c.Query = "/ml/v1/text/chat", "/ml/v1/text/chat_stream", "version="+apiVersion
	c.TokenFunc = cfg.Auth.Token
	c.Rewrite = func(req map[string]any) {
		req["model_id"] = req["model"]
		delete(req, "model")
		req["project_id"] = projectID(cfg.ProjectID)
		delete(req, "stream_options")
		if v, ok := req["max_completion_tokens"]; ok {
			req["max_tokens"] = v
			delete(req, "max_completion_tokens")
		}
		if s, ok := req["tool_choice"].(string); ok { // "auto" | "none" | "required"
			req["tool_choice_option"] = s
			delete(req, "tool_choice")
		}
	}
	return c
}

// Embeddings is the watsonx.ai embeddings model.
type Embeddings struct {
	Model     string // e.g. ibm/slate-125m-english-rtrvr
	ProjectID string // default $WATSONX_PROJECT_ID
	URL       string
	Auth      *Auth
	HTTP      *http.Client
}

// EmbedDocuments implements embeddings.Embedder.
func (e *Embeddings) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if e.Auth == nil {
		e.Auth = &Auth{}
	}
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	hc := &httpx.Client{HTTP: e.HTTP, BaseURL: baseURL(e.URL), Headers: map[string]string{"Authorization": "Bearer " + tok}, Query: "version=" + apiVersion}
	var out struct {
		Results []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"results"`
	}
	if err := hc.PostJSON(ctx, "/ml/v1/text/embeddings", map[string]any{"model_id": e.Model, "project_id": projectID(e.ProjectID), "inputs": texts}, &out); err != nil {
		return nil, err
	}
	if len(out.Results) != len(texts) {
		return nil, fmt.Errorf("watsonx: %d embeddings for %d inputs", len(out.Results), len(texts))
	}
	vecs := make([][]float32, len(out.Results))
	for i, r := range out.Results {
		vecs[i] = r.Embedding
	}
	return vecs, nil
}

// EmbedQuery implements embeddings.Embedder.
func (e *Embeddings) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	v, err := e.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}
