// Package spicedb is a client for SpiceDB's HTTP gateway (--http-enabled)
// and a retriever wrapper that returns only the documents the caller may
// read, enforcing fine-grained permissions in RAG.
//
//	c := &spicedb.Client{URL: "http://localhost:8443", Token: "key"}
//	safe := spicedb.FilterRetriever(inner, c, spicedb.Subject{Type: "user", ID: "alice"},
//		"document", "view", func(d documents.Document) string { return d.Metadata["id"].(string) })
package spicedb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
)

// Client talks to the SpiceDB HTTP gateway.
type Client struct {
	URL   string // http://localhost:8443
	Token string // preshared key
	HTTP  *http.Client
}

// Ref names an object ("document", "doc1").
type Ref struct{ Type, ID string }

// Subject is who is asking ("user", "alice").
type Subject = Ref

func (r Ref) obj() map[string]string {
	return map[string]string{"objectType": r.Type, "objectId": r.ID}
}

func (c *Client) post(ctx context.Context, path string, body any) (io.ReadCloser, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
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
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("spicedb: %s: http %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return resp.Body, nil
}

func (c *Client) call(ctx context.Context, path string, body any, out any) error {
	rc, err := c.post(ctx, path, body)
	if err != nil {
		return err
	}
	defer rc.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, rc)
		return err
	}
	return json.NewDecoder(rc).Decode(out)
}

// WriteSchema replaces the schema.
func (c *Client) WriteSchema(ctx context.Context, schema string) error {
	return c.call(ctx, "/v1/schema/write", map[string]string{"schema": schema}, nil)
}

// Grant touches the relationship resource#relation@subject.
func (c *Client) Grant(ctx context.Context, resource Ref, relation string, subject Subject) error {
	return c.call(ctx, "/v1/relationships/write", map[string]any{"updates": []map[string]any{{
		"operation": "OPERATION_TOUCH",
		"relationship": map[string]any{"resource": resource.obj(), "relation": relation,
			"subject": map[string]any{"object": subject.obj()}}}}}, nil)
}

var fully = map[string]any{"fullyConsistent": true}

// Check reports whether subject has permission on resource.
func (c *Client) Check(ctx context.Context, resource Ref, permission string, subject Subject) (bool, error) {
	var out struct {
		Permissionship string `json:"permissionship"`
	}
	err := c.call(ctx, "/v1/permissions/check", map[string]any{"consistency": fully, "resource": resource.obj(),
		"permission": permission, "subject": map[string]any{"object": subject.obj()}}, &out)
	return out.Permissionship == "PERMISSIONSHIP_HAS_PERMISSION", err
}

// LookupResources lists the ids of resourceType objects subject has permission on.
func (c *Client) LookupResources(ctx context.Context, resourceType, permission string, subject Subject) ([]string, error) {
	rc, err := c.post(ctx, "/v1/permissions/resources", map[string]any{"consistency": fully, "resourceObjectType": resourceType,
		"permission": permission, "subject": map[string]any{"object": subject.obj()}})
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var ids []string
	sc := bufio.NewScanner(rc) // newline-delimited stream of {"result":{...}}
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var line struct {
			Result struct {
				ResourceObjectID string `json:"resourceObjectId"`
			} `json:"result"`
			Error struct{ Message string } `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Error.Message != "" {
			return nil, fmt.Errorf("spicedb: %s", line.Error.Message)
		}
		if line.Result.ResourceObjectID != "" {
			ids = append(ids, line.Result.ResourceObjectID)
		}
	}
	return ids, sc.Err()
}

// FilterRetriever wraps inner so only documents subject can access are
// returned. idOf extracts the SpiceDB object id from a document; documents
// for which it returns "" are dropped (fail closed). A failed check returns
// the error rather than leaking documents.
func FilterRetriever(inner retrievers.Retriever, c *Client, subject Subject, resourceType, permission string, idOf func(documents.Document) string) retrievers.Retriever {
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
			ok, err := c.Check(ctx, Ref{resourceType, id}, permission, subject)
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
