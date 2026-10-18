// Package replicate is the Replicate chat integration (langchain-community
// Replicate): language models hosted on replicate.com, called through the
// predictions API. Text only: no tool calling and no token streaming (Stream
// yields the finished answer as one chunk).
//
//	m := replicate.New("meta/meta-llama-3-8b-instruct")   // $REPLICATE_API_TOKEN
package replicate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

// Chat is a Replicate-hosted language model.
type Chat struct {
	Model    string // "owner/name"
	APIKey   string // default $REPLICATE_API_TOKEN
	BaseURL  string // default https://api.replicate.com/v1
	HTTP     *http.Client
	Defaults []models.Option
	// PollEvery is the polling interval for predictions that outlive the
	// synchronous wait (default 1s).
	PollEvery time.Duration
}

var _ models.ChatModel = (*Chat)(nil)

// New creates a chat model for owner/name.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

func (c *Chat) ModelID() string { return "replicate:" + c.Model }

type prediction struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`
	Error  any             `json:"error"`
	URLs   struct {
		Get string `json:"get"`
	} `json:"urls"`
	Metrics struct {
		InputTokens  int `json:"input_token_count"`
		OutputTokens int `json:"output_token_count"`
	} `json:"metrics"`
}

// prompt flattens a conversation: system messages go to system_prompt, the
// rest becomes a plain-text transcript ending with "Assistant:" unless the
// conversation is a single user message.
func prompt(msgs []messages.Message) (system, text string) {
	var sys, lines []string
	var humans int
	for _, m := range msgs {
		switch m.Role {
		case messages.RoleSystem:
			sys = append(sys, m.Text())
		case messages.RoleHuman:
			humans++
			lines = append(lines, "User: "+m.Text())
		case messages.RoleAI:
			lines = append(lines, "Assistant: "+m.Text())
		}
	}
	if len(lines) == 1 && humans == 1 {
		return strings.Join(sys, "\n"), strings.TrimPrefix(lines[0], "User: ")
	}
	return strings.Join(sys, "\n"), strings.Join(lines, "\n") + "\nAssistant:"
}

func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		key := c.APIKey
		if key == "" {
			key = os.Getenv("REPLICATE_API_TOKEN")
		}
		base := c.BaseURL
		if base == "" {
			base = "https://api.replicate.com/v1"
		}
		model := c.Model
		if o.Model != "" {
			model = o.Model
		}
		sys, text := prompt(msgs)
		in := map[string]any{"prompt": text}
		if sys != "" {
			in["system_prompt"] = sys
		}
		if o.Temperature != nil {
			in["temperature"] = *o.Temperature
		}
		if o.TopP != nil {
			in["top_p"] = *o.TopP
		}
		if o.MaxTokens > 0 {
			in["max_tokens"] = o.MaxTokens
		}
		if len(o.Stop) > 0 {
			in["stop_sequences"] = strings.Join(o.Stop, ",")
		}
		for k, v := range o.Extra {
			in[k] = v
		}
		hc := &httpx.Client{HTTP: c.HTTP, BaseURL: base, Headers: map[string]string{"Authorization": "Bearer " + key, "Prefer": "wait"}}
		var p prediction
		if err := hc.PostJSON(ctx, "/models/"+model+"/predictions", map[string]any{"input": in}, &p); err != nil {
			return messages.Message{}, err
		}
		every := c.PollEvery
		if every <= 0 {
			every = time.Second
		}
		for p.Status == "starting" || p.Status == "processing" {
			select {
			case <-ctx.Done():
				return messages.Message{}, ctx.Err()
			case <-time.After(every):
			}
			if err := c.poll(ctx, key, p.URLs.Get, &p); err != nil {
				return messages.Message{}, err
			}
		}
		if p.Status != "succeeded" {
			return messages.Message{}, fmt.Errorf("replicate: prediction %s %s: %v", p.ID, p.Status, p.Error)
		}
		out := messages.Message{Role: messages.RoleAI, ID: p.ID, Content: text0(p.Output),
			ResponseMetadata: map[string]any{"model_name": model, "finish_reason": "stop"}}
		if p.Metrics.InputTokens+p.Metrics.OutputTokens > 0 {
			out.Usage = &messages.Usage{InputTokens: p.Metrics.InputTokens, OutputTokens: p.Metrics.OutputTokens, TotalTokens: p.Metrics.InputTokens + p.Metrics.OutputTokens}
		}
		return out, nil
	})
}

func (c *Chat) poll(ctx context.Context, key, url string, p *prediction) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &httpx.StatusError{Code: resp.StatusCode, Body: string(b)}
	}
	return json.NewDecoder(resp.Body).Decode(p)
}

// text0 turns the output (a string, or an array of string tokens) into text.
func text0(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.Join(parts, "")
	}
	return string(raw)
}

func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	return func(yield func(messages.Message, error) bool) {
		m, err := c.Generate(ctx, msgs, opts...)
		if err != nil {
			yield(messages.Message{}, err)
			return
		}
		models.EmitToken(ctx, m)
		yield(m, nil)
	}
}
