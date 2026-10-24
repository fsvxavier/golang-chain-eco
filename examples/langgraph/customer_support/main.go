// customer_support: an assistant with read-only tools that run freely and
// sensitive tools (change a booking) that need explicit user approval
// (port of customer-support/customer-support.ipynb).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type bookingArgs struct {
	BookingID string `json:"booking_id"`
}

type changeArgs struct {
	BookingID string `json:"booking_id"`
	NewDate   string `json:"new_date"`
}

func main() {
	db := map[string]string{"B-100": "2025-03-10"}
	lookup := tools.New("lookup_booking", "Read a booking.", func(_ context.Context, a bookingArgs) (any, error) {
		return fmt.Sprintf("booking %s on %s", a.BookingID, db[a.BookingID]), nil
	})
	change := tools.New("change_booking", "Change the date of a booking (needs approval).", func(_ context.Context, a changeArgs) (any, error) {
		db[a.BookingID] = a.NewDate
		return "changed to " + a.NewDate, nil
	})
	model := demo.Smart(func(msgs []messages.Message) messages.Message {
		switch last := msgs[len(msgs)-1]; {
		case last.Role == messages.RoleHuman:
			return messages.AI("", messages.ToolCall{Name: "lookup_booking", Args: map[string]any{"booking_id": "B-100"}},
				messages.ToolCall{Name: "change_booking", Args: map[string]any{"booking_id": "B-100", "new_date": "2025-04-01"}})
		case last.Status == "error":
			return messages.AI("Understood, I left your booking unchanged.")
		}
		return messages.AI("All done: your booking now reads " + db["B-100"] + ".")
	})
	agent, err := agents.Create(agents.Options{
		Model: model, Tools: []tools.Tool{lookup, change},
		SystemPrompt: "You are a customer support assistant for an airline.",
		Middleware: []agents.Middleware{middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{
			"change_booking": {Allowed: []string{middleware.Approve, middleware.Reject}},
		}}, middleware.Compaction()},
		Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())},
	})
	if err != nil {
		log.Fatal(err)
	}
	run := func(thread string, d middleware.HITLDecision) {
		ctx := runnables.WithConfig(context.Background(), graph.Thread(thread))
		out, err := agent.Invoke(ctx, graph.State{"messages": "Move my booking B-100 to April 1st."})
		if err != nil {
			log.Fatal(err)
		}
		req := graph.Interrupts(out)[0].Value.(middleware.HITLRequest)
		fmt.Printf("[%s] approval needed for: %s %v\n", thread, req.ActionRequests[0].Name, req.ActionRequests[0].Args)
		out, err = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: []middleware.HITLDecision{d}}})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("[%s] decision=%s -> %s (db=%s)\n", thread, d.Type, demo.Last(out), db["B-100"])
	}
	run("user-approves", middleware.HITLDecision{Type: middleware.Approve})
	db["B-100"] = "2025-03-10"
	run("user-rejects", middleware.HITLDecision{Type: middleware.Reject, Message: "Customer declined."})
}
