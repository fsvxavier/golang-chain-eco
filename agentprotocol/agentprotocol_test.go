package agentprotocol_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agentprotocol"
	"github.com/fsvxavier/golang-chain-eco/agents"
	amw "github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type cityArgs struct {
	City string `json:"city"`
}

// chatAgent: model calls the weather tool once, then answers.
func chatAgent(t *testing.T) *graph.Compiled {
	t.Helper()
	weather := tools.New("weather", "weather", func(_ context.Context, a cityArgs) (any, error) { return "sunny in " + a.City, nil })
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		if last, _ := messages.Last(msgs); last.Role == messages.RoleTool {
			return messages.AI("It is " + last.Text()), nil
		}
		return messages.AI("Checking", messages.ToolCall{ID: "call_1", Name: "weather", Args: map[string]any{"city": "Rome"}}), nil
	}}
	g, err := agents.Create(agents.Options{Model: model, Tools: []tools.Tool{weather},
		Compile: []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// approvalGraph pauses for human approval, then records the answer.
func approvalGraph(t *testing.T) *graph.Compiled {
	t.Helper()
	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages(), "decision": channels.LastValue()}).
		AddNode("ask", func(ctx context.Context, s graph.State) (any, error) {
			ans := graph.Interrupt(ctx, map[string]any{"question": "approve deploy?"})
			return graph.State{"decision": ans, "messages": []messages.Message{messages.AI(fmt.Sprint("decision: ", ans))}}, nil
		}).
		AddEdge(graph.Start, "ask").AddEdge("ask", graph.End)
	c, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// slowGraph blocks until released (for busy/cancel tests).
func slowGraph(t *testing.T, started chan<- struct{}, release <-chan struct{}) *graph.Compiled {
	t.Helper()
	g := graph.NewStateGraph(graph.Schema{"messages": graph.Messages()}).
		AddNode("work", func(ctx context.Context, s graph.State) (any, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return graph.State{"messages": []messages.Message{messages.AI("done")}}, nil
		}).
		AddEdge(graph.Start, "work").AddEdge("work", graph.End)
	c, err := g.Compile(graph.WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newServer(t *testing.T, opts agentprotocol.Options) (*agentprotocol.Client, *httptest.Server) {
	t.Helper()
	srv, err := agentprotocol.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return agentprotocol.NewClient(ts.URL, opts.APIKey), ts
}

func chatServer(t *testing.T) (*agentprotocol.Client, *httptest.Server) {
	return newServer(t, agentprotocol.Options{Agents: []agentprotocol.AgentSpec{
		{ID: "chat", Name: "Chat Agent", Description: "weather bot", Metadata: map[string]any{"team": "ml"}, Graph: chatAgent(t)},
		{ID: "approval", Graph: approvalGraph(t)},
	}})
}

func statusOf(err error) int {
	var se *agentprotocol.StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

func TestAgentsIntrospection(t *testing.T) {
	ctx := context.Background()
	c, _ := chatServer(t)
	all, err := c.SearchAgents(ctx, agentprotocol.SearchAgentsRequest{})
	if err != nil || len(all) != 2 || all[0].AgentID != "chat" || !all[0].Capabilities.Messages || !all[0].Capabilities.Streaming {
		t.Fatalf("%+v %v", all, err)
	}
	byMeta, _ := c.SearchAgents(ctx, agentprotocol.SearchAgentsRequest{Metadata: map[string]any{"team": "ml"}})
	byName, _ := c.SearchAgents(ctx, agentprotocol.SearchAgentsRequest{Name: "chat"})
	if len(byMeta) != 1 || len(byName) != 1 {
		t.Fatalf("%v %v", byMeta, byName)
	}
	a, err := c.GetAgent(ctx, "chat")
	if err != nil || a.Description != "weather bot" {
		t.Fatalf("%+v %v", a, err)
	}
	sc, err := c.GetAgentSchemas(ctx, "chat")
	if err != nil || sc.InputSchema["type"] != "object" || sc.OutputSchema == nil {
		t.Fatalf("%+v %v", sc, err)
	}
	if _, err := c.GetAgent(ctx, "ghost"); statusOf(err) != 404 {
		t.Fatalf("%v", err)
	}
}

func TestThreadsLifecycle(t *testing.T) {
	ctx := context.Background()
	c, _ := chatServer(t)
	th, err := c.CreateThread(ctx, agentprotocol.ThreadCreate{ThreadID: "t1", Metadata: map[string]any{"user": "ana", "agent_id": "chat"}})
	if err != nil || th.ThreadID != "t1" || th.Status != "idle" {
		t.Fatalf("%+v %v", th, err)
	}
	if _, err := c.CreateThread(ctx, agentprotocol.ThreadCreate{ThreadID: "t1"}); statusOf(err) != 409 {
		t.Fatalf("duplicate must be 409: %v", err)
	}
	if again, err := c.CreateThread(ctx, agentprotocol.ThreadCreate{ThreadID: "t1", IfExists: "do_nothing"}); err != nil || again.ThreadID != "t1" {
		t.Fatalf("%v", err)
	}
	// run a turn, then inspect state through the thread resources
	if _, err := c.CreateAndWait(ctx, agentprotocol.RunCreate{ThreadID: "t1", Messages: []agentprotocol.Message{{Role: "user", Content: "weather in Rome?"}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := c.GetThread(ctx, "t1")
	if len(got.Messages) != 4 || got.Messages[3].Content != "It is sunny in Rome" || got.Messages[0].Role != "human" {
		t.Fatalf("%+v", got.Messages)
	}
	hist, err := c.ThreadHistory(ctx, "t1", 100, "")
	if err != nil || len(hist) < 3 || hist[0].Checkpoint.CheckpointID == "" || len(hist[0].Messages) != 4 {
		t.Fatalf("%d %v", len(hist), err)
	}
	older, _ := c.ThreadHistory(ctx, "t1", 100, hist[0].Checkpoint.CheckpointID)
	if len(older) != len(hist)-1 {
		t.Fatalf("before= must page backwards: %d vs %d", len(older), len(hist))
	}

	// patch: metadata merges, messages append as a new revision
	p, err := c.PatchThread(ctx, "t1", agentprotocol.ThreadPatch{Metadata: map[string]any{"tag": "x"}, Messages: []agentprotocol.Message{{Role: "user", Content: "thanks"}}})
	if err != nil || p.Metadata["tag"] != "x" || p.Metadata["user"] != "ana" || len(p.Messages) != 5 {
		t.Fatalf("%+v %v", p, err)
	}
	// time travel: patching from an older checkpoint forks the state
	forked, err := c.PatchThread(ctx, "t1", agentprotocol.ThreadPatch{Checkpoint: &agentprotocol.ThreadCheckpoint{CheckpointID: hist[len(hist)-1].Checkpoint.CheckpointID}, Messages: []agentprotocol.Message{{Role: "user", Content: "other path"}}})
	if err != nil || len(forked.Messages) != 2 || forked.Messages[1].Content != "other path" {
		t.Fatalf("%+v %v", forked.Messages, err)
	}

	// copy is independent
	cp, err := c.CopyThread(ctx, "t1")
	if err != nil || cp.ThreadID == "t1" || len(cp.Messages) != 2 {
		t.Fatalf("%+v %v", cp, err)
	}
	if _, err := c.PatchThread(ctx, cp.ThreadID, agentprotocol.ThreadPatch{Messages: []agentprotocol.Message{{Role: "user", Content: "only in copy"}}}); err != nil {
		t.Fatal(err)
	}
	if orig, _ := c.GetThread(ctx, "t1"); len(orig.Messages) != 2 {
		t.Fatal("copy leaked into the original")
	}

	// search by metadata and by state values
	found, _ := c.SearchThreads(ctx, agentprotocol.ThreadSearchRequest{Metadata: map[string]any{"user": "ana"}})
	if len(found) != 2 {
		t.Fatalf("%d", len(found))
	}
	if none, _ := c.SearchThreads(ctx, agentprotocol.ThreadSearchRequest{Status: "busy"}); len(none) != 0 {
		t.Fatal("no thread is busy")
	}

	if err := c.DeleteThread(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetThread(ctx, "t1"); statusOf(err) != 404 {
		t.Fatalf("deleted thread must be gone: %v", err)
	}
	if err := c.DeleteThread(ctx, "t1"); statusOf(err) != 404 {
		t.Fatalf("%v", err)
	}
}

func TestRunsWaitStatelessAndErrors(t *testing.T) {
	ctx := context.Background()
	c, _ := chatServer(t)
	// stateless run: ephemeral thread, nothing left behind
	out, err := c.CreateAndWait(ctx, agentprotocol.RunCreate{AgentID: "chat", Input: json.RawMessage(`"weather?"`)})
	if err != nil || out.Run.Status != "success" || out.Messages[len(out.Messages)-1].Content != "It is sunny in Rome" {
		t.Fatalf("%+v %v", out, err)
	}
	if th, _ := c.SearchThreads(ctx, agentprotocol.ThreadSearchRequest{}); len(th) != 0 {
		t.Fatalf("ephemeral thread leaked: %d", len(th))
	}
	// several agents: agent_id required
	if _, err := c.CreateAndWait(ctx, agentprotocol.RunCreate{Input: json.RawMessage(`"x"`)}); statusOf(err) != 404 {
		t.Fatalf("%v", err)
	}
	// unknown thread: reject by default, create on request
	if _, err := c.CreateRun(ctx, agentprotocol.RunCreate{ThreadID: "nope", AgentID: "chat", Input: json.RawMessage(`"x"`)}); statusOf(err) != 404 {
		t.Fatalf("%v", err)
	}
	r, err := c.CreateRun(ctx, agentprotocol.RunCreate{ThreadID: "lazy", AgentID: "chat", IfNotExists: "create", Input: json.RawMessage(`"x"`),
		Metadata: map[string]any{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := c.WaitRun(ctx, r.RunID)
	if err != nil || w.Run.Status != "success" {
		t.Fatalf("%+v %v", w, err)
	}
	if g, _ := c.GetRun(ctx, r.RunID); g.Status != "success" || g.Metadata["k"] != "v" {
		t.Fatalf("%+v", g)
	}
	if runs, _ := c.SearchRuns(ctx, agentprotocol.RunSearchRequest{ThreadID: "lazy", Status: "success"}); len(runs) != 1 {
		t.Fatalf("%d", len(runs))
	}
	if err := c.DeleteRun(ctx, r.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRun(ctx, r.RunID); statusOf(err) != 404 {
		t.Fatal("deleted run must be 404")
	}
	if _, err := c.CreateRun(ctx, agentprotocol.RunCreate{ThreadID: "lazy", IfNotExists: "bogus"}); statusOf(err) != 422 {
		t.Fatalf("%v", err)
	}
}

func TestBusyThreadCancelAndWebhook(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	c, _ := newServer(t, agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "slow", Graph: slowGraph(t, started, release)}}})

	var hook sync.WaitGroup
	hook.Add(1)
	var hookBody map[string]any
	wh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&hookBody)
		hook.Done()
	}))
	defer wh.Close()

	r1, err := c.CreateRun(ctx, agentprotocol.RunCreate{ThreadID: "b", IfNotExists: "create", Input: json.RawMessage(`"go"`), Webhook: wh.URL})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if th, _ := c.GetThread(ctx, "b"); th.Status != "busy" {
		t.Fatalf("status %q", th.Status)
	}
	if _, err := c.CreateRun(ctx, agentprotocol.RunCreate{ThreadID: "b", Input: json.RawMessage(`"again"`)}); statusOf(err) != 409 {
		t.Fatalf("a busy thread must answer 409: %v", err)
	}
	if err := c.CancelRun(ctx, r1.RunID, "interrupt", true); err != nil {
		t.Fatal(err)
	}
	if g, _ := c.GetRun(ctx, r1.RunID); g.Status != "interrupted" {
		t.Fatalf("%+v", g)
	}
	if th, _ := c.GetThread(ctx, "b"); th.Status != "idle" {
		t.Fatalf("status after cancel: %q", th.Status)
	}
	hook.Wait()
	if run, _ := hookBody["run"].(map[string]any); run["status"] != "interrupted" {
		t.Fatalf("webhook body %v", hookBody)
	}
	// now it can run again, and finish
	close(release)
	r2, err := c.CreateRun(ctx, agentprotocol.RunCreate{ThreadID: "b", Input: json.RawMessage(`"again"`), OnCompletion: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := c.GetRun(ctx, r2.RunID); statusOf(err) != 404 {
		t.Fatalf("on_completion=delete must drop the run: %v", err)
	}
}

func TestRunStreamSSE(t *testing.T) {
	ctx := context.Background()
	c, _ := chatServer(t)
	var names []string
	var lastValues map[string]any
	for ev, err := range c.StreamRun(ctx, agentprotocol.RunCreate{AgentID: "chat", Input: json.RawMessage(`"weather?"`), StreamMode: agentprotocol.StreamModes{"values", "updates"}}) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, ev.Event)
		if ev.Event == "values" {
			_ = json.Unmarshal(ev.Data, &lastValues)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.HasPrefix(joined, "metadata,") || !strings.HasSuffix(joined, ",end") || !strings.Contains(joined, "updates") || strings.Contains(joined, "messages") {
		t.Fatalf("events: %s", joined)
	}
	if msgs, _ := lastValues["messages"].([]any); len(msgs) != 4 {
		t.Fatalf("%v", lastValues)
	}
}

func TestStoreAPI(t *testing.T) {
	ctx := context.Background()
	c, _ := chatServer(t)
	if err := c.PutItem(ctx, []string{"users", "ana"}, "prefs", map[string]any{"lang": "pt"}); err != nil {
		t.Fatal(err)
	}
	_ = c.PutItem(ctx, []string{"users", "bob"}, "prefs", map[string]any{"lang": "en"})
	it, err := c.GetItem(ctx, []string{"users", "ana"}, "prefs")
	if err != nil || it.Value["lang"] != "pt" || it.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", it, err)
	}
	items, _ := c.SearchItems(ctx, agentprotocol.StoreSearchRequest{NamespacePrefix: []string{"users"}, Filter: map[string]any{"lang": "en"}})
	if len(items) != 1 || items[0].Namespace[1] != "bob" {
		t.Fatalf("%+v", items)
	}
	nss, _ := c.ListNamespaces(ctx, agentprotocol.StoreListNamespacesRequest{Prefix: []string{"users"}})
	if len(nss) != 2 {
		t.Fatalf("%v", nss)
	}
	depth, _ := c.ListNamespaces(ctx, agentprotocol.StoreListNamespacesRequest{MaxDepth: 1})
	if len(depth) != 1 || depth[0][0] != "users" {
		t.Fatalf("%v", depth)
	}
	if err := c.DeleteItem(ctx, []string{"users", "ana"}, "prefs"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetItem(ctx, []string{"users", "ana"}, "prefs"); statusOf(err) != 404 {
		t.Fatalf("%v", err)
	}
	if err := c.DeleteItem(ctx, []string{"users", "ana"}, "prefs"); statusOf(err) != 404 {
		t.Fatalf("%v", err)
	}
	if err := c.PutItem(ctx, []string{"x"}, "", map[string]any{}); statusOf(err) != 422 {
		t.Fatalf("%v", err)
	}
}

func TestAPIKey(t *testing.T) {
	ctx := context.Background()
	c, ts := newServer(t, agentprotocol.Options{APIKey: "s3cret", Agents: []agentprotocol.AgentSpec{{ID: "chat", Graph: chatAgent(t)}}})
	if _, err := c.GetAgent(ctx, "chat"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	bad := agentprotocol.NewClient(ts.URL, "wrong")
	if _, err := bad.GetAgent(ctx, "chat"); statusOf(err) != 401 {
		t.Fatalf("%v", err)
	}
	none := agentprotocol.NewClient(ts.URL, "")
	if _, err := none.GetAgent(ctx, "chat"); statusOf(err) != 401 {
		t.Fatalf("%v", err)
	}
	if resp, err := http.Get(ts.URL + "/ok"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("/ok must stay open for health checks: %v", err)
	}
	// bearer works too
	req, _ := http.NewRequest("GET", ts.URL+"/agents/chat", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("bearer: %v", err)
	}
}

// ------------------------------------------------------------ streaming protocol

// collect reads events until a lifecycle event with one of the end statuses.
func collect(t *testing.T, ctx context.Context, c *agentprotocol.Client, thread string, f agentprotocol.Filter, trigger func()) []agentprotocol.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	go func() { time.Sleep(150 * time.Millisecond); once.Do(func() { close(ready) }) }()
	var out []agentprotocol.Event
	go func() { <-ready; trigger() }()
	for ev, err := range c.Events(ctx, thread, f) {
		if err != nil {
			t.Fatalf("stream: %v (got %d events)", err, len(out))
		}
		out = append(out, ev)
		if ev.Method == "lifecycle" {
			d, _ := ev.Params["data"].(map[string]any)
			if s, _ := d["event"].(string); s == "completed" || s == "failed" || s == "interrupted" {
				return out
			}
		}
	}
	return out
}

func dataOf(e agentprotocol.Event) map[string]any {
	d, _ := e.Params["data"].(map[string]any)
	return d
}

func TestStreamingProtocolMessagesToolsLifecycle(t *testing.T) {
	ctx := context.Background()
	c, _ := chatServer(t)
	evs := collect(t, ctx, c, "s1", agentprotocol.Filter{Channels: []string{"messages", "tools", "lifecycle", "values", "updates", "checkpoints"}}, func() {
		res, err := c.Command(ctx, "s1", "run.start", map[string]any{"assistantId": "chat", "input": map[string]any{"messages": []map[string]any{{"role": "user", "content": "weather in Rome?"}}}})
		if err != nil || res.Type != "success" {
			t.Errorf("run.start: %+v %v", res, err)
		}
	})
	var seqPrev uint64
	var methods []string
	for _, e := range evs {
		if e.Seq <= seqPrev || e.EventID == "" || e.Type != "event" {
			t.Fatalf("bad envelope %+v", e)
		}
		seqPrev = e.Seq
		methods = append(methods, e.Method)
	}
	if first := dataOf(evs[0]); first["event"] != "started" {
		t.Fatalf("first event %v", first)
	}
	// messages channel: every message-start is closed by message-finish, blocks never interleave
	open, blocks := 0, map[int]bool{}
	var toolCallBlockID, toolStartID string
	var sawText, sawFinish bool
	for _, e := range evs {
		d := dataOf(e)
		switch e.Method {
		case "messages":
			switch d["event"] {
			case "message-start":
				if open != 0 {
					t.Fatal("message-start inside an open message")
				}
				open++
			case "content-block-start":
				idx := int(d["index"].(float64))
				if blocks[idx] {
					t.Fatalf("block %d started twice", idx)
				}
				blocks[idx] = true
			case "content-block-delta":
				if dl, _ := d["delta"].(map[string]any); dl["type"] == "text-delta" {
					sawText = true
				}
			case "content-block-finish":
				if c, _ := d["content"].(map[string]any); c["type"] == "tool_call" {
					toolCallBlockID, _ = c["id"].(string)
				}
			case "message-finish":
				open--
				blocks = map[int]bool{}
				sawFinish = true
			}
		case "tools":
			if d["event"] == "tool-started" {
				toolStartID, _ = d["toolCallId"].(string)
				if d["toolName"] != "weather" {
					t.Fatalf("%v", d)
				}
			}
			if d["event"] == "tool-finished" && d["toolCallId"] != toolStartID {
				t.Fatalf("tool-finished %v does not match tool-started %v", d["toolCallId"], toolStartID)
			}
		}
	}
	if open != 0 || !sawText || !sawFinish {
		t.Fatalf("message lifecycle incomplete: open=%d text=%v finish=%v\n%v", open, sawText, sawFinish, methods)
	}
	if toolCallBlockID != "call_1" || toolStartID != toolCallBlockID {
		t.Fatalf("tool call id must link the content block (%q) to the tools channel (%q)", toolCallBlockID, toolStartID)
	}
	last := dataOf(evs[len(evs)-1])
	if last["event"] != "completed" {
		t.Fatalf("last %v", last)
	}
	has := map[string]bool{}
	for _, m := range methods {
		has[m] = true
	}
	for _, want := range []string{"values", "updates", "checkpoints", "messages", "tools", "lifecycle"} {
		if !has[want] {
			t.Errorf("channel %s produced nothing", want)
		}
	}

	// channel filtering: only lifecycle
	evs2 := collect(t, ctx, c, "s2", agentprotocol.Filter{Channels: []string{"lifecycle"}}, func() {
		_, _ = c.Command(ctx, "s2", "run.start", map[string]any{"assistantId": "chat", "input": "hi"})
	})
	for _, e := range evs2 {
		if e.Method != "lifecycle" {
			t.Fatalf("filter leaked %s", e.Method)
		}
	}

	// replay: a late subscriber with since=1 gets the buffered lifecycle tail (running, completed)
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var replayed []agentprotocol.Event
	for ev, err := range c.Events(rctx, "s1", agentprotocol.Filter{Channels: []string{"lifecycle"}, Since: 1}) {
		if err != nil {
			break
		}
		replayed = append(replayed, ev)
		if len(replayed) == 2 {
			break
		}
	}
	if len(replayed) != 2 || dataOf(replayed[len(replayed)-1])["event"] != "completed" {
		t.Fatalf("replay: %d events", len(replayed))
	}
}

func TestStreamingProtocolInterruptAndCommands(t *testing.T) {
	ctx := context.Background()
	c, _ := chatServer(t)
	evs := collect(t, ctx, c, "h1", agentprotocol.Filter{Channels: []string{"input", "lifecycle"}}, func() {
		_, _ = c.Command(ctx, "h1", "run.start", map[string]any{"assistantId": "approval", "input": map[string]any{"messages": []map[string]any{{"role": "user", "content": "deploy"}}}})
	})
	var interruptID string
	for _, e := range evs {
		if e.Method == "input.requested" {
			interruptID, _ = dataOf(e)["interruptId"].(string)
			if p, _ := dataOf(e)["payload"].(map[string]any); p["question"] != "approve deploy?" {
				t.Fatalf("%v", dataOf(e))
			}
		}
	}
	if interruptID == "" || dataOf(evs[len(evs)-1])["event"] != "interrupted" {
		t.Fatalf("expected input.requested + interrupted, got %d events", len(evs))
	}
	if th, _ := c.GetThread(ctx, "h1"); th.Status != "interrupted" {
		t.Fatalf("status %q", th.Status)
	}
	// a wrong interrupt id is rejected with the protocol error code
	if res, err := c.Command(ctx, "h1", "input.respond", map[string]any{"namespace": []string{}, "interruptId": "nope", "response": "yes"}); err == nil || res.Error != "no_such_interrupt" {
		t.Fatalf("%+v %v", res, err)
	}
	evs = collect(t, ctx, c, "h1", agentprotocol.Filter{Channels: []string{"lifecycle", "values"}}, func() {
		res, err := c.Command(ctx, "h1", "input.respond", map[string]any{"namespace": []string{}, "interruptId": interruptID, "response": "yes"})
		if err != nil || res.Type != "success" {
			t.Errorf("%+v %v", res, err)
		}
	})
	if dataOf(evs[len(evs)-1])["event"] != "completed" {
		t.Fatalf("resumed run did not complete: %v", dataOf(evs[len(evs)-1]))
	}

	// state.get / listCheckpoints / fork / getTree
	sg, err := c.Command(ctx, "h1", "state.get", map[string]any{"namespace": []string{}, "keys": []string{"decision"}})
	var sgr struct {
		Values     map[string]any
		Checkpoint map[string]any
	}
	_ = json.Unmarshal(sg.Result, &sgr)
	if err != nil || sgr.Values["decision"] != "yes" || sgr.Checkpoint["id"] == "" || len(sgr.Values) != 1 {
		t.Fatalf("%s %v", sg.Result, err)
	}
	lc, err := c.Command(ctx, "h1", "state.listCheckpoints", map[string]any{"limit": 50})
	var lcr struct {
		Checkpoints []struct {
			ID, Timestamp string
			Step          int
		}
	}
	_ = json.Unmarshal(lc.Result, &lcr)
	if err != nil || len(lcr.Checkpoints) < 2 || lcr.Checkpoints[0].Timestamp == "" {
		t.Fatalf("%s %v", lc.Result, err)
	}
	oldest := lcr.Checkpoints[len(lcr.Checkpoints)-1].ID
	fk, err := c.Command(ctx, "h1", "state.fork", map[string]any{"checkpointId": oldest})
	var fkr struct{ ThreadID, RunID string }
	_ = json.Unmarshal(fk.Result, &fkr)
	if err != nil || fkr.ThreadID == "" || fkr.ThreadID == "h1" {
		t.Fatalf("%s %v", fk.Result, err)
	}
	if _, err := c.Command(ctx, "h1", "state.fork", map[string]any{"checkpointId": "missing"}); err == nil {
		t.Fatal("unknown checkpoint must fail")
	}
	tree, _ := c.Command(ctx, "h1", "agent.getTree", map[string]any{})
	if !strings.Contains(string(tree.Result), `"status":"completed"`) {
		t.Fatalf("%s", tree.Result)
	}
	// error codes
	if res, err := c.Command(ctx, "h1", "no.such", nil); err == nil || res.Error != "unknown_command" {
		t.Fatalf("%+v %v", res, err)
	}
	if res, err := c.Command(ctx, "h1", "subscription.subscribe", map[string]any{"channels": []string{"values"}}); err == nil || res.Error != "not_supported" {
		t.Fatalf("SSE has connection-scoped subscriptions: %+v %v", res, err)
	}
	if res, err := c.Command(ctx, "h1", "run.start", map[string]any{"input": "x"}); err == nil || res.Error != "invalid_argument" {
		t.Fatalf("%+v %v", res, err)
	}
	// SSE requires at least one channel
	resp, _ := http.Post(c.BaseURL+"/threads/h1/stream", "application/json", strings.NewReader(`{"channels":[]}`))
	if resp.StatusCode != 422 {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestRunStartWhileActiveIsQueued(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	c, _ := newServer(t, agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "slow", Graph: slowGraph(t, started, release)}}})
	r, err := c.Command(ctx, "q", "run.start", map[string]any{"assistantId": "slow", "input": "one"})
	if err != nil || r.Type != "success" {
		t.Fatal(err)
	}
	<-started
	q, err := c.Command(ctx, "q", "run.start", map[string]any{"assistantId": "slow", "input": "two"})
	if err != nil || !strings.Contains(string(q.Result), `"queued":true`) {
		t.Fatalf("a run.start during an active run must be queued: %s %v", q.Result, err)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		runs, _ := c.SearchRuns(ctx, agentprotocol.RunSearchRequest{ThreadID: "q", Status: "success"})
		if len(runs) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued run never ran: %d finished", len(runs))
		}
		time.Sleep(20 * time.Millisecond)
	}
	th, _ := c.GetThread(ctx, "q")
	if len(th.Messages) != 4 { // two turns: human + ai each
		t.Fatalf("%+v", th.Messages)
	}
}

// ------------------------------------------------------------------ websocket

type wsClient struct {
	c  net.Conn
	br *bufio.Reader
}

func dialWS(t *testing.T, ts *httptest.Server, path string) *wsClient {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	keyRaw := make([]byte, 16)
	_, _ = rand.Read(keyRaw)
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, base64.StdEncoding.EncodeToString(keyRaw))
	br := bufio.NewReader(conn)
	status, _ := br.ReadString('\n')
	if !strings.Contains(status, "101") {
		t.Fatalf("handshake: %s", status)
	}
	for {
		l, _ := br.ReadString('\n')
		if l == "\r\n" || l == "" {
			break
		}
	}
	t.Cleanup(func() { conn.Close() })
	return &wsClient{conn, br}
}

func (w *wsClient) send(t *testing.T, v any, fragment bool) {
	t.Helper()
	b, _ := json.Marshal(v)
	mask := [4]byte{1, 2, 3, 4}
	frame := func(op byte, fin bool, payload []byte) {
		h := []byte{op}
		if fin {
			h[0] |= 0x80
		}
		switch n := len(payload); {
		case n < 126:
			h = append(h, 0x80|byte(n))
		default:
			h = append(h, 0x80|126, byte(n>>8), byte(n))
		}
		h = append(h, mask[:]...)
		p := make([]byte, len(payload))
		for i := range payload {
			p[i] = payload[i] ^ mask[i%4]
		}
		_, _ = w.c.Write(append(h, p...))
	}
	if fragment {
		frame(0x1, false, b[:5])
		frame(0x0, true, b[5:])
		return
	}
	frame(0x1, true, b)
}

func (w *wsClient) recv(t *testing.T) map[string]any {
	t.Helper()
	_ = w.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	b0, err := w.br.ReadByte()
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	b1, _ := w.br.ReadByte()
	n := int(b1 & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		_, _ = io.ReadFull(w.br, b[:])
		n = int(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		_, _ = io.ReadFull(w.br, b[:])
		n = int(binary.BigEndian.Uint64(b[:]))
	}
	data := make([]byte, n)
	_, _ = io.ReadFull(w.br, data)
	if b0&0x0F != 0x1 {
		t.Fatalf("unexpected opcode %x", b0&0x0F)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	return m
}

func TestWebSocketTransport(t *testing.T) {
	_, ts := chatServer(t)
	ws := dialWS(t, ts, "/threads/w1/stream")
	// fragmented command frame
	ws.send(t, map[string]any{"id": 1, "method": "subscription.subscribe", "params": map[string]any{"channels": []string{"lifecycle", "tools"}}}, true)
	resp := ws.recv(t)
	res, _ := resp["result"].(map[string]any)
	if resp["type"] != "success" || res["subscriptionId"] == nil {
		t.Fatalf("%v", resp)
	}
	subID := res["subscriptionId"].(string)
	ws.send(t, map[string]any{"id": 2, "method": "run.start", "params": map[string]any{"assistantId": "chat", "input": "weather?"}}, false)
	sawTool, completed, gotStart := false, false, false
	for !completed {
		m := ws.recv(t)
		switch m["type"] {
		case "success":
			gotStart = gotStart || m["id"] == float64(2)
		case "event":
			p := m["params"].(map[string]any)
			d := p["data"].(map[string]any)
			if m["method"] == "tools" && d["event"] == "tool-finished" {
				sawTool = true
			}
			if m["method"] == "lifecycle" && d["event"] == "completed" {
				completed = true
			}
		}
	}
	if !sawTool || !gotStart {
		t.Fatalf("tool=%v start=%v", sawTool, gotStart)
	}
	ws.send(t, map[string]any{"id": 3, "method": "subscription.unsubscribe", "params": map[string]any{"subscriptionId": subID}}, false)
	if r := ws.recv(t); r["type"] != "success" {
		t.Fatalf("%v", r)
	}
	ws.send(t, map[string]any{"id": 4, "method": "subscription.unsubscribe", "params": map[string]any{"subscriptionId": subID}}, false)
	if r := ws.recv(t); r["error"] != "no_such_subscription" {
		t.Fatalf("%v", r)
	}
	// reconnect restores a subscription and reports what it replays
	ws.send(t, map[string]any{"id": 5, "method": "subscription.subscribe", "params": map[string]any{"channels": []string{"lifecycle"}}}, false)
	sub2 := ws.recv(t)["result"].(map[string]any)["subscriptionId"].(string)
	ws.send(t, map[string]any{"id": 6, "method": "subscription.reconnect", "params": map[string]any{"lastEventId": "evt_1", "subscriptions": []string{sub2}}}, false)
	var rec map[string]any
	for rec == nil {
		m := ws.recv(t)
		if m["type"] == "success" && m["id"] == float64(6) {
			rec = m["result"].(map[string]any)
		}
	}
	if rec["restored"] != true || rec["missedEvents"].(float64) < 2 {
		t.Fatalf("%v", rec)
	}
	// plain GET without upgrade is told so
	resp2, _ := http.Get(ts.URL + "/threads/w1/stream")
	if resp2.StatusCode != 426 {
		t.Fatalf("%d", resp2.StatusCode)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := agentprotocol.New(agentprotocol.Options{}); err == nil {
		t.Fatal("no agents must fail")
	}
	g := chatAgent(t)
	if _, err := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "a", Graph: g}, {ID: "a", Graph: g}}}); err == nil {
		t.Fatal("duplicate agent must fail")
	}
	if _, err := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "", Graph: g}}}); err == nil {
		t.Fatal("missing id must fail")
	}
}

