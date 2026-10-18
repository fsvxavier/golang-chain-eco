// Package vectara is a client for Vectara's v2 REST API (managed RAG: it
// embeds and ranks on the server, so it is a retriever rather than a
// vectorstore.Backend that takes vectors).
package vectara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
)

// Client talks to one Vectara corpus.
type Client struct {
	APIKey    string // default $VECTARA_API_KEY
	CorpusKey string
	BaseURL   string // default https://api.vectara.io
	HTTP      *http.Client
}

func (c *Client) call(ctx context.Context, path string, body, out any) error {
	b, _ := json.Marshal(body)
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.vectara.io"
	}
	key := c.APIKey
	if key == "" {
		key = os.Getenv("VECTARA_API_KEY")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v2/corpora/"+url.PathEscape(c.CorpusKey)+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", key)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("vectara: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Index adds or replaces one document (id required) in the corpus.
func (c *Client) Index(ctx context.Context, id, text string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	return c.call(ctx, "/documents", map[string]any{"id": id, "type": "core", "metadata": metadata,
		"document_parts": []map[string]any{{"text": text}}}, nil)
}

// Search returns the top-k passages for query, best first.
func (c *Client) Search(ctx context.Context, query string, k int) ([]documents.Document, error) {
	var out struct {
		SearchResults []struct {
			Text             string         `json:"text"`
			Score            float64        `json:"score"`
			DocumentID       string         `json:"document_id"`
			PartMetadata     map[string]any `json:"part_metadata"`
			DocumentMetadata map[string]any `json:"document_metadata"`
		} `json:"search_results"`
	}
	if err := c.call(ctx, "/query", map[string]any{"query": query, "search": map[string]any{"limit": k}}, &out); err != nil {
		return nil, err
	}
	docs := make([]documents.Document, len(out.SearchResults))
	for i, r := range out.SearchResults {
		md := map[string]any{"document_id": r.DocumentID, "score": r.Score}
		for k, v := range r.DocumentMetadata {
			md[k] = v
		}
		for k, v := range r.PartMetadata {
			md[k] = v
		}
		docs[i] = documents.Document{PageContent: r.Text, Metadata: md}
	}
	return docs, nil
}

// Retriever returns the top-k passages as a retrievers.Retriever.
func (c *Client) Retriever(k int) retrievers.Retriever {
	return retrievers.Func(func(ctx context.Context, q string) ([]documents.Document, error) { return c.Search(ctx, q, k) })
}
