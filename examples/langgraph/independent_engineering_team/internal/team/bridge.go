package team

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/fsvxavier/golang-chain-eco/agentprotocol"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/mcp"
	"github.com/fsvxavier/golang-chain-eco/mcp/server"
	"github.com/fsvxavier/golang-chain-eco/remote"
)

// Bridge lets MCP chats (Copilot, Claude Code, Kilo, Cursor, Opencode) drive
// the team: it talks Agent Server API to the manager, HTTP to the hub, Agent
// Protocol to a worker and MCP to the team memory.
type Bridge struct {
	Manager *remote.Client
	Hub     *HubClient
	HubURL  string
}

// NewBridge reads MANAGER_URL, MANAGER_API_KEY and HUB_URL.
func NewBridge() *Bridge {
	m := remote.NewClient(Env("MANAGER_URL", "http://localhost:8080"))
	m.APIKey = os.Getenv("MANAGER_API_KEY")
	hub := Env("HUB_URL", "http://localhost:8000")
	return &Bridge{Manager: m, Hub: NewHubClient(hub), HubURL: hub}
}

type (
	submitArgs struct {
		Request string `json:"request" jsonschema:"what the team must deliver"`
	}
	threadArgs struct {
		Thread string `json:"thread_id" jsonschema:"thread_id returned by team_submit"`
	}
	askArgs struct {
		Worker   string `json:"worker" jsonschema:"worker id from team_queue"`
		Question string `json:"question"`
	}
	memArgs struct {
		Scope string `json:"scope" jsonschema:"the request's thread_id"`
		Query string `json:"query"`
	}
)

// Tools returns the bridge tools.
func (b *Bridge) Tools() []tools.Tool {
	return []tools.Tool{
		tools.New("team_submit", "Send a demand to the engineering team. Returns at once with thread_id and run_id; follow it with team_status.",
			func(ctx context.Context, a submitArgs) (any, error) {
				tid, err := b.Manager.CreateThread(ctx, map[string]any{"request": a.Request})
				if err != nil {
					return nil, tools.Errorf("team_submit: %v", err)
				}
				run, err := b.Manager.CreateRun(ctx, tid, remote.RunRequest{AssistantID: "engineering_team", Input: map[string]any{"request": a.Request}})
				if err != nil {
					return nil, tools.Errorf("team_submit: %v", err)
				}
				return fmt.Sprintf("thread_id=%s run_id=%s status=%s", tid, run.RunID, run.Status), nil
			}),
		tools.New("team_status", "Show the delivery state of a request: task statuses (received ... completed), report and handoff history.",
			func(ctx context.Context, a threadArgs) (any, error) {
				s, err := b.Manager.GetState(ctx, a.Thread)
				if err != nil {
					return nil, tools.Errorf("team_status: %v", err)
				}
				out, _ := json.MarshalIndent(map[string]any{
					"status": s.Values["status"], "report": s.Values["report"],
					"history": s.Values["history"], "running": s.Next,
				}, "", "  ")
				return string(out), nil
			}),
		tools.New("team_queue", "List the registered workers (role, current job) and the queued jobs.",
			func(ctx context.Context, _ struct{}) (any, error) {
				s, err := b.Hub.State(ctx)
				if err != nil {
					return nil, tools.Errorf("team_queue: %v", err)
				}
				out, _ := json.MarshalIndent(s, "", "  ")
				return string(out), nil
			}),
		tools.New("team_ask_worker", "Ask one worker directly over Agent Protocol, outside the delivery flow.",
			func(ctx context.Context, a askArgs) (any, error) {
				s, err := b.Hub.State(ctx)
				if err != nil {
					return nil, tools.Errorf("team_ask_worker: %v", err)
				}
				for _, w := range s.Workers {
					if w.ID != a.Worker {
						continue
					}
					res, err := agentprotocol.NewClient(w.URL, "").CreateAndWait(ctx, agentprotocol.RunCreate{
						AgentID: w.Role, IfNotExists: "create",
						Messages: []agentprotocol.Message{{Role: "user", Content: a.Question}},
					})
					if err != nil {
						return nil, tools.Errorf("team_ask_worker: %v", err)
					}
					if len(res.Messages) == 0 {
						return nil, tools.Errorf("team_ask_worker: %s returned no messages", w.ID)
					}
					return fmt.Sprint(res.Messages[len(res.Messages)-1].Content), nil
				}
				return nil, tools.Errorf("team_ask_worker: unknown worker %q (see team_queue)", a.Worker)
			}),
		tools.New("team_memory", "Recall what the team recorded for a request (the hub's memory_search).",
			func(ctx context.Context, a memArgs) (any, error) {
				c, err := mcp.Connect(ctx, mcp.HTTP(b.HubURL+"/mcp", nil))
				if err != nil {
					return nil, tools.Errorf("team_memory: %v", err)
				}
				defer c.Close()
				return c.CallTool(ctx, "memory_search", map[string]any{"scope": a.Scope, "query": a.Query})
			}),
	}
}

// MCPServer serves the bridge tools; run it with ServeStdio.
func (b *Bridge) MCPServer() *server.Server {
	return server.New("engineering-team", "1.0").AddTool(b.Tools()...)
}

