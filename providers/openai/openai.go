// Package openai is the OpenAI chat model and embeddings integration
// (langchain-openai). It speaks the Chat Completions API, so it also works
// with any OpenAI-compatible server (vLLM, LM Studio, Groq, Together,
// OpenRouter, DeepSeek...) by setting BaseURL.
package openai

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

// Chat is an OpenAI chat model (ChatOpenAI).
type Chat struct {
	Model   string
	APIKey  string // default $OPENAI_API_KEY
	BaseURL string // default $OPENAI_BASE_URL or https://api.openai.com/v1
	Org     string
	// Azure, when set, targets an Azure OpenAI deployment instead of api.openai.com.
	Azure *Azure
	HTTP  *http.Client
	// Defaults applied to every call.
	Defaults []models.Option

	// ID overrides ModelID (default "openai:<Model>").
	ID string

	// Hooks for servers that speak the Chat Completions dialect on other
	// routes (IBM watsonx.ai, ...). All are optional.
	//
	// Path / StreamPath replace "/chat/completions" (StreamPath defaults to Path).
	Path, StreamPath string
	// Query is appended to every URL, e.g. "version=2024-05-31".
	Query string
	// TokenFunc supplies a bearer token per request (OAuth/IAM); it takes
	// precedence over APIKey.
	TokenFunc func(context.Context) (string, error)
	// Rewrite edits the request body just before it is sent.
	Rewrite func(req map[string]any)
}

// New creates a chat model for the given model id.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

// ModelID implements models.ChatModel.
func (c *Chat) ModelID() string {
	if c.ID != "" {
		return c.ID
	}
	return "openai:" + c.Model
}

// Azure configures Azure OpenAI (AzureChatOpenAI): requests go to
// <Endpoint>/openai/deployments/<Deployment> with an api-key header.
type Azure struct {
	Endpoint   string // default $AZURE_OPENAI_ENDPOINT
	Deployment string
	APIVersion string // default 2024-10-21
	APIKey     string // default $AZURE_OPENAI_API_KEY
}

func (a *Azure) client(hc *http.Client) *httpx.Client {
	ep, key, ver := a.Endpoint, a.APIKey, a.APIVersion
	if ep == "" {
		ep = os.Getenv("AZURE_OPENAI_ENDPOINT")
	}
	if key == "" {
		key = os.Getenv("AZURE_OPENAI_API_KEY")
	}
	if ver == "" {
		ver = "2024-10-21"
	}
	return &httpx.Client{HTTP: hc, BaseURL: strings.TrimRight(ep, "/") + "/openai/deployments/" + a.Deployment,
		Headers: map[string]string{"api-key": key}, Query: "api-version=" + ver}
}

// clientFor builds the HTTP client for one request, resolving TokenFunc.
func (c *Chat) clientFor(ctx context.Context) (*httpx.Client, error) {
	hc := c.client()
	hc.Query = joinQuery(hc.Query, c.Query)
	if c.TokenFunc != nil {
		tok, err := c.TokenFunc(ctx)
		if err != nil {
			return nil, err
		}
		hc.Headers = map[string]string{"Authorization": "Bearer " + tok}
	}
	return hc, nil
}

func joinQuery(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + "&" + b
}

func (c *Chat) client() *httpx.Client {
	if c.Azure != nil {
		return c.Azure.client(c.HTTP)
	}
	key := c.APIKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	base := c.BaseURL
	if base == "" {
		base = os.Getenv("OPENAI_BASE_URL")
	}
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	h := map[string]string{"Authorization": "Bearer " + key}
	if c.Org != "" {
		h["OpenAI-Organization"] = c.Org
	}
	return &httpx.Client{HTTP: c.HTTP, BaseURL: base, Headers: h}
}

type fn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function fn     `json:"function"`
}

type wireMsg struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// convertMessages maps messages to the Chat Completions wire format.
func convertMessages(msgs []messages.Message) []wireMsg {
	out := make([]wireMsg, 0, len(msgs))
	for _, m := range msgs {
		w := wireMsg{Name: m.Name}
		switch m.Role {
		case messages.RoleSystem:
			w.Role = "system"
		case messages.RoleHuman:
			w.Role = "user"
		case messages.RoleAI:
			w.Role = "assistant"
			for _, tc := range m.ToolCalls {
				args, _ := json.Marshal(tc.Args)
				w.ToolCalls = append(w.ToolCalls, toolCall{ID: tc.ID, Type: "function", Function: fn{tc.Name, string(args)}})
			}
		case messages.RoleTool:
			w.Role, w.ToolCallID = "tool", m.ToolCallID
		default:
			continue
		}
		w.Content = content(m)
		out = append(out, w)
	}
	return out
}

func content(m messages.Message) any {
	if len(m.Parts) == 0 {
		return m.Content
	}
	var parts []map[string]any
	if m.Content != "" {
		parts = append(parts, map[string]any{"type": "text", "text": m.Content})
	}
	for _, p := range m.Parts {
		switch p.Type {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": p.Text})
		case "image", "image_url":
			url := p.URL
			if url == "" && p.Data != "" {
				url = "data:" + p.MimeType + ";base64," + p.Data
			}
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
		case "file":
			parts = append(parts, map[string]any{"type": "file", "file": map[string]any{"file_data": "data:" + p.MimeType + ";base64," + p.Data}})
		}
	}
	return parts
}

