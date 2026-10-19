// Package slack connects Open SWE to Slack: a minimal Web API client, an
// Events API handler that turns mentions and thread replies into agent runs
// (one Slack thread = one openswe thread), and a Runner wrapper that reports
// the outcome back into the thread. Standard library only.
package slack

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
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the Slack Web API root.
const DefaultBaseURL = "https://slack.com/api"

// Client calls the Slack Web API with a bot token (xoxb-...).
type Client struct {
	// BaseURL defaults to DefaultBaseURL (set it for tests).
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient builds a client for a bot token.
func NewClient(botToken string) *Client {
	return &Client{BaseURL: DefaultBaseURL, Token: botToken, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// APIError is a Slack error: either {"ok":false,"error":...} (Code) or a
// non-2xx HTTP status without a Slack body.
type APIError struct {
	Method string
	Code   string
	Status int
	// RetryAfter is set on rate limits (HTTP 429).
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("slack: %s: HTTP %d", e.Method, e.Status)
	}
	return fmt.Sprintf("slack: %s: %s", e.Method, e.Code)
}

// IsCode reports whether err is a Slack error with the given code
// (for example "channel_not_found" or "already_reacted").
func IsCode(err error, code string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Code == code
}

// call posts a JSON body to a Web API method and decodes the Slack envelope
// into out (which may be nil).
func (c *Client) call(ctx context.Context, method string, body map[string]any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/"+method, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("slack: %s: read: %w", method, err)
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode == http.StatusTooManyRequests {
		ra, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return &APIError{Method: method, Code: "ratelimited", Status: resp.StatusCode, RetryAfter: time.Duration(ra) * time.Second}
	}
	if !env.OK {
		if env.Error == "" && resp.StatusCode/100 == 2 {
			env.Error = "invalid_response"
		}
		return &APIError{Method: method, Code: env.Error, Status: resp.StatusCode}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("slack: %s: decode: %w", method, err)
		}
	}
	return nil
}

// PostMessage sends a message (text is the fallback when blocks are given).
// threadTS, when set, posts into that thread. It returns the new message ts.
func (c *Client) PostMessage(ctx context.Context, channel, text, threadTS string, blocks []any) (string, error) {
	body := map[string]any{"channel": channel, "text": text}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}
	if len(blocks) > 0 {
		body["blocks"] = blocks
	}
	var out struct {
		TS string `json:"ts"`
	}
	if err := c.call(ctx, "chat.postMessage", body, &out); err != nil {
		return "", err
	}
	return out.TS, nil
}

// UpdateMessage replaces the text (and optionally the blocks) of a message
// the bot posted.
func (c *Client) UpdateMessage(ctx context.Context, channel, ts, text string, blocks []any) error {
	body := map[string]any{"channel": channel, "ts": ts, "text": text}
	if len(blocks) > 0 {
		body["blocks"] = blocks
	}
	return c.call(ctx, "chat.update", body, nil)
}

// AddReaction adds an emoji reaction (name without colons, e.g. "eyes").
func (c *Client) AddReaction(ctx context.Context, channel, ts, name string) error {
	return c.call(ctx, "reactions.add", map[string]any{"channel": channel, "timestamp": ts, "name": name}, nil)
}

// SignatureVersion is the only version Slack signs requests with.
const SignatureVersion = "v0"

// Sign computes the X-Slack-Signature value for a request (tests, tools).
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s:%s:", SignatureVersion, timestamp)
	mac.Write(body)
	return SignatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
}
