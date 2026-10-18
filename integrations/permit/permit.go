// Package permit checks permissions with a Permit.io Policy Decision Point
// (cloud or self-hosted) and filters retrieved documents by them.
package permit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
)

// Client talks to a PDP.
type Client struct {
	PDPURL string // default https://cloudpdp.api.permit.io
	APIKey string // default $PERMIT_API_KEY
	Tenant string // default "default"
	HTTP   *http.Client
}

// Allowed asks whether user may perform action on the resource.
func (c *Client) Allowed(ctx context.Context, user, action, resourceType, resourceKey string) (bool, error) {
	tenant := c.Tenant
	if tenant == "" {
		tenant = "default"
	}
	b, _ := json.Marshal(map[string]any{"user": map[string]any{"key": user}, "action": action,
		"resource": map[string]any{"type": resourceType, "key": resourceKey, "tenant": tenant}})
	base := strings.TrimRight(c.PDPURL, "/")
	if base == "" {
		base = "https://cloudpdp.api.permit.io"
	}
	key := c.APIKey
	if key == "" {
		key = os.Getenv("PERMIT_API_KEY")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/allowed", bytes.NewReader(b))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return false, fmt.Errorf("permit: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	var out struct{ Allow bool }
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, err
	}
	return out.Allow, nil
}

// FilterRetriever returns only documents user may perform action on. idOf
// gives the resource key of a document; "" drops it (fail closed). A failed
// check returns the error instead of leaking documents.
func FilterRetriever(inner retrievers.Retriever, c *Client, user, action, resourceType string, idOf func(documents.Document) string) retrievers.Retriever {
	return retrievers.Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		docs, err := inner.Invoke(ctx, q)
		if err != nil {
			return nil, err
		}
		var out []documents.Document
		for _, d := range docs {
			id := idOf(d)
			if id == "" {
				continue
			}
			ok, err := c.Allowed(ctx, user, action, resourceType, id)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, d)
			}
		}
		return out, nil
	})
}
