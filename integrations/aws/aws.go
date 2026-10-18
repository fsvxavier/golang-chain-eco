// Package aws holds SigV4-signed AWS integrations beyond model calls:
// Knowledge Bases for Amazon Bedrock and Amazon Kendra retrievers, and an
// AWS Lambda invocation tool. Contracts follow the public AWS API
// references; they are covered by httptest-based tests, not by live AWS.
package aws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

// Client signs requests with SigV4.
type Client struct {
	Region      string // default us-east-1
	Credentials bedrock.Credentials
	HTTP        *http.Client
	// Endpoint, when set, replaces the regional endpoint of every service
	// (tests, VPC endpoints).
	Endpoint string
}

func (c *Client) region() string {
	if c.Region == "" {
		return "us-east-1"
	}
	return c.Region
}

func (c *Client) call(ctx context.Context, service, host, path string, headers map[string]string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	base := c.Endpoint
	if base == "" {
		base = "https://" + host + "." + c.region() + ".amazonaws.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	bedrock.Sign(req, b, c.Credentials, c.region(), service, time.Now())
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("aws %s: http %d: %s", service, resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	return raw, nil
}

// KnowledgeBase retrieves passages from a Knowledge Base for Amazon Bedrock.
func (c *Client) KnowledgeBase(knowledgeBaseID string, k int) retrievers.Retriever {
	return retrievers.Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		raw, err := c.call(ctx, "bedrock", "bedrock-agent-runtime", "/knowledgebases/"+url.PathEscape(knowledgeBaseID)+"/retrieve", nil, map[string]any{
			"retrievalQuery":         map[string]any{"text": q},
			"retrievalConfiguration": map[string]any{"vectorSearchConfiguration": map[string]any{"numberOfResults": k}},
		})
		if err != nil {
			return nil, err
		}
		var out struct {
			RetrievalResults []struct {
				Content  struct{ Text string } `json:"content"`
				Score    float64               `json:"score"`
				Location map[string]any        `json:"location"`
				Metadata map[string]any        `json:"metadata"`
			} `json:"retrievalResults"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		docs := make([]documents.Document, len(out.RetrievalResults))
		for i, r := range out.RetrievalResults {
			md := map[string]any{"score": r.Score, "location": r.Location}
			for k, v := range r.Metadata {
				md[k] = v
			}
			docs[i] = documents.Document{PageContent: r.Content.Text, Metadata: md}
		}
		return docs, nil
	})
}

// Kendra retrieves excerpts from an Amazon Kendra index.
func (c *Client) Kendra(indexID string, k int) retrievers.Retriever {
	return retrievers.Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		raw, err := c.call(ctx, "kendra", "kendra", "/", map[string]string{"X-Amz-Target": "AWSKendraFrontendService.Query"},
			map[string]any{"IndexId": indexID, "QueryText": q, "PageSize": k})
		if err != nil {
			return nil, err
		}
		var out struct {
			ResultItems []struct {
				ID              string
				Type            string
				DocumentTitle   struct{ Text string }
				DocumentExcerpt struct{ Text string }
				DocumentURI     string
			}
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		var docs []documents.Document
		for _, r := range out.ResultItems {
			docs = append(docs, documents.Document{PageContent: r.DocumentExcerpt.Text,
				Metadata: map[string]any{"title": r.DocumentTitle.Text, "source": r.DocumentURI, "type": r.Type, "id": r.ID}})
			if k > 0 && len(docs) >= k {
				break
			}
		}
		return docs, nil
	})
}

type lambdaArgs struct {
	Payload map[string]any `json:"payload,omitempty" description:"JSON object passed to the function as its event"`
}

// LambdaTool exposes one Lambda function as an agent tool. Only the named
// function can be called; the synchronous response body is returned.
func (c *Client) LambdaTool(toolName, description, functionName string) tools.Tool {
	return tools.New(toolName, description, func(ctx context.Context, a lambdaArgs) (any, error) {
		payload := a.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		raw, err := c.call(ctx, "lambda", "lambda", "/2015-03-31/functions/"+url.PathEscape(functionName)+"/invocations", nil, payload)
		if err != nil {
			return nil, tools.Errorf("lambda failed: %v", err)
		}
		return string(raw), nil
	})
}