func TestStateSurvivesServerRestart(t *testing.T) {
	ctx := context.Background()
	g := chatAgent(t) // same graph + checkpointer behind two server instances
	opts := agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "chat", Graph: g}}}
	c1, _ := newServer(t, opts)
	if _, err := c1.CreateAndWait(ctx, agentprotocol.RunCreate{ThreadID: "keep", IfNotExists: "create", Input: json.RawMessage(`"weather?"`)}); err != nil {
		t.Fatal(err)
	}
	c2, _ := newServer(t, opts) // fresh in-memory records
	th, err := c2.GetThread(ctx, "keep")
	if err != nil || len(th.Messages) != 4 {
		t.Fatalf("thread must be rebuilt from the checkpointer: %+v %v", th, err)
	}
}

func TestResumedRunDoesNotReplayAndKeepsToolCallIDs(t *testing.T) {
	ctx := context.Background()
	pay := tools.New("pay", "pay", func(_ context.Context, a cityArgs) (any, error) { return "paid " + a.City, nil })
	n := 0
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		if last, _ := messages.Last(msgs); last.Role == messages.RoleTool {
			return messages.AI("done"), nil
		}
		n++
		return messages.AI("paying", messages.ToolCall{ID: fmt.Sprintf("pay_%d", n), Name: "pay", Args: map[string]any{"city": "x"}}), nil
	}}
	g, err := agents.Create(agents.Options{Model: model, Tools: []tools.Tool{pay},
		Middleware: []agents.Middleware{amw.HumanInTheLoop{Tools: map[string]amw.ToolPolicy{"pay": {}}}},
		Compile:    []graph.CompileOption{graph.WithCheckpointer(checkpoint.NewMemory())}})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := newServer(t, agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "p", Graph: g}}})
	f := agentprotocol.Filter{Channels: []string{"input", "lifecycle", "messages", "tools"}}
	evs := collect(t, ctx, c, "r1", f, func() {
		_, _ = c.Command(ctx, "r1", "run.start", map[string]any{"assistantId": "p", "input": "pay"})
	})
	var id string
	for _, e := range evs {
		if e.Method == "input.requested" {
			id, _ = dataOf(e)["interruptId"].(string)
		}
	}
	if id == "" {
		t.Fatal("no interrupt")
	}
	evs = collect(t, ctx, c, "r1", f, func() {
		_, _ = c.Command(ctx, "r1", "input.respond", map[string]any{"namespace": []string{}, "interruptId": id, "response": []map[string]any{{"type": "approve"}}})
	})
	starts := 0
	var toolID string
	for _, e := range evs {
		d := dataOf(e)
		if e.Method == "messages" && d["event"] == "message-start" {
			starts++
			if d["role"] != "ai" {
				t.Fatal("role")
			}
		}
		if e.Method == "tools" && d["event"] == "tool-finished" {
			toolID, _ = d["toolCallId"].(string)
		}
	}
	if starts != 1 { // only the final "done" message: the proposal was announced before the pause
		t.Fatalf("a resumed run replayed %d messages", starts)
	}
	if toolID != "pay_1" {
		t.Fatalf("tool id after a resume must still be the streamed call id, got %q", toolID)
	}
}
