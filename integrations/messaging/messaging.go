// Package messaging gives agents SMS tools on Plivo and Telnyx. Sending a
// message is an irreversible, outward-facing action: always restrict who the
// agent may text with Allow, and put a human-approval step in front of the
// tool in production.
package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Plivo sends SMS through the Plivo Message API.
type Plivo struct {
	AuthID, AuthToken string
	From              string
	BaseURL           string // default https://api.plivo.com
	HTTP              *http.Client
	// Allow, if set, must approve every destination number.
	Allow func(to string) bool
}

// Telnyx sends SMS through the Telnyx Messages API.
type Telnyx struct {
	APIKey  string
	From    string
	BaseURL string // default https://api.telnyx.com
	HTTP    *http.Client
	Allow   func(to string) bool
}

func send(ctx context.Context, hc *http.Client, url string, body any, setAuth func(*http.Request)) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuth(req)
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	return raw, nil
}

// Send texts one message and returns the provider's message id.
func (p *Plivo) Send(ctx context.Context, to, text string) (string, error) {
	if p.Allow != nil && !p.Allow(to) {
		return "", fmt.Errorf("messaging: destination %s is not allowed", to)
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = "https://api.plivo.com"
	}
	raw, err := send(ctx, p.HTTP, base+"/v1/Account/"+p.AuthID+"/Message/", map[string]string{"src": p.From, "dst": to, "text": text},
		func(r *http.Request) { r.SetBasicAuth(p.AuthID, p.AuthToken) })
	if err != nil {
		return "", err
	}
	var out struct {
		MessageUUID []string `json:"message_uuid"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.MessageUUID) == 0 {
		return "", nil
	}
	return out.MessageUUID[0], nil
}

// Send texts one message and returns the provider's message id.
func (t *Telnyx) Send(ctx context.Context, to, text string) (string, error) {
	if t.Allow != nil && !t.Allow(to) {
		return "", fmt.Errorf("messaging: destination %s is not allowed", to)
	}
	base := strings.TrimRight(t.BaseURL, "/")
	if base == "" {
		base = "https://api.telnyx.com"
	}
	raw, err := send(ctx, t.HTTP, base+"/v2/messages", map[string]string{"from": t.From, "to": to, "text": text},
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+t.APIKey) })
	if err != nil {
		return "", err
	}
	var out struct{ Data struct{ ID string } }
	_ = json.Unmarshal(raw, &out)
	return out.Data.ID, nil
}

type smsArgs struct {
	To   string `json:"to" description:"Destination phone number in E.164 format, e.g. +14155550123"`
	Text string `json:"text" description:"Message body"`
}

// Sender is implemented by Plivo and Telnyx.
type Sender interface {
	Send(ctx context.Context, to, text string) (string, error)
}

// Tool exposes a Sender as an agent tool named name.
func Tool(name string, s Sender) tools.Tool {
	return tools.New(name, "Send an SMS text message. This cannot be undone.", func(ctx context.Context, a smsArgs) (any, error) {
		id, err := s.Send(ctx, a.To, a.Text)
		if err != nil {
			return nil, tools.Errorf("sms failed: %v", err)
		}
		return "sent, id " + id, nil
	})
}
