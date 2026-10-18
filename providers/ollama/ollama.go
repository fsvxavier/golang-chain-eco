// Package ollama integrates local models served by Ollama (langchain-ollama):
// chat with tools, JSON-schema structured output, streaming and embeddings.
package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"os"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

// Chat is an Ollama chat model (ChatOllama).
type Chat struct {
	Model    string
	BaseURL  string // default $OLLAMA_HOST or http://localhost:11434
	HTTP     *http.Client
	Defaults []models.Option
}

// New creates an Ollama chat model.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

// ModelID implements models.ChatModel.
func (c *Chat) ModelID() string { return "ollama:" + c.Model }

func baseURL(u string) string {
	if u == "" {
		u = os.Getenv("OLLAMA_HOST")
	}
	if u == "" {
		u = "http://localhost:11434"
	}
	return u
}

func (c *Chat) client() *httpx.Client {
	return &httpx.Client{HTTP: c.HTTP, BaseURL: baseURL(c.BaseURL)}
}

type wireCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type wireMsg struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []wireCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
	Thinking  string     `json:"thinking,omitempty"`
}

func convert(msgs []messages.Message) []wireMsg {
	names := map[string]string{}
	var out []wireMsg
	for _, m := range msgs {
		w := wireMsg{Content: m.Text()}
		for _, p := range m.Parts {
			if (p.Type == "image" || p.Type == "image_url") && p.Data != "" {
				w.Images = append(w.Images, p.Data)
			}
		}
		switch m.Role {
		case messages.RoleSystem:
			w.Role = "system"
		case messages.RoleHuman:
			w.Role = "user"
		case messages.RoleAI:
			w.Role = "assistant"
			for _, tc := range m.ToolCalls {
				var c wireCall
				c.Function.Name, c.Function.Arguments = tc.Name, tc.Args
				w.ToolCalls = append(w.ToolCalls, c)
				names[tc.ID] = tc.Name
			}
		case messages.RoleTool:
			w.Role, w.ToolName = "tool", names[m.ToolCallID]
		default:
			continue
		}
		out = append(out, w)
	}
	return out
}

func (c *Chat) request(msgs []messages.Message, o models.Options, stream bool) map[string]any {
	model := c.Model
	if o.Model != "" {
		model = o.Model
	}
	req := map[string]any{"model": model, "messages": convert(msgs), "stream": stream}
	if len(o.Tools) > 0 && o.ToolChoice != "none" {
		var ts []map[string]any
		for _, t := range o.Tools {
			ts = append(ts, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
		}
		req["tools"] = ts
	}
	opts := map[string]any{}
	if o.Temperature != nil {
		opts["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		opts["top_p"] = *o.TopP
	}
	if o.MaxTokens > 0 {
		opts["num_predict"] = o.MaxTokens
	}
	if len(o.Stop) > 0 {
		opts["stop"] = o.Stop
	}
	if len(opts) > 0 {
		req["options"] = opts
	}
	if o.ResponseFormat != nil {
		req["format"] = o.ResponseFormat.Schema
	}
	for k, v := range o.Extra {
		req[k] = v
	}
	return req
}

type chatResp struct {
	Model           string  `json:"model"`
	Message         wireMsg `json:"message"`
	Done            bool    `json:"done"`
	DoneReason      string  `json:"done_reason"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	Error           string  `json:"error"`
}

func (r chatResp) toMessage(callBase int) messages.Message {
	m := messages.Message{Role: messages.RoleAI, Content: r.Message.Content}
	if r.Message.Thinking != "" {
		m.Parts = []messages.ContentPart{{Type: "reasoning", Text: r.Message.Thinking}}
	}
	for i, tc := range r.Message.ToolCalls {
		m.ToolCalls = append(m.ToolCalls, messages.ToolCall{ID: fmt.Sprintf("call_%d", callBase+i), Name: tc.Function.Name, Args: tc.Function.Arguments})
	}
	if r.Done {
		m.Usage = &messages.Usage{InputTokens: r.PromptEvalCount, OutputTokens: r.EvalCount, TotalTokens: r.PromptEvalCount + r.EvalCount}
		m.ResponseMetadata = map[string]any{"model_name": r.Model, "done_reason": r.DoneReason}
	}
	return m
}

// Generate implements models.ChatModel.
func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		var r chatResp
		if err := c.client().PostJSON(ctx, "/api/chat", c.request(msgs, o, false), &r); err != nil {
			return messages.Message{}, err
		}
		if r.Error != "" {
			return messages.Message{}, fmt.Errorf("ollama: %s", r.Error)
		}
		m := r.toMessage(0)
		m.ID = messages.NewID()
		return m, nil
	})
}

// Stream implements models.ChatModel (Ollama streams NDJSON).
func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return func(yield func(messages.Message, error) bool) {
		_, _ = models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
			resp, err := c.client().Post(ctx, "/api/chat", c.request(msgs, o, true))
			if err != nil {
				yield(messages.Message{}, err)
				return messages.Message{}, err
			}
			defer resp.Body.Close()
			id := messages.NewID()
			var acc messages.Message
			calls := 0
			var failed error
			stopped := false
			perr := httpx.NDJSON(resp.Body, func(line []byte) bool {
				var r chatResp
				if json.Unmarshal(line, &r) != nil {
					return true
				}
				if r.Error != "" {
					failed = fmt.Errorf("ollama: %s", r.Error)
					return false
				}
				chunk := r.toMessage(calls)
				calls += len(chunk.ToolCalls)
				chunk.ID = id
				acc = acc.Merge(chunk)
				models.EmitToken(ctx, chunk)
				if !yield(chunk, nil) {
					stopped = true
					return false
				}
				return !r.Done
			})
			if failed == nil {
				failed = perr
			}
			if failed != nil && !stopped {
				yield(messages.Message{}, failed)
			}
			return acc.Finalize(), failed
		})
	}
}

// Embeddings uses Ollama's /api/embed endpoint (OllamaEmbeddings).
type Embeddings struct {
	Model   string
	BaseURL string
	HTTP    *http.Client
}

// EmbedDocuments implements embeddings.Embedder.
func (e *Embeddings) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	var r struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	c := &httpx.Client{HTTP: e.HTTP, BaseURL: baseURL(e.BaseURL)}
	if err := c.PostJSON(ctx, "/api/embed", map[string]any{"model": e.Model, "input": texts}, &r); err != nil {
		return nil, err
	}
	return r.Embeddings, nil
}

// EmbedQuery implements embeddings.Embedder.
func (e *Embeddings) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vs, err := e.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vs) == 0 {
		return nil, fmt.Errorf("ollama: empty embedding response")
	}
	return vs[0], nil
}
