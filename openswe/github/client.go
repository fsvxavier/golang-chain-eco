// Package github is a small, typed GitHub REST/GraphQL client for the Open SWE
// workflows: pull requests, reviews, checks, workflow runs and webhooks. It
// uses only the standard library; authentication is a TokenSource, which can
// be a fixed token or a GitHub App that mints installation tokens.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TokenSource supplies the credential for a repository.
type TokenSource interface {
	Token(ctx context.Context, owner, repo string) (string, error)
}

// StaticToken is a fixed token (a PAT or a pre-minted installation token).
type StaticToken string

// Token implements TokenSource.
func (t StaticToken) Token(context.Context, string, string) (string, error) { return string(t), nil }

// Client calls the GitHub API.
type Client struct {
	// BaseURL defaults to https://api.github.com (set it for GHES or tests).
	BaseURL string
	Auth    TokenSource
	HTTP    *http.Client
	// UserAgent defaults to "golang-chain-eco-openswe".
	UserAgent string
}

// NewClient builds a client.
func NewClient(auth TokenSource) *Client {
	return &Client{BaseURL: "https://api.github.com", Auth: auth, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// APIError is a non-2xx GitHub response.
type APIError struct {
	Status  int
	Message string
	Method  string
	Path    string
	// RetryAfter is set on rate limits.
	RetryAfter time.Duration
	Headers    http.Header
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

// IsNotFound reports a 404 (which GitHub also uses for "no access").
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// IsAccessError reports 401/403/404: the caller lacks access or the app is not
// installed. Such failures must be reported, not worked around.
func IsAccessError(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == 401 || ae.Status == 403 || ae.Status == 404)
}

// Repo scopes calls to one repository.
type Repo struct {
	c           *Client
	Owner, Name string
}

// Repo returns a repository handle.
func (c *Client) Repo(owner, name string) *Repo { return &Repo{c: c, Owner: owner, Name: name} }

func (r *Repo) path(format string, a ...any) string {
	return fmt.Sprintf("/repos/%s/%s", url.PathEscape(r.Owner), url.PathEscape(r.Name)) + fmt.Sprintf(format, a...)
}

type reqOpts struct {
	accept  string
	query   url.Values
	body    any
	timeout time.Duration
}

// do sends a request and returns the body; out (if non-nil) receives decoded JSON.
func (c *Client) do(ctx context.Context, owner, repo, method, path string, o reqOpts, out any) (http.Header, []byte, error) {
	token := ""
	if c.Auth != nil {
		t, err := c.Auth.Token(ctx, owner, repo)
		if err != nil {
			return nil, nil, fmt.Errorf("github: token for %s/%s: %w", owner, repo, err)
		}
		token = t
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	u := base + path
	if len(o.query) > 0 {
		u += "?" + o.query.Encode()
	}
	var rd io.Reader
	if o.body != nil {
		b, err := json.Marshal(o.body)
		if err != nil {
			return nil, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, nil, err
	}
	accept := o.accept
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	ua := c.UserAgent
	if ua == "" {
		ua = "golang-chain-eco-openswe"
	}
	req.Header.Set("User-Agent", ua)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if o.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode, Method: method, Path: path, Headers: resp.Header, Message: strings.TrimSpace(string(data))}
		var body struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &body) == nil && body.Message != "" {
			ae.Message = body.Message
		}
		if ra, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			ae.RetryAfter = time.Duration(ra) * time.Second
		}
		return resp.Header, data, ae
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.Header, data, fmt.Errorf("github: decoding %s %s: %w", method, path, err)
		}
	}
	return resp.Header, data, nil
}

// paged collects every page of a list endpoint (per_page=100).
func paged[T any](ctx context.Context, c *Client, owner, repo, path string, q url.Values, field string) ([]T, error) {
	var all []T
	for page := 1; page <= 50; page++ {
		qq := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}
		for k, v := range q {
			qq[k] = v
		}
		var batch []T
		if field != "" {
			var wrap map[string]json.RawMessage
			if _, _, err := c.do(ctx, owner, repo, "GET", path, reqOpts{query: qq}, &wrap); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(wrap[field], &batch); err != nil {
				return nil, err
			}
		} else if _, _, err := c.do(ctx, owner, repo, "GET", path, reqOpts{query: qq}, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			return all, nil
		}
	}
	return all, nil
}

// GraphQL runs a GraphQL query (needed for review threads).
func (c *Client) GraphQL(ctx context.Context, owner, repo, query string, vars map[string]any, out any) error {
	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_, _, err := c.do(ctx, owner, repo, "POST", "/graphql", reqOpts{body: map[string]any{"query": query, "variables": vars}}, &res)
	if err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		return fmt.Errorf("github: graphql: %s", res.Errors[0].Message)
	}
	if out != nil {
		return json.Unmarshal(res.Data, out)
	}
	return nil
}
