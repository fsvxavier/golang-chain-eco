// Package anthropic is the Claude chat model integration (langchain-anthropic)
// over the Anthropic Messages API, with tool use, images, prompt caching
// markers and streaming.
package anthropic

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"os"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

// Chat is a Claude chat model (ChatAnthropic).
type Chat struct {
	Model     string // e.g. "claude-sonnet-5-5"
	APIKey    string // default $ANTHROPIC_API_KEY
	BaseURL   string // default https://api.anthropic.com
	MaxTokens int    // default 4096 (required by the API)
	HTTP      *http.Client
	Defaults  []models.Option
}

// New creates a Claude chat model.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

// ModelID implements models.ChatModel.
func (c *Chat) ModelID() string { return "anthropic:" + c.Model }

func (c *Chat) client() *httpx.Client {
	key := c.APIKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	return &httpx.Client{HTTP: c.HTTP, BaseURL: base, Headers: map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}}
}

type block = map[string]any

func partsToBlocks(m messages.Message) []block {
	var bs []block
	if m.Content != "" {
		bs = append(bs, block{"type": "text", "text": m.Content})
	}
	for _, p := range m.Parts {
		var b block
		switch p.Type {
		case "text":
			b = block{"type": "text", "text": p.Text}
		case "image", "image_url":
			if p.Data != "" {
				b = block{"type": "image", "source": block{"type": "base64", "media_type": p.MimeType, "data": p.Data}}
			} else {
				b = block{"type": "image", "source": block{"type": "url", "url": p.URL}}
			}
		case "file":
			b = block{"type": "document", "source": block{"type": "base64", "media_type": p.MimeType, "data": p.Data}}
		default:
			continue
		}
		for k, v := range p.Extra {
			b[k] = v // e.g. cache_control
		}
		bs = append(bs, b)
	}
	return bs
}

// convert splits out the system prompt and maps messages to API turns,
// merging consecutive tool results into one user turn.
func convert(msgs []messages.Message) ([]block, []block) {
	var system []block
	var turns []block
	push := func(role string, content []block) {
		if n := len(turns); n > 0 && turns[n-1]["role"] == role {
			turns[n-1]["content"] = append(turns[n-1]["content"].([]block), content...)
			return
		}
		turns = append(turns, block{"role": role, "content": content})
	}
	for _, m := range msgs {
		switch m.Role {
		case messages.RoleSystem:
			system = append(system, partsToBlocks(m)...)
		case messages.RoleHuman:
			push("user", partsToBlocks(m))
		case messages.RoleAI:
			bs := partsToBlocks(m)
			for _, tc := range m.ToolCalls {
				args := tc.Args
				if args == nil {
					args = map[string]any{}
				}
				bs = append(bs, block{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": args})
			}
			if len(bs) == 0 {
				bs = []block{{"type": "text", "text": ""}}
			}
			push("assistant", bs)
		case messages.RoleTool:
			tr := block{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Text()}
			if m.Status == "error" {
				tr["is_error"] = true
			}
			push("user", []block{tr})
		}
	}
	return system, turns
}

func (c *Chat) request(msgs []messages.Message, o models.Options, stream bool) map[string]any {
	model := c.Model
	if o.Model != "" {
		model = o.Model
	}
	maxTok := c.MaxTokens
	if o.MaxTokens > 0 {
		maxTok = o.MaxTokens
	}
	if maxTok <= 0 {
		maxTok = 4096
	}
	system, turns := convert(msgs)
	req := map[string]any{"model": model, "messages": turns, "max_tokens": maxTok}
	if len(system) > 0 {
		req["system"] = system
	}
	ts := o.Tools
	choice := o.ToolChoice
	if rf := o.ResponseFormat; rf != nil {
		// Emulate JSON-schema output with a forced tool call.
		ts = append(ts, tools.Definition{Name: rf.Name, Description: "Respond with structured output", Parameters: rf.Schema})
		choice = rf.Name
	}
	if len(ts) > 0 {
		var defs []block
		for _, t := range ts {
			defs = append(defs, block{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
		}
		req["tools"] = defs
	}
	switch choice {
	case "":
	case "auto":
		req["tool_choice"] = block{"type": "auto"}
	case "none":
		req["tool_choice"] = block{"type": "none"}
	case "any", "required":
		req["tool_choice"] = block{"type": "any"}
	default:
		req["tool_choice"] = block{"type": "tool", "name": choice}
	}
	if o.Temperature != nil {
		req["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		req["top_p"] = *o.TopP
	}
	if len(o.Stop) > 0 {
		req["stop_sequences"] = o.Stop
	}
	if stream {
		req["stream"] = true
	}
	for k, v := range o.Extra {
		req[k] = v
	}
	return req
}

type apiUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func (u apiUsage) toUsage() *messages.Usage {
	in := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	return &messages.Usage{
		InputTokens:         in,
		OutputTokens:        u.OutputTokens,
		TotalTokens:         in + u.OutputTokens,
		CacheReadTokens:     u.CacheReadInputTokens,
		CacheCreationTokens: u.CacheCreationInputTokens,
	}
}

type contentBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Input    map[string]any `json:"input"`
	Thinking string         `json:"thinking"`
}

type apiResponse struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      apiUsage       `json:"usage"`
}

// Generate implements models.ChatModel.
func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		var r apiResponse
		if err := c.client().PostJSON(ctx, "/v1/messages", c.request(msgs, o, false), &r); err != nil {
			return messages.Message{}, err
		}
		out := messages.Message{Role: messages.RoleAI, ID: r.ID, Usage: r.Usage.toUsage(),
			ResponseMetadata: map[string]any{"model_name": r.Model, "stop_reason": r.StopReason}}
		for _, b := range r.Content {
			switch b.Type {
			case "text":
				out.Content += b.Text
			case "thinking":
				out.Parts = append(out.Parts, messages.ContentPart{Type: "reasoning", Text: b.Thinking})
			case "tool_use":
				out.ToolCalls = append(out.ToolCalls, messages.ToolCall{ID: b.ID, Name: b.Name, Args: b.Input})
			}
		}
		return structuredToText(out, o), nil
	})
}

