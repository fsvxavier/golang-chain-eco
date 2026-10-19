// serve_and_call: serve a LangGraph-style agent over the Agent Protocol and
// drive it with the protocol client: agent discovery, threads, runs (wait,
// background, SSE stream), state history and time travel, and the long-term
// store. Everything runs in-process against httptest.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http/httptest"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agentprotocol"
	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type cityArgs struct {
	City string `json:"city"`
}

func main() {
	ctx := context.Background()

	weather := tools.New("weather", "Current weather for a city.", func(_ context.Context, a cityArgs) (any, error) {
		return "sunny, 24C in " + a.City, nil
	})
	calls := 0
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		if last, _ := messages.Last(msgs); last.Role == messages.RoleTool {
			return messages.AI("It is " + last.Text() + "."), nil
		}
		if last, _ := messages.Last(msgs); strings.Contains(strings.ToLower(last.Text()), "thanks") {
			return messages.AI("You're welcome!"), nil
		}
		calls++ // tool call ids must be unique across the conversation
		return messages.AI("Let me check.", messages.ToolCall{ID: fmt.Sprintf("call_%d", calls), Name: "weather", Args: map[string]any{"city": "Lisbon"}}), nil
	}}
	agent, err := agents.Create(agents.Options{Model: model, Tools: []tools.Tool{weather},
		Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}})
	if err != nil {
		log.Fatal(err)
	}

	srv, err := agentprotocol.New(agentprotocol.Options{
		Agents: []agentprotocol.AgentSpec{{ID: "weather-bot", Name: "Weather Bot", Description: "Answers weather questions", Metadata: map[string]any{"team": "demo"}, Graph: agent}},
	})
	if err != nil {
		log.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	c := agentprotocol.NewClient(ts.URL, "")
	fmt.Println("serving on", ts.URL)

	demo.Title("agents: introspection")
	found, _ := c.SearchAgents(ctx, agentprotocol.SearchAgentsRequest{Name: "weather"})
	fmt.Printf("%d agent(s): %s (%s) capabilities=%+v\n", len(found), found[0].AgentID, found[0].Description, found[0].Capabilities)
	schemas, _ := c.GetAgentSchemas(ctx, "weather-bot")
	b, _ := json.Marshal(schemas.InputSchema)
	fmt.Println("input schema:", string(b)[:80], "…")

	demo.Title("threads + runs: a multi-turn conversation")
	th, _ := c.CreateThread(ctx, agentprotocol.ThreadCreate{Metadata: map[string]any{"user": "ana"}})
	fmt.Println("thread", th.ThreadID[:8]+"…", "status", th.Status)
	for _, q := range []string{"What's the weather in Lisbon?", "Thanks!"} {
		out, err := c.CreateAndWait(ctx, agentprotocol.RunCreate{ThreadID: th.ThreadID, AgentID: "weather-bot", Messages: []agentprotocol.Message{{Role: "user", Content: q}}})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Q: %-32s -> run %s: %s\n", q, out.Run.Status, out.Messages[len(out.Messages)-1].Content)
	}
	state, _ := c.GetThread(ctx, th.ThreadID)
	fmt.Println("messages in thread state:", len(state.Messages))

	demo.Title("history and time travel")
	hist, _ := c.ThreadHistory(ctx, th.ThreadID, 50, "")
	fmt.Printf("%d checkpoints (newest first)\n", len(hist))
	early := hist[len(hist)-3].Checkpoint.CheckpointID
	forked, _ := c.PatchThread(ctx, th.ThreadID, agentprotocol.ThreadPatch{
		Checkpoint: &agentprotocol.ThreadCheckpoint{CheckpointID: early},
		Messages:   []agentprotocol.Message{{Role: "user", Content: "Actually, what about Porto?"}}})
	fmt.Printf("patched from checkpoint %s…: thread now has %d message(s); last: %q\n", early[:8], len(forked.Messages), forked.Messages[len(forked.Messages)-1].Content)
	cp, _ := c.CopyThread(ctx, th.ThreadID)
	fmt.Println("copy is independent:", cp.ThreadID != th.ThreadID, "messages:", len(cp.Messages))

	demo.Title("streaming a run (SSE)")
	var kinds []string
	for ev, err := range c.StreamRun(ctx, agentprotocol.RunCreate{AgentID: "weather-bot", Input: json.RawMessage(`"weather in Lisbon?"`),
		StreamMode: agentprotocol.StreamModes{"updates", "values"}}) {
		if err != nil {
			log.Fatal(err)
		}
		kinds = append(kinds, ev.Event)
	}
	fmt.Println("events:", strings.Join(kinds, " → "))
	fmt.Println("(stateless run: its ephemeral thread is deleted when it ends)")

	demo.Title("long-term store")
	_ = c.PutItem(ctx, []string{"users", "ana"}, "prefs", map[string]any{"units": "metric"})
	_ = c.PutItem(ctx, []string{"users", "bob"}, "prefs", map[string]any{"units": "imperial"})
	items, _ := c.SearchItems(ctx, agentprotocol.StoreSearchRequest{NamespacePrefix: []string{"users"}, Filter: map[string]any{"units": "imperial"}})
	fmt.Printf("search found %d item(s): %v\n", len(items), items[0].Namespace)
	ns, _ := c.ListNamespaces(ctx, agentprotocol.StoreListNamespacesRequest{Prefix: []string{"users"}})
	fmt.Println("namespaces:", ns)
}
