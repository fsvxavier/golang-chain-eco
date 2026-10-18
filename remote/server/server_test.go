package server_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/deepagents/middleware"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/remote"
	"github.com/fsvxavier/golang-chain-eco/remote/server"
)

func setup(t *testing.T) (*remote.Client, *server.Server) {
	slow := &models.Fake{Func: func([]messages.Message, models.Options) (messages.Message, error) {
		time.Sleep(30 * time.Millisecond)
		return messages.AI("remote answer"), nil
	}}
	agent, err := agents.Create(agents.Options{Model: slow, Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}})
	if err != nil {
		t.Fatal(err)
	}
	ask, _ := graph.NewStateGraph(graph.Schema{"answer": channels.LastValue()}).
		AddNode("ask", func(ctx context.Context, _ graph.State) (any, error) {
			return graph.State{"answer": graph.Interrupt(ctx, "name?")}, nil
		}).AddEdge(graph.Start, "ask").AddEdge("ask", graph.End).Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
	srv := server.New(map[string]*graph.Compiled{"agent": agent, "ask": ask})
	srv.APIKey = "secret"
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	c := remote.NewClient(ts.URL)
	c.APIKey = "secret"
	return c, srv
}

func TestRoundTripWithRemoteGraph(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	bad := remote.NewClient(c.BaseURL)
	bad.APIKey = "wrong"
	if _, err := bad.CreateThread(ctx, nil); err == nil {
		t.Fatal("wrong key must be rejected")
	}

	rg := &remote.Graph{Client: c, AssistantID: "agent"}
	tid, _ := c.CreateThread(ctx, nil)
	out, err := rg.Invoke(runnables.WithConfig(ctx, graph.Thread(tid)), graph.State{"messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	ms := out["messages"].([]any)
	if last := ms[len(ms)-1].(map[string]any); last["content"] != "remote answer" {
		t.Fatalf("%v", out)
	}
	st, err := c.GetState(ctx, tid)
	if err != nil || len(st.Values["messages"].([]any)) != 2 {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestInterruptResumeOverHTTP(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	rg := &remote.Graph{Client: c, AssistantID: "ask"}
	tid, _ := c.CreateThread(ctx, nil)
	tctx := runnables.WithConfig(ctx, graph.Thread(tid))
	out, err := rg.Invoke(tctx, graph.State{})
	if err != nil || len(graph.Interrupts(out)) != 1 || graph.Interrupts(out)[0].Value != "name?" {
		t.Fatalf("%v %v", out, err)
	}
	out, err = rg.Invoke(tctx, graph.Command{Resume: "Ana"})
	if err != nil || out["answer"] != "Ana" {
		t.Fatalf("%v %v", out, err)
	}
}

func TestAsyncSubagentOverRemoteRunner(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	r := middleware.RemoteRunner{Client: c, AssistantID: "agent"}
	h, err := r.Start(ctx, "do work")
	if err != nil {
		t.Fatal(err)
	}
	var status middleware.AsyncStatus
	var result string
	for i := 0; i < 200; i++ {
		status, result, err = r.Poll(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if status != middleware.AsyncRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status != middleware.AsyncSuccess || result != "remote answer" {
		t.Fatalf("%s %q", status, result)
	}
	// cancel a fresh run
	h2, _ := r.Start(ctx, "another")
	if err := r.Cancel(ctx, h2); err != nil {
		t.Fatal(err)
	}
}
