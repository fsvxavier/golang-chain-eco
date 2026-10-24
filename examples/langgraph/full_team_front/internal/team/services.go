package team

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/tasktrack"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/remote"
	"github.com/fsvxavier/golang-chain-eco/remote/server"
)

func serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// RunHub serves the registry, the queue and the knowledge/memory MCP server.
// HUB_LEASE_TTL (default 15s) is how long a silent worker keeps its place.
func RunHub(ctx context.Context, ln net.Listener) error {
	ttl := 15 * time.Second
	if v := os.Getenv("HUB_LEASE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("HUB_LEASE_TTL: %w", err)
		}
		ttl = d
	}
	chat, label, err := KnowledgeModel()
	if err != nil {
		return err
	}
	emb, err := Embedder()
	if err != nil {
		return err
	}
	mcpSrv, err := NewMCP(ctx, chat, emb)
	if err != nil {
		return err
	}
	hub := NewHub(ttl)
	go hub.Run(ctx)
	log.Printf("hub: up on %s (lease %s, knowledge model %s)", ln.Addr(), ttl, label)
	return serve(ctx, ln, hub.Handler(mcpSrv.Handler()))
}

// RunManager serves the manager's delivery graph as an Agent Server
// (remote/server, the LangGraph Platform subset). Clients call it with
// remote.Graph.
func RunManager(ctx context.Context, hubURL string, ln net.Listener) error {
	chat, label, err := NewModel("manager", offline)
	if err != nil {
		return err
	}
	track, err := tasktrack.FromEnv(os.Stdout)
	if err != nil {
		return err
	}
	track.OnError = func(err error) { log.Print(err) }
	app, err := NewManager(NewHubClient(hubURL), chat, track)
	if err != nil {
		return err
	}
	srv := server.New(map[string]*graph.Compiled{"engineering_team": app})
	srv.APIKey = os.Getenv("MANAGER_API_KEY")
	log.Printf("manager: up on %s (model %s, hub %s)", ln.Addr(), label, hubURL)
	return serve(ctx, ln, srv)
}

// Env returns the environment variable k, or def when it is empty.
func Env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Invoke sends a request to the manager service with remote.Graph and
// returns its final state. thread is the request id: it keys the manager's
// checkpoint and the team memory.
func Invoke(ctx context.Context, managerURL, thread, request string) (map[string]any, error) {
	c := remote.NewClient(managerURL)
	c.APIKey = os.Getenv("MANAGER_API_KEY")
	tid, err := c.CreateThread(ctx, map[string]any{"request": thread})
	if err != nil {
		return nil, err
	}
	g := &remote.Graph{Client: c, AssistantID: "engineering_team"}
	return g.Invoke(runnables.WithConfig(ctx, graph.Thread(tid)), graph.State{"request": request})
}

// DefaultRequest is the sample request the demo and the client send.
const DefaultRequest = "Let shoppers apply a discount coupon at checkout. The last coupon must never be consumed twice, even when two payments race or a payment fails."

// Print writes the manager's report and the handoff history.
func Print(out map[string]any) {
	fmt.Println("\n== report ==")
	fmt.Println(out["report"])
	fmt.Println("\n== final status ==")
	st, _ := out["status"].(map[string]any)
	ids := make([]string, 0, len(st))
	for id := range st {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		name := id
		if id == "" {
			name = "request"
		}
		fmt.Printf("%-8s %v\n", name, st[id])
	}
	fmt.Println("\n== handoff history ==")
	hist, _ := out["history"].([]any)
	for i, h := range hist {
		fmt.Printf("%2d. %v\n", i+1, h)
	}
}
