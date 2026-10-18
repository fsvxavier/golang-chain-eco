// Package google implements Gemini chat and embeddings over the Generative
// Language REST API, and Vertex AI when Vertex is set (ChatGoogleGenerativeAI,
// ChatVertexAI).
package google

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"os"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

// Vertex selects Vertex AI instead of the public Gemini API.
type Vertex struct {
	Project  string // default $GOOGLE_CLOUD_PROJECT
	Location string // default $GOOGLE_CLOUD_LOCATION or us-central1
	// Token returns an OAuth2 access token (e.g. from gcloud or a service
	// account library). Required.
	Token func(ctx context.Context) (string, error)
}

// Chat is a Gemini chat model.
type Chat struct {
	Model    string
	APIKey   string // default $GOOGLE_API_KEY / $GEMINI_API_KEY
	BaseURL  string // default https://generativelanguage.googleapis.com/v1beta
	Vertex   *Vertex
	HTTP     *http.Client
	Defaults []models.Option
}

// New creates a Gemini chat model.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

// ModelID implements models.ChatModel.
func (c *Chat) ModelID() string { return "google:" + c.Model }

func (c *Chat) client(ctx context.Context, model string) (*httpx.Client, string, error) {
	if v := c.Vertex; v != nil {
		if v.Token == nil {
			return nil, "", fmt.Errorf("google: Vertex.Token is required")
		}
		tok, err := v.Token(ctx)
		if err != nil {
			return nil, "", err
		}
		proj, loc := orEnv(v.Project, "GOOGLE_CLOUD_PROJECT"), orEnv(v.Location, "GOOGLE_CLOUD_LOCATION")
		if loc == "" {
			loc = "us-central1"
		}
		base := c.BaseURL
		if base == "" {
			base = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1", loc)
		}
		return &httpx.Client{HTTP: c.HTTP, BaseURL: base, Headers: map[string]string{"Authorization": "Bearer " + tok}},
			fmt.Sprintf("/projects/%s/locations/%s/publishers/google/models/%s", proj, loc, model), nil
	}
	key := c.APIKey
	if key == "" {
		key = orEnv("", "GOOGLE_API_KEY")
	}
	if key == "" {
		key = os.Getenv("GEMINI_API_KEY")
	}
	base := c.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &httpx.Client{HTTP: c.HTTP, BaseURL: base, Headers: map[string]string{"x-goog-api-key": key}}, "/models/" + model, nil
}

func orEnv(v, env string) string {
	if v != "" {
		return v
	}
	return os.Getenv(env)
}

var dropKeys = map[string]bool{"additionalProperties": true, "$schema": true, "title": true, "default": true}

// cleanSchema removes JSON Schema keywords Gemini rejects.
func cleanSchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if !dropKeys[k] {
				out[k] = cleanSchema(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cleanSchema(e)
		}
		return out
	}
	return v
}

