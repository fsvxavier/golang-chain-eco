// Package bedrock implements chat over the Amazon Bedrock Converse API with
// built-in AWS Signature V4 signing (ChatBedrockConverse). Streaming returns
// the complete reply as one chunk: Bedrock's binary event stream is not
// decoded.
package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// Chat is a Bedrock Converse model.
type Chat struct {
	Model       string // model or inference-profile ID
	Region      string // default $AWS_REGION / $AWS_DEFAULT_REGION / us-east-1
	Credentials Credentials
	// BaseURL overrides https://bedrock-runtime.<region>.amazonaws.com (tests, VPC endpoints).
	BaseURL  string
	HTTP     *http.Client
	Defaults []models.Option
	Now      func() time.Time
}

// New creates a Bedrock chat model; credentials come from the environment
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN) unless set.
func New(model string, defaults ...models.Option) *Chat {
	return &Chat{Model: model, Defaults: defaults}
}

// ModelID implements models.ChatModel.
func (c *Chat) ModelID() string { return "bedrock:" + c.Model }

func (c *Chat) request(msgs []messages.Message, o models.Options) map[string]any {
	var system []map[string]any
	var ms []map[string]any
	add := func(role string, blocks ...map[string]any) {
		if n := len(ms); n > 0 && ms[n-1]["role"] == role {
			ms[n-1]["content"] = append(ms[n-1]["content"].([]map[string]any), blocks...)
			return
		}
		ms = append(ms, map[string]any{"role": role, "content": blocks})
	}
	for _, m := range msgs {
		switch m.Role {
		case messages.RoleSystem:
			system = append(system, map[string]any{"text": m.Text()})
		case messages.RoleHuman:
			add("user", map[string]any{"text": m.Text()})
		case messages.RoleAI:
			var blocks []map[string]any
			if t := m.Text(); t != "" {
				blocks = append(blocks, map[string]any{"text": t})
			}
			for _, tc := range m.ToolCalls {
				in := tc.Args
				if in == nil {
					in = map[string]any{}
				}
				blocks = append(blocks, map[string]any{"toolUse": map[string]any{"toolUseId": tc.ID, "name": tc.Name, "input": in}})
			}
			if len(blocks) > 0 {
				add("assistant", blocks...)
			}
		case messages.RoleTool:
			status := "success"
			if m.Status == "error" {
				status = "error"
			}
			add("user", map[string]any{"toolResult": map[string]any{
				"toolUseId": m.ToolCallID, "status": status, "content": []map[string]any{{"text": m.Text()}}}})
		}
	}
	req := map[string]any{"messages": ms}
	if len(system) > 0 {
		req["system"] = system
	}
	inf := map[string]any{}
	if o.Temperature != nil {
		inf["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		inf["topP"] = *o.TopP
	}
	if o.MaxTokens > 0 {
		inf["maxTokens"] = o.MaxTokens
	}
	if len(o.Stop) > 0 {
		inf["stopSequences"] = o.Stop
	}
	if len(inf) > 0 {
		req["inferenceConfig"] = inf
	}
	if len(o.Tools) > 0 {
		var specs []map[string]any
		for _, t := range o.Tools {
			specs = append(specs, map[string]any{"toolSpec": map[string]any{
				"name": t.Name, "description": t.Description, "inputSchema": map[string]any{"json": t.Parameters}}})
		}
		tc := map[string]any{"tools": specs}
		switch o.ToolChoice {
		case "any", "required":
			tc["toolChoice"] = map[string]any{"any": map[string]any{}}
		case "", "auto", "none":
		default:
			tc["toolChoice"] = map[string]any{"tool": map[string]any{"name": o.ToolChoice}}
		}
		req["toolConfig"] = tc
	}
	for k, v := range o.Extra {
		req[k] = v
	}
	return req
}

func (c *Chat) creds() Credentials {
	cr := c.Credentials
	if cr.AccessKeyID == "" {
		cr.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if cr.SecretAccessKey == "" {
		cr.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if cr.SessionToken == "" {
		cr.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
	}
	return cr
}

func (c *Chat) region() string {
	for _, r := range []string{c.Region, os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION")} {
		if r != "" {
			return r
		}
	}
	return "us-east-1"
}

// Generate implements models.ChatModel.
func (c *Chat) Generate(ctx context.Context, msgs []messages.Message, opts ...models.Option) (messages.Message, error) {
	o := models.Apply(append(append([]models.Option(nil), c.Defaults...), opts...)...)
	model := c.Model
	if o.Model != "" {
		model = o.Model
	}
	return models.Track(ctx, c.ModelID(), msgs, func(ctx context.Context) (messages.Message, error) {
		body, err := json.Marshal(c.request(msgs, o))
		if err != nil {
			return messages.Message{}, err
		}
		region := c.region()
		base := c.BaseURL
		if base == "" {
			base = "https://bedrock-runtime." + region + ".amazonaws.com"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/model/"+url.PathEscape(model)+"/converse", bytes.NewReader(body))
		if err != nil {
			return messages.Message{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		now := time.Now
		if c.Now != nil {
			now = c.Now
		}
		Sign(req, body, c.creds(), region, "bedrock", now())
		hc := c.HTTP
		if hc == nil {
			hc = http.DefaultClient
		}
		resp, err := hc.Do(req)
		if err != nil {
			return messages.Message{}, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode/100 != 2 {
			return messages.Message{}, fmt.Errorf("bedrock: http %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
		}
		var r struct {
			Output struct {
				Message struct {
					Content []struct {
						Text    string `json:"text"`
						ToolUse *struct {
							ID    string         `json:"toolUseId"`
							Name  string         `json:"name"`
							Input map[string]any `json:"input"`
						} `json:"toolUse"`
					} `json:"content"`
				} `json:"message"`
			} `json:"output"`
			StopReason string `json:"stopReason"`
			Usage      struct {
				In, Out, Total int
			} `json:"usage"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return messages.Message{}, err
		}
		out := messages.Message{Role: messages.RoleAI, ID: messages.NewID(), ResponseMetadata: map[string]any{"stop_reason": r.StopReason}}
		for _, b := range r.Output.Message.Content {
			out.Content += b.Text
			if b.ToolUse != nil {
				out.ToolCalls = append(out.ToolCalls, messages.ToolCall{ID: b.ToolUse.ID, Name: b.ToolUse.Name, Args: b.ToolUse.Input})
			}
		}
		var u struct {
			Usage struct {
				In    int `json:"inputTokens"`
				Out   int `json:"outputTokens"`
				Total int `json:"totalTokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(raw, &u)
		out.Usage = &messages.Usage{InputTokens: u.Usage.In, OutputTokens: u.Usage.Out, TotalTokens: u.Usage.Total}
		return out, nil
	})
}

// Stream implements models.ChatModel by yielding the whole reply once.
func (c *Chat) Stream(ctx context.Context, msgs []messages.Message, opts ...models.Option) iter.Seq2[messages.Message, error] {
	return func(yield func(messages.Message, error) bool) {
		out, err := c.Generate(ctx, msgs, opts...)
		yield(out, err)
	}
}
