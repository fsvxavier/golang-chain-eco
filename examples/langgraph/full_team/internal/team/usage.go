package team

import (
	"context"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

// usageAcc sums the token usage of model calls.
type usageAcc struct {
	mu sync.Mutex
	u  messages.Usage
}

func (a *usageAcc) add(u *messages.Usage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.u.InputTokens += u.InputTokens
	a.u.OutputTokens += u.OutputTokens
	a.u.CacheReadTokens += u.CacheReadTokens
	a.u.CacheCreationTokens += u.CacheCreationTokens
	if u.TotalTokens > 0 {
		a.u.TotalTokens += u.TotalTokens
	} else {
		a.u.TotalTokens += u.InputTokens + u.OutputTokens
	}
}

// take returns the sum so far and starts over.
func (a *usageAcc) take() usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.u
	a.u = messages.Usage{}
	return usage(u)
}

// usage is a messages.Usage that knows when it is empty.
type usage messages.Usage

// ptr is nil for an empty usage, so a job that used no tokens reports none.
func (u usage) ptr() *messages.Usage {
	if u.TotalTokens == 0 {
		return nil
	}
	m := messages.Usage(u)
	return &m
}

type usageSinkKey struct{}

// usageMeter adds the usage of every model response to fixed (when set) and to
// the sink carried in the call's context (when there is one).
type usageMeter struct{ fixed *usageAcc }

func (usageMeter) Name() string { return "usage_meter" }

func (m usageMeter) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	msg, err := next(ctx, req)
	if err != nil || msg.Usage == nil {
		return msg, err
	}
	if m.fixed != nil {
		m.fixed.add(msg.Usage)
	}
	if s, _ := ctx.Value(usageSinkKey{}).(*usageAcc); s != nil {
		s.add(msg.Usage)
	}
	return msg, nil
}

// activityMeter reports tool calls as the worker's activity; it goes back to
// "model" when the tool returns.
type activityMeter struct{ set func(string) }

func (activityMeter) Name() string { return "activity_meter" }

func (m activityMeter) WrapToolCall(ctx context.Context, c messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	m.set("tool: " + c.Name)
	defer m.set("model")
	return next(ctx, c)
}