func (c *Chat) request(msgs []messages.Message, o models.Options, stream bool) map[string]any {
	model := c.Model
	if o.Model != "" {
		model = o.Model
	}
	req := map[string]any{"model": model, "messages": convertMessages(msgs)}
	if len(o.Tools) > 0 {
		var ts []map[string]any
		for _, t := range o.Tools {
			ts = append(ts, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
		}
		req["tools"] = ts
	}
	switch o.ToolChoice {
	case "":
	case "auto", "none":
		req["tool_choice"] = o.ToolChoice
	case "any", "required":
		req["tool_choice"] = "required"
	default:
		req["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": o.ToolChoice}}
	}
	if o.Temperature != nil {
		req["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		req["top_p"] = *o.TopP
	}
	if o.MaxTokens > 0 {
		req["max_completion_tokens"] = o.MaxTokens
	}
	if len(o.Stop) > 0 {
		req["stop"] = o.Stop
	}
	if rf := o.ResponseFormat; rf != nil {
		req["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": rf.Name, "schema": rf.Schema, "strict": rf.Strict}}
	}
	if stream {
		req["stream"] = true
		req["stream_options"] = map[string]any{"include_usage": true}
	}
	for k, v := range o.Extra {
		req[k] = v
	}
	if c.Rewrite != nil {
		c.Rewrite(req)
	}
	return req
}

func orStr(s, d string) string {
	if s != "" {
		return s
	}
	return d
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (u *usage) toUsage() *messages.Usage {
	if u == nil {
		return nil
	}
	return &messages.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, TotalTokens: u.TotalTokens}
}

type choice struct {
	Message      wireResp `json:"message"`
	Delta        wireResp `json:"delta"`
	FinishReason string   `json:"finish_reason"`
}

type wireResp struct {
	Content   *string    `json:"content"`
	Refusal   *string    `json:"refusal"`
	ToolCalls []toolCall `json:"tool_calls"`
}

type response struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   *usage   `json:"usage"`
}

// Generate implements models.ChatModel.
func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		var r response
		hc, err := c.clientFor(ctx)
		if err != nil {
			return messages.Message{}, err
		}
		if err := hc.PostJSON(ctx, orStr(c.Path, "/chat/completions"), c.request(msgs, o, false), &r); err != nil {
			return messages.Message{}, err
		}
		out := messages.Message{Role: messages.RoleAI, ID: r.ID, Usage: r.Usage.toUsage(), ResponseMetadata: map[string]any{"model_name": r.Model}}
		if len(r.Choices) == 0 {
			return out, nil
		}
		ch := r.Choices[0]
		out.ResponseMetadata["finish_reason"] = ch.FinishReason
		if ch.Message.Content != nil {
			out.Content = *ch.Message.Content
		}
		if ch.Message.Refusal != nil {
			out.AdditionalKwargs = map[string]any{"refusal": *ch.Message.Refusal}
		}
		for i, tc := range ch.Message.ToolCalls {
			out.ToolCallChunks = append(out.ToolCallChunks, messages.ToolCallChunk{Index: i, ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
		}
		return out.Finalize(), nil
	})
}

// Stream implements models.ChatModel using server-sent events.
func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return func(yield func(messages.Message, error) bool) {
		_, _ = models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
			hc, err := c.clientFor(ctx)
			var resp *http.Response
			if err == nil {
				resp, err = hc.Post(ctx, orStr(c.StreamPath, orStr(c.Path, "/chat/completions")), c.request(msgs, o, true))
			}
			if err != nil {
				yield(messages.Message{}, err)
				return messages.Message{}, err
			}
			defer resp.Body.Close()
			var acc messages.Message
			stopped := false
			perr := httpx.SSE(resp.Body, func(_, data string) bool {
				var r response
				if err := json.Unmarshal([]byte(data), &r); err != nil {
					return true
				}
				chunk := messages.Message{Role: messages.RoleAI, ID: r.ID, Usage: r.Usage.toUsage()}
				if len(r.Choices) > 0 {
					d := r.Choices[0].Delta
					if d.Content != nil {
						chunk.Content = *d.Content
					}
					for _, tc := range d.ToolCalls {
						idx := 0
						if tc.Index != nil {
							idx = *tc.Index
						}
						chunk.ToolCallChunks = append(chunk.ToolCallChunks, messages.ToolCallChunk{Index: idx, ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
					}
					if fr := r.Choices[0].FinishReason; fr != "" {
						chunk.ResponseMetadata = map[string]any{"finish_reason": fr}
					}
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

// Embeddings is the OpenAI embeddings model (OpenAIEmbeddings).
type Embeddings struct {
	Model      string // default text-embedding-3-small
	Dimensions int
	Chat       Chat // reuses APIKey/BaseURL/HTTP settings
}

// EmbedDocuments implements embeddings.Embedder.
func (e *Embeddings) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	model := e.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	req := map[string]any{"model": model, "input": texts}
	if e.Dimensions > 0 {
		req["dimensions"] = e.Dimensions
	}
	var r struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := e.Chat.client().PostJSON(ctx, "/embeddings", req, &r); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for _, d := range r.Data {
		if d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	return out, nil
}

// EmbedQuery implements embeddings.Embedder.
func (e *Embeddings) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vs, err := e.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}