func (c *Chat) request(msgs []messages.Message, o models.Options) map[string]any {
	names := map[string]string{} // tool call id -> function name
	var contents []map[string]any
	var system []map[string]any
	add := func(role string, parts ...map[string]any) {
		if n := len(contents); n > 0 && contents[n-1]["role"] == role {
			contents[n-1]["parts"] = append(contents[n-1]["parts"].([]map[string]any), parts...)
			return
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	for _, m := range msgs {
		switch m.Role {
		case messages.RoleSystem:
			system = append(system, map[string]any{"text": m.Text()})
		case messages.RoleHuman:
			var parts []map[string]any
			if len(m.Parts) == 0 {
				parts = append(parts, map[string]any{"text": m.Text()})
			}
			for _, p := range m.Parts {
				switch p.Type {
				case "text":
					parts = append(parts, map[string]any{"text": p.Text})
				case "image", "file":
					parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": p.MimeType, "data": p.Data}})
				case "image_url":
					parts = append(parts, map[string]any{"fileData": map[string]any{"mimeType": p.MimeType, "fileUri": p.URL}})
				}
			}
			add("user", parts...)
		case messages.RoleAI:
			var parts []map[string]any
			if t := m.Text(); t != "" {
				parts = append(parts, map[string]any{"text": t})
			}
			for _, tc := range m.ToolCalls {
				names[tc.ID] = tc.Name
				args := tc.Args
				if args == nil {
					args = map[string]any{}
				}
				parts = append(parts, map[string]any{"functionCall": map[string]any{"name": tc.Name, "args": args}})
			}
			if len(parts) > 0 {
				add("model", parts...)
			}
		case messages.RoleTool:
			name := m.Name
			if name == "" {
				name = names[m.ToolCallID]
			}
			resp := map[string]any{"result": m.Text()}
			if m.Status == "error" {
				resp = map[string]any{"error": m.Text()}
			}
			add("user", map[string]any{"functionResponse": map[string]any{"name": name, "response": resp}})
		}
	}
	req := map[string]any{"contents": contents}
	if len(system) > 0 {
		req["systemInstruction"] = map[string]any{"parts": system}
	}
	if len(o.Tools) > 0 {
		var decls []map[string]any
		for _, t := range o.Tools {
			decls = append(decls, map[string]any{"name": t.Name, "description": t.Description, "parameters": cleanSchema(t.Parameters)})
		}
		req["tools"] = []map[string]any{{"functionDeclarations": decls}}
		switch o.ToolChoice {
		case "":
		case "auto":
			req["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}
		case "none":
			req["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}}
		case "any", "required":
			req["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}
		default:
			req["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY", "allowedFunctionNames": []string{o.ToolChoice}}}
		}
	}
	gc := map[string]any{}
	if o.Temperature != nil {
		gc["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		gc["topP"] = *o.TopP
	}
	if o.MaxTokens > 0 {
		gc["maxOutputTokens"] = o.MaxTokens
	}
	if len(o.Stop) > 0 {
		gc["stopSequences"] = o.Stop
	}
	if rf := o.ResponseFormat; rf != nil {
		gc["responseMimeType"] = "application/json"
		gc["responseSchema"] = cleanSchema(rf.Schema)
	}
	if len(gc) > 0 {
		req["generationConfig"] = gc
	}
	for k, v := range o.Extra {
		req[k] = v
	}
	return req
}

type wirePart struct {
	Text         string `json:"text"`
	Thought      bool   `json:"thought"`
	FunctionCall *struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	} `json:"functionCall"`
}

type wireResp struct {
	Candidates []struct {
		Content struct {
			Parts []wirePart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Usage *struct {
		Prompt int `json:"promptTokenCount"`
		Out    int `json:"candidatesTokenCount"`
		Total  int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

func (r wireResp) message(idPrefix string) messages.Message {
	out := messages.Message{Role: messages.RoleAI}
	if r.Usage != nil {
		out.Usage = &messages.Usage{InputTokens: r.Usage.Prompt, OutputTokens: r.Usage.Out, TotalTokens: r.Usage.Total}
	}
	if r.PromptFeedback != nil && r.PromptFeedback.BlockReason != "" {
		out.ResponseMetadata = map[string]any{"block_reason": r.PromptFeedback.BlockReason}
	}
	if len(r.Candidates) == 0 {
		return out
	}
	c := r.Candidates[0]
	if c.FinishReason != "" {
		if out.ResponseMetadata == nil {
			out.ResponseMetadata = map[string]any{}
		}
		out.ResponseMetadata["finish_reason"] = c.FinishReason
	}
	n := 0
	for _, p := range c.Content.Parts {
		switch {
		case p.FunctionCall != nil:
			out.ToolCalls = append(out.ToolCalls, messages.ToolCall{ID: fmt.Sprintf("%s_%d", idPrefix, n), Name: p.FunctionCall.Name, Args: p.FunctionCall.Args})
			n++
		case !p.Thought:
			out.Content += p.Text
		}
	}
	return out
}

// Generate implements models.ChatModel.
func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	model := c.Model
	if o.Model != "" {
		model = o.Model
	}
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		cl, base, err := c.client(ctx, model)
		if err != nil {
			return messages.Message{}, err
		}
		var r wireResp
		if err := cl.PostJSON(ctx, base+":generateContent", c.request(msgs, o), &r); err != nil {
			return messages.Message{}, err
		}
		out := r.message("call")
		out.ID = messages.NewID()
		return out, nil
	})
}

// Stream implements models.ChatModel over server-sent events.
func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	model := c.Model
	if o.Model != "" {
		model = o.Model
	}
	return func(yield func(messages.Message, error) bool) {
		_, _ = models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
			cl, base, err := c.client(ctx, model)
			if err != nil {
				yield(messages.Message{}, err)
				return messages.Message{}, err
			}
			resp, err := cl.Post(ctx, base+":streamGenerateContent?alt=sse", c.request(msgs, o))
			if err != nil {
				yield(messages.Message{}, err)
				return messages.Message{}, err
			}
			defer resp.Body.Close()
			id := messages.NewID()
			var acc messages.Message
			calls := 0
			stopped := false
			perr := httpx.SSE(resp.Body, func(_, data string) bool {
				var r wireResp
				if json.Unmarshal([]byte(data), &r) != nil {
					return true
				}
				chunk := r.message(fmt.Sprintf("call%d", calls))
				chunk.ID = id
				for i := range chunk.ToolCalls { // each chunk numbers its own calls; keep IDs unique
					chunk.ToolCalls[i].ID = fmt.Sprintf("call_%d", calls)
					calls++
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

// Embeddings is the Gemini embeddings model (GoogleGenerativeAIEmbeddings).
type Embeddings struct {
	Model   string // default gemini-embedding-001
	APIKey  string
	BaseURL string
	// TaskType, e.g. RETRIEVAL_DOCUMENT; queries use RETRIEVAL_QUERY.
	HTTP *http.Client
}

func (e *Embeddings) batch(ctx context.Context, texts []string, task string) ([][]float32, error) {
	model := e.Model
	if model == "" {
		model = "gemini-embedding-001"
	}
	c := Chat{APIKey: e.APIKey, BaseURL: e.BaseURL, HTTP: e.HTTP}
	cl, _, err := c.client(ctx, model)
	if err != nil {
		return nil, err
	}
	reqs := make([]map[string]any, len(texts))
	for i, t := range texts {
		reqs[i] = map[string]any{"model": "models/" + model, "taskType": task, "content": map[string]any{"parts": []map[string]any{{"text": t}}}}
	}
	var r struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := cl.PostJSON(ctx, "/models/"+url.PathEscape(model)+":batchEmbedContents", map[string]any{"requests": reqs}, &r); err != nil {
		return nil, err
	}
	out := make([][]float32, len(r.Embeddings))
	for i, v := range r.Embeddings {
		out[i] = v.Values
	}
	return out, nil
}

// EmbedDocuments implements embeddings.Embedder.
func (e *Embeddings) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.batch(ctx, texts, "RETRIEVAL_DOCUMENT")
}

// EmbedQuery implements embeddings.Embedder.
func (e *Embeddings) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vs, err := e.batch(ctx, []string{text}, "RETRIEVAL_QUERY")
	if err != nil || len(vs) == 0 {
		return nil, err
	}
	return vs[0], nil
}
