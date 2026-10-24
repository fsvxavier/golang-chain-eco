// human_in_the_loop: the Agent Streaming Protocol. A client subscribes to a
// thread's channels (lifecycle, messages, tools, input), starts a run with
// run.start, receives the model's tokens and tool activity as ordered events,
// gets an input.requested when the agent asks for approval, answers it with
// input.respond, and then inspects checkpoints with state.listCheckpoints.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agentprotocol"
	"github.com/fsvxavier/golang-chain-eco/agents"
	amw "github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type payArgs struct {
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pay := tools.New("pay", "Send a payment.", func(_ context.Context, a payArgs) (any, error) {
		return fmt.Sprintf("paid %.2f to %s", a.Amount, a.To), nil
	})
	calls := 0
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		if last, _ := messages.Last(msgs); last.Role == messages.RoleTool {
			return messages.AI("Done: " + last.Text() + "."), nil
		}
		calls++
		return messages.AI("I will pay the invoice now.", messages.ToolCall{ID: fmt.Sprintf("pay_%d", calls), Name: "pay", Args: map[string]any{"to": "ACME Corp", "amount": 120.5}}), nil
	}}
	// the agent must ask a human before calling "pay"
	agent, err := agents.Create(agents.Options{Model: model, Tools: []tools.Tool{pay},
		Middleware: []agents.Middleware{amw.HumanInTheLoop{Tools: map[string]amw.ToolPolicy{"pay": {}}}, amw.Compaction()},
		Compile:    []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}})
	if err != nil {
		log.Fatal(err)
	}
	srv, _ := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "payer", Graph: agent}}})
	ts := httptest.NewServer(srv)
	defer ts.Close()
	c := agentprotocol.NewClient(ts.URL, "")
	const thread = "invoice-42"

	show := func(e agentprotocol.Event) string {
		d, _ := e.Params["data"].(map[string]any)
		switch e.Method {
		case "messages":
			if d["event"] == "content-block-delta" {
				dl, _ := d["delta"].(map[string]any)
				return fmt.Sprintf("messages  delta %q", dl["text"])
			}
			return fmt.Sprintf("messages  %v", d["event"])
		case "tools":
			if d["toolName"] != nil {
				return fmt.Sprintf("tools     %v %v", d["event"], d["toolName"])
			}
			return fmt.Sprintf("tools     %v call=%v", d["event"], d["toolCallId"])
		case "input.requested":
			p, _ := json.Marshal(d["payload"])
			return fmt.Sprintf("input     requested id=%v payload=%s", d["interruptId"], p)
		case "lifecycle":
			return fmt.Sprintf("lifecycle %v", d["event"])
		}
		return e.Method
	}
	// open a stream and run until the lifecycle reaches a stopping status
	watch := func(title string, start func()) (interruptID string) {
		demo.Title(title)
		go func() { time.Sleep(150 * time.Millisecond); start() }()
		for ev, err := range c.Events(ctx, thread, agentprotocol.Filter{Channels: []string{"lifecycle", "messages", "tools", "input"}}) {
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("  seq=%-3d %s\n", ev.Seq, show(ev))
			d, _ := ev.Params["data"].(map[string]any)
			if ev.Method == "input.requested" {
				interruptID, _ = d["interruptId"].(string)
			}
			if ev.Method == "lifecycle" && (d["event"] == "interrupted" || d["event"] == "completed" || d["event"] == "failed") {
				return interruptID
			}
		}
		return ""
	}

	id := watch("run.start: the agent proposes a payment and pauses for approval", func() {
		_, err := c.Command(ctx, thread, "run.start", map[string]any{"assistantId": "payer",
			"input": map[string]any{"messages": []map[string]any{{"role": "user", "content": "Pay invoice 42 (120.50) to ACME Corp"}}}})
		if err != nil {
			log.Fatal(err)
		}
	})

	t, _ := c.GetThread(ctx, thread)
	fmt.Printf("\nthread status while waiting: %q\n", t.Status)

	watch("input.respond: a human approves", func() {
		res, err := c.Command(ctx, thread, "input.respond", map[string]any{"namespace": []string{}, "interruptId": id,
			"response": []map[string]any{{"type": "approve"}}})
		if err != nil {
			log.Fatal(err, res)
		}
	})

	demo.Title("state.get and state.listCheckpoints")
	sg, _ := c.Command(ctx, thread, "state.get", map[string]any{"namespace": []string{}, "keys": []string{"messages"}})
	var got struct {
		Values struct{ Messages []agentprotocol.Message }
	}
	_ = json.Unmarshal(sg.Result, &got)
	for _, m := range got.Values.Messages {
		fmt.Printf("  %-6s %s\n", m.Role, strings.TrimSpace(fmt.Sprint(m.Content)))
	}
	lc, _ := c.Command(ctx, thread, "state.listCheckpoints", map[string]any{"limit": 3})
	fmt.Println("  latest checkpoints:", string(lc.Result)[:120], "…")
	tree, _ := c.Command(ctx, thread, "agent.getTree", map[string]any{})
	fmt.Println("  agent tree:", string(tree.Result))
}
