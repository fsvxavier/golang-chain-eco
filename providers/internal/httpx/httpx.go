// Package httpx holds the HTTP/JSON/SSE plumbing shared by providers.
package httpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client posts JSON requests.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Headers map[string]string
	// Query is appended to every URL (e.g. "api-version=2024-10-21").
	Query string
}

// Post sends body as JSON to path and returns the open response. Non-2xx
// responses are turned into errors that include the response body.
func (c *Client) Post(ctx context.Context, path string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, &StatusError{Code: resp.StatusCode, Body: string(msg)}
	}
	return resp, nil
}

func (c *Client) url(path string) string {
	u := strings.TrimRight(c.BaseURL, "/") + path
	if c.Query != "" {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + c.Query
	}
	return u
}

// PostJSON posts body and decodes the JSON response into out.
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	resp, err := c.Post(ctx, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// StatusError is a non-2xx HTTP response.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("http %d: %s", e.Code, e.Body) }

// Retryable reports whether the status is worth retrying (429/5xx).
func (e *StatusError) Retryable() bool { return e.Code == 429 || e.Code >= 500 }

// SSE calls fn with the data of each server-sent event until the stream ends,
// fn returns false, or a "[DONE]" sentinel arrives. event is the event name.
func SSE(r io.Reader, fn func(event, data string) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	var event string
	var data []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				d := strings.Join(data, "\n")
				if d == "[DONE]" || !fn(event, d) {
					return nil
				}
			}
			event, data = "", nil
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(line[5:], " "))
		}
	}
	if len(data) > 0 && strings.Join(data, "\n") != "[DONE]" {
		fn(event, strings.Join(data, "\n"))
	}
	return sc.Err()
}

// NDJSON calls fn with each line of a newline-delimited JSON stream.
func NDJSON(r io.Reader, fn func(line []byte) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		if !fn(sc.Bytes()) {
			return nil
		}
	}
	return sc.Err()
}
