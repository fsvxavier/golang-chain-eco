package team

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// The meter must see the usage both through its fixed accumulator (workers) and
// through the sink in the context (manager): the context has to survive the
// agent's graph.
func TestUsageMeter(t *testing.T) {
	fake := &models.Fake{Name: "u", Func: func([]messages.Message, models.Options) (messages.Message, error) {
		m := messages.AI("ok")
		m.Usage = &messages.Usage{InputTokens: 10, OutputTokens: 5}
		return m, nil
	}}
	fixed, sink := &usageAcc{}, &usageAcc{}
	agent, err := agents.Create(agents.Options{Model: fake, Middleware: []agents.Middleware{usageMeter{fixed: fixed}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), usageSinkKey{}, sink)
	if _, err := agent.Invoke(ctx, graph.State{"messages": []messages.Message{messages.Human("hi")}}); err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]*usageAcc{"fixed": fixed, "sink": sink} {
		if u := a.take(); u.InputTokens != 10 || u.OutputTokens != 5 || u.TotalTokens != 15 {
			t.Fatalf("%s: %+v", name, u)
		}
	}
	if fixed.take().ptr() != nil {
		t.Fatal("empty usage must be nil")
	}
}
