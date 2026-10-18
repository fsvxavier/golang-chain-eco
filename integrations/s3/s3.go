// Package s3 is a minimal Amazon S3 client (SigV4, path-style) plus a
// checkpoint.Saver, a store.Store and a documents loader on top of it. It
// works with any S3-compatible service: AWS S3, MinIO, Tigris, Cloudflare R2,
// Backblaze B2, Wasabi, XNS, DigitalOcean Spaces...
//
//	c := &s3.Client{Endpoint: "https://fly.storage.tigris.dev", Region: "auto",
//		Credentials: bedrock.Credentials{AccessKeyID: "...", SecretAccessKey: "..."}}
//	saver := s3.NewSaver(c, "my-bucket", "checkpoints/")
package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

// Client talks to an S3-compatible endpoint.
type Client struct {
	Endpoint    string // default https://s3.<region>.amazonaws.com
	Region      string // default us-east-1
	Credentials bedrock.Credentials
	HTTP        *http.Client
}

// Error is an S3 error response.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("s3: %d %s: %s", e.Status, e.Code, e.Message) }

// IsNotFound reports whether err is a missing key or bucket.
func IsNotFound(err error) bool {
	e, ok := err.(*Error)
	return ok && (e.Status == http.StatusNotFound)
}

func (c *Client) region() string {
	if c.Region == "" {
		return "us-east-1"
	}
	return c.Region
}

// escKey percent-encodes an object key once, keeping "/" separators.
func escKey(key string) string {
	segs := strings.Split(key, "/")
	for i, s := range segs {
		segs[i] = strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
	}
	return strings.Join(segs, "/")
}

func (c *Client) do(ctx context.Context, method, bucket, key string, query url.Values, body []byte) (*http.Response, error) {
	ep := strings.TrimRight(c.Endpoint, "/")
	if ep == "" {
		ep = "https://s3." + c.region() + ".amazonaws.com"
	}
	u := ep + "/" + url.PathEscape(bucket)
	if key != "" {
		u += "/" + escKey(key)
	}
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(sum[:]))
	bedrock.Sign(req, body, c.Credentials, c.region(), "s3", time.Now())
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
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var x struct{ Code, Message string }
		_ = xml.Unmarshal(raw, &x)
		return nil, &Error{resp.StatusCode, x.Code, x.Message}
	}
	return resp, nil
}

// EnsureBucket creates the bucket if it does not exist.
func (c *Client) EnsureBucket(ctx context.Context, bucket string) error {
	resp, err := c.do(ctx, http.MethodHead, bucket, "", nil, nil)
	if err == nil {
		resp.Body.Close()
		return nil
	}
	if !IsNotFound(err) {
		return err
	}
	resp, err = c.do(ctx, http.MethodPut, bucket, "", nil, nil)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

// Put stores an object.
func (c *Client) Put(ctx context.Context, bucket, key string, data []byte) error {
	resp, err := c.do(ctx, http.MethodPut, bucket, key, nil, data)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

// Get returns an object's bytes; a missing key yields an error satisfying IsNotFound.
func (c *Client) Get(ctx context.Context, bucket, key string) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, bucket, key, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Delete removes an object (deleting a missing key is not an error).
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	resp, err := c.do(ctx, http.MethodDelete, bucket, key, nil, nil)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

// Object is a listing entry.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// List returns every key under prefix in ascending key order (all pages).
func (c *Client) List(ctx context.Context, bucket, prefix string) ([]Object, error) {
	var out []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		resp, err := c.do(ctx, http.MethodGet, bucket, "", q, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Contents []struct {
				Key          string
				Size         int64
				LastModified time.Time
			}
			IsTruncated           bool
			NextContinuationToken string
		}
		err = xml.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			out = append(out, Object{o.Key, o.Size, o.LastModified})
		}
		if !page.IsTruncated {
			return out, nil
		}
		token = page.NextContinuationToken
	}
}