// structuredToText turns the emulated response-format tool call into JSON text.
func structuredToText(m messages.Message, o models.Options) messages.Message {
	if o.ResponseFormat == nil {
		return m
	}
	for i, tc := range m.ToolCalls {
		if tc.Name == o.ResponseFormat.Name {
			b, _ := json.Marshal(tc.Args)
			m.Content = string(b)
			m.ToolCalls = append(m.ToolCalls[:i], m.ToolCalls[i+1:]...)
			break
		}
	}
	return m
}

type streamEvent struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	Message      apiResponse  `json:"message"`
	ContentBlock contentBlock `json:"content_block"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage apiUsage `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type streamErr string

func (e streamErr) Error() string { return "anthropic stream: " + string(e) }

// Stream implements models.ChatModel.
func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return func(yield func(messages.Message, error) bool) {
		_, _ = models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
			resp, err := c.client().Post(ctx, "/v1/messages", c.request(msgs, o, true))
			if err != nil {
				yield(messages.Message{}, err)
				return messages.Message{}, err
			}
			defer resp.Body.Close()
			var acc messages.Message
			var id string
			var failed error
			stopped := false
			perr := httpx.SSE(resp.Body, func(_, data string) bool {
				var ev streamEvent
				if json.Unmarshal([]byte(data), &ev) != nil {
					return true
				}
				chunk := messages.Message{Role: messages.RoleAI, ID: id}
				switch ev.Type {
				case "message_start":
					id = ev.Message.ID
					chunk.ID = id
					chunk.Usage = ev.Message.Usage.toUsage()
				case "content_block_start":
					if ev.ContentBlock.Type == "tool_use" {
						chunk.ToolCallChunks = []messages.ToolCallChunk{{Index: ev.Index, ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}}
					} else {
						return true
					}
				case "content_block_delta":
					switch ev.Delta.Type {
					case "text_delta":
						chunk.Content = ev.Delta.Text
					case "input_json_delta":
						chunk.ToolCallChunks = []messages.ToolCallChunk{{Index: ev.Index, Args: ev.Delta.PartialJSON}}
					case "thinking_delta":
						chunk.Parts = []messages.ContentPart{{Type: "reasoning", Text: ev.Delta.Thinking}}
					default:
						return true
					}
				case "message_delta":
					chunk.Usage = &messages.Usage{OutputTokens: ev.Usage.OutputTokens, TotalTokens: ev.Usage.OutputTokens}
					chunk.ResponseMetadata = map[string]any{"stop_reason": ev.Delta.StopReason}
				case "error":
					if ev.Error != nil {
						failed = streamErr(ev.Error.Message)
					}
					return false
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
