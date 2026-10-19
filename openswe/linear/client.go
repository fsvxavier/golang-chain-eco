// Package linear connects Open SWE to Linear: a GraphQL client (issue fetch,
// comment create), a webhook handler that turns assignments, labels and
// mentions into agent runs (one Linear issue = one openswe thread), and a
// Runner wrapper that comments the outcome on the issue. Standard library only.
package linear

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultEndpoint is the Linear GraphQL API.
const DefaultEndpoint = "https://api.linear.app/graphql"

// Client calls the Linear GraphQL API.
type Client struct {
	// Endpoint defaults to DefaultEndpoint (set it for tests).
	Endpoint string
	// APIKey is sent as the Authorization header value, as Linear expects for
	// personal API keys. For an OAuth access token, set AuthPrefix to "Bearer ".
	APIKey     string
	AuthPrefix string
	HTTP       *http.Client
}

// NewClient builds a client for a personal API key.
func NewClient(apiKey string) *Client {
	return &Client{Endpoint: DefaultEndpoint, APIKey: apiKey, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// APIError is a Linear failure: a non-2xx status or a GraphQL "errors" entry.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("linear: HTTP %d: %s", e.Status, e.Message)
	}
	return "linear: " + e.Message
}

// Do runs a query or mutation. out (if non-nil) receives the "data" object.
func (c *Client) Do(ctx context.Context, query string, vars map[string]any, out any) error {
	b, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	ep := c.Endpoint
	if ep == "" {
		ep = DefaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.AuthPrefix+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("linear: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("linear: read: %w", err)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(raw))
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	if len(env.Errors) > 0 {
		return &APIError{Message: env.Errors[0].Message}
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("linear: decode: %w", err)
		}
	}
	return nil
}

// Issue is the part of a Linear issue the agent needs.
type Issue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// Issue fetches an issue by id or identifier (ENG-123).
func (c *Client) Issue(ctx context.Context, id string) (Issue, error) {
	var out struct {
		Issue *Issue `json:"issue"`
	}
	if err := c.Do(ctx, `query Issue($id: String!) { issue(id: $id) { id identifier title description url } }`, map[string]any{"id": id}, &out); err != nil {
		return Issue{}, err
	}
	if out.Issue == nil {
		return Issue{}, &APIError{Message: "issue not found: " + id}
	}
	return *out.Issue, nil
}

// CreateComment posts a comment on an issue and returns the comment id.
func (c *Client) CreateComment(ctx context.Context, issueID, body string) (string, error) {
	var out struct {
		CommentCreate struct {
			Success bool `json:"success"`
			Comment struct {
				ID string `json:"id"`
			} `json:"comment"`
		} `json:"commentCreate"`
	}
	q := `mutation CommentCreate($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id } } }`
	if err := c.Do(ctx, q, map[string]any{"input": map[string]any{"issueId": issueID, "body": body}}, &out); err != nil {
		return "", err
	}
	if !out.CommentCreate.Success {
		return "", errors.New("linear: commentCreate returned success=false")
	}
	return out.CommentCreate.Comment.ID, nil
}

// Sign computes the Linear-Signature value (hex HMAC-SHA256 of the raw body).
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature checks a Linear-Signature header in constant time.
func VerifySignature(secret, header string, body []byte) error {
	if secret == "" {
		return errors.New("linear: no webhook secret configured")
	}
	got, err := hex.DecodeString(header)
	if err != nil {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrBadSignature
	}
	return nil
}

// ErrBadSignature is a Linear-Signature that does not match the body.
var ErrBadSignature = errors.New("linear: invalid webhook signature")
