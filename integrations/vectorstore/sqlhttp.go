package vectorstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// post sends body and returns the response bytes; non-2xx becomes an error
// carrying the start of the response.
func post(ctx context.Context, hc *http.Client, url, ctype string, body []byte, user, pass string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ctype)
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return raw, fmt.Errorf("%s: http %d: %s", url, resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 400)])))
	}
	return raw, nil
}
