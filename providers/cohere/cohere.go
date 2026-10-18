// Package cohere implements Cohere v2 chat, embeddings and rerank
// (ChatCohere, CohereEmbeddings, CohereRerank).
package cohere

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"os"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

// Chat is a Cohere chat model.
type Chat struct {
	Model    string
	APIKey   string // default $COHERE_API_KEY
	BaseURL  string // default https://api.cohere.com
	HTTP     *http.Client
	Defaults []models.Option
}

// New creates a Cohere chat model.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

// ModelID implements models.ChatModel.
func (c *Chat) ModelID() string { return "cohere:" + c.Model }

func client(key, base string, hc *http.Client) *httpx.Client {
	if key == "" {
		key = os.Getenv("COHERE_API_KEY")
	}
	if base == "" {
		base = "https://api.cohere.com"
	}
	return &httpx.Client{HTTP: hc, BaseURL: base, Headers: map[string]string{"Authorization": "Bearer " + key}}
}

func (c *Chat) request(msgs []messages.Message, o models.Options, stream bool) map[string]any {
	model := c.Model
	if o.Model != "" {
		model = o.Model
	}
	var ms []map[string]any
	for _, m := range msgs {
		switch m.Role {
		case messages.RoleSystem:
			ms = append(ms, map[string]any{"role": "system", "content": m.Text()})
		case messages.RoleHuman:
			ms = append(ms, map[string]any{"role": "user", "content": m.Text()})
		case messages.RoleAI:
			x := map[string]any{"role": "assistant"}
			if t := m.Text(); t != "" {
				x["content"] = t
			}
			var tcs []map[string]any
			for _, tc := range m.ToolCalls {
				args, _ := json.Marshal(tc.Args)
				tcs = append(tcs, map[string]any{"id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": string(args)}})
			}
			if len(tcs) > 0 {
				x["tool_calls"] = tcs
			}
			ms = append(ms, x)
		case messages.RoleTool:
			ms = append(ms, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Text()})
		}
	}
	req := map[string]any{"model": model, "messages": ms}
	if len(o.Tools) > 0 {
		var ts []map[string]any
		for _, t := range o.Tools {
			ts = append(ts, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
		}
		req["tools"] = ts
		if o.ToolChoice == "any" || o.ToolChoice == "required" {
			req["tool_choice"] = "REQUIRED"
		} else if o.ToolChoice == "none" {
			req["tool_choice"] = "NONE"
		}
	}
	if o.Temperature != nil {
		req["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		req["p"] = *o.TopP
	}
	if o.MaxTokens > 0 {
		req["max_tokens"] = o.MaxTokens
	}
	if len(o.Stop) > 0 {
		req["stop_sequences"] = o.Stop
	}
	if rf := o.ResponseFormat; rf != nil {
		req["response_format"] = map[string]any{"type": "json_object", "json_schema": rf.Schema}
	}
	if stream {
		req["stream"] = true
	}
	for k, v := range o.Extra {
		req[k] = v
	}
	return req
}

type wireCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type tokens struct {
	In  int `json:"input_tokens"`
	Out int `json:"output_tokens"`
}

func (t *tokens) usage() *messages.Usage {
	if t == nil {
		return nil
	}
	return &messages.Usage{InputTokens: t.In, OutputTokens: t.Out, TotalTokens: t.In + t.Out}
}

// Generate implements models.ChatModel.
func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		var r struct {
			ID      string `json:"id"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				ToolCalls []wireCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
			Usage        struct {
				Tokens *tokens `json:"tokens"`
			} `json:"usage"`
		}
		if err := client(c.APIKey, c.BaseURL, c.HTTP).PostJSON(ctx, "/v2/chat", c.request(msgs, o, false), &r); err != nil {
			return messages.Message{}, err
		}
		out := messages.Message{Role: messages.RoleAI, ID: r.ID, Usage: r.Usage.Tokens.usage(), ResponseMetadata: map[string]any{"finish_reason": r.FinishReason}}
		for _, b := range r.Message.Content {
			if b.Type == "text" {
				out.Content += b.Text
			}
		}
		for i, tc := range r.Message.ToolCalls {
			out.ToolCallChunks = append(out.ToolCallChunks, messages.ToolCallChunk{Index: i, ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
		}
		return out.Finalize(), nil
	})
}

// Stream implements models.ChatModel.
func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return func(yield func(messages.Message, error) bool) {
		_, _ = models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
			resp, err := client(c.APIKey, c.BaseURL, c.HTTP).Post(ctx, "/v2/chat", c.request(msgs, o, true))
			if err != nil {
				yield(messages.Message{}, err)
				return messages.Message{}, err
			}
			defer resp.Body.Close()
			var acc messages.Message
			stopped := false
			perr := httpx.SSE(resp.Body, func(_, data string) bool {
				var ev struct {
					Type  string `json:"type"`
					Index int    `json:"index"`
					Delta struct {
						Message struct {
							Content   struct{ Text string } `json:"content"`
							ToolCalls wireCall              `json:"tool_calls"`
						} `json:"message"`
						Usage struct {
							Tokens *tokens `json:"tokens"`
						} `json:"usage"`
						FinishReason string `json:"finish_reason"`
					} `json:"delta"`
				}
				if json.Unmarshal([]byte(data), &ev) != nil {
					return true
				}
				chunk := messages.Message{Role: messages.RoleAI}
				switch ev.Type {
				case "content-delta":
					chunk.Content = ev.Delta.Message.Content.Text
				case "tool-call-start", "tool-call-delta":
					tc := ev.Delta.Message.ToolCalls
					chunk.ToolCallChunks = []messages.ToolCallChunk{{Index: ev.Index, ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments}}
				case "message-end":
					chunk.Usage = ev.Delta.Usage.Tokens.usage()
				default:
					return true
				}
				acc = acc.Merge(chunk)
				models.EmitToken(ctx, chunk)
				if !yield(chunk, nil) {
					stopped = true
					return false
				}
				return true
			})
			if perr != nil && !stopped {
				yield(messages.Message{}, perr)
			}
			return acc.Finalize(), perr
		})
	}
}

// Embeddings is CohereEmbeddings.
type Embeddings struct {
	Model   string // default embed-v4.0
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

func (e *Embeddings) embed(ctx context.Context, texts []string, kind string) ([][]float32, error) {
	model := e.Model
	if model == "" {
		model = "embed-v4.0"
	}
	var r struct {
		Embeddings struct {
			Float [][]float32 `json:"float"`
		} `json:"embeddings"`
	}
	err := client(e.APIKey, e.BaseURL, e.HTTP).PostJSON(ctx, "/v2/embed",
		map[string]any{"model": model, "texts": texts, "input_type": kind, "embedding_types": []string{"float"}}, &r)
	return r.Embeddings.Float, err
}

// EmbedDocuments implements embeddings.Embedder.
func (e *Embeddings) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts, "search_document")
}

// EmbedQuery implements embeddings.Embedder.
func (e *Embeddings) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vs, err := e.embed(ctx, []string{text}, "search_query")
	if err != nil || len(vs) == 0 {
		return nil, err
	}
	return vs[0], nil
}

// Reranker reorders documents by relevance to a query (CohereRerank).
type Reranker struct {
	Model   string // default rerank-v3.5
	APIKey  string
	BaseURL string
	HTTP    *http.Client
	TopN    int
	// Path overrides the endpoint (default /v2/rerank; Jina and Voyage use /v1/rerank).
	Path string
}

// Rerank returns docs sorted by relevance with the score stored in
// Metadata["relevance_score"].
func (r *Reranker) Rerank(ctx context.Context, query string, docs []documents.Document) ([]documents.Document, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	model := r.Model
	if model == "" {
		model = "rerank-v3.5"
	}
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.PageContent
	}
	body := map[string]any{"model": model, "query": query, "documents": texts}
	if r.TopN > 0 {
		body["top_n"] = r.TopN
	}
	type item struct {
		Index int     `json:"index"`
		Score float64 `json:"relevance_score"`
	}
	var resp struct {
		Results []item `json:"results"` // Cohere, Jina
		Data    []item `json:"data"`    // Voyage
	}
	path := r.Path
	if path == "" {
		path = "/v2/rerank"
	}
	if err := client(r.APIKey, r.BaseURL, r.HTTP).PostJSON(ctx, path, body, &resp); err != nil {
		return nil, err
	}
	items := append(resp.Results, resp.Data...)
	out := make([]documents.Document, 0, len(items))
	for _, x := range items {
		if x.Index < 0 || x.Index >= len(docs) {
			continue
		}
		d := docs[x.Index]
		md := map[string]any{}
		for k, v := range d.Metadata {
			md[k] = v
		}
		md["relevance_score"] = x.Score
		d.Metadata = md
		out = append(out, d)
	}
	return out, nil
}
