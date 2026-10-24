package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/internal/team"
)

// rpc drives serve over pipes and keeps every line the server wrote.
type rpc struct {
	t     *testing.T
	in    *io.PipeWriter
	lines chan string
	all   []string
	mu    sync.Mutex
	next  int
	done  chan error
}

func startServe(t *testing.T) *rpc {
	t.Helper()
	for _, r := range team.Roles {
		if team.ModelSpec(r) != "" {
			t.Skipf("offline run only: role %s has a model spec", r)
		}
	}
	// The process stdout must stay empty: it would corrupt the MCP stream.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = pw
	leaked := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(pr); leaked <- b }()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	c := &rpc{t: t, in: inW, lines: make(chan string, 64), done: make(chan error, 1)}
	go func() {
		c.done <- serve(ctx, inR, outW, io.Discard)
		close(c.done)
		outW.Close()
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			c.mu.Lock()
			c.all = append(c.all, sc.Text())
			c.mu.Unlock()
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	t.Cleanup(func() {
		cancel()
		inW.Close()
		<-c.done
		os.Stdout = old
		pw.Close()
		if b := <-leaked; len(b) != 0 {
			t.Errorf("serve wrote to os.Stdout: %q", b)
		}
	})
	c.call("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "1"}})
	fmt.Fprintln(inW, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	return c
}

// call sends one request and returns its result.
func (c *rpc) call(method string, params any) map[string]any {
	c.t.Helper()
	c.next++
	id := c.next
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	fmt.Fprintln(c.in, string(b))
	timeout := time.After(30 * time.Second)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				c.t.Fatalf("%s: server closed stdout", method)
			}
			var m struct {
				ID     *int           `json:"id"`
				Result map[string]any `json:"result"`
				Error  any            `json:"error"`
			}
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				c.t.Fatalf("not JSON: %q", l)
			}
			if m.ID != nil && *m.ID == id {
				if m.Error != nil {
					c.t.Fatalf("%s: %v", method, m.Error)
				}
				return m.Result
			}
		case <-timeout:
			c.t.Fatalf("%s: no answer", method)
		}
	}
}

// tool calls a tool and returns its text.
func (c *rpc) tool(name string, args map[string]any) string {
	c.t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		c.t.Fatalf("%s: no content: %v", name, res)
	}
	return fmt.Sprint(content[0].(map[string]any)["text"])
}

func (c *rpc) checkStdout() {
	c.t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.all {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) != nil || m["jsonrpc"] != "2.0" {
			c.t.Errorf("stdout carries a non JSON-RPC line: %q", l)
		}
	}
}

func TestServeListsTeamTools(t *testing.T) {
	c := startServe(t)
	res := c.call("tools/list", map[string]any{})
	got := map[string]bool{}
	for _, x := range res["tools"].([]any) {
		got[x.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{"team_submit", "team_status", "team_queue", "team_ask_worker", "team_memory"} {
		if !got[n] {
			t.Errorf("missing tool %s (have %v)", n, got)
		}
	}
	if got["shell"] {
		t.Error("serve mode must give no shell tool to the bridge")
	}
	c.checkStdout()
}

func TestServeStopsOnEOF(t *testing.T) {
	c := startServe(t)
	c.in.Close()
	select {
	case err := <-c.done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after stdin EOF")
	}
	c.checkStdout()
}

func TestServeStopsOnContextCancel(t *testing.T) {
	inR, inW := io.Pipe() // stays open and idle
	defer inW.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, inR, io.Discard, io.Discard) }()
	time.Sleep(500 * time.Millisecond) // team up, server blocked on stdin
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop on cancel while stdin was idle")
	}
}

func TestServeRestoresGlobals(t *testing.T) {
	oldLog, oldConsole := log.Writer(), team.Console
	t.Cleanup(func() { log.SetOutput(oldLog); team.Console = oldConsole })
	logSentinel, consoleSentinel := &bytes.Buffer{}, &bytes.Buffer{}
	log.SetOutput(logSentinel)
	team.Console = consoleSentinel
	inR, inW := io.Pipe()
	inW.Close()
	if err := serve(context.Background(), inR, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if log.Writer() != io.Writer(logSentinel) || team.Console != io.Writer(consoleSentinel) {
		t.Errorf("serve leaked globals: log %v console %v", log.Writer(), team.Console)
	}
}

func TestServeOptionsHaveNoShell(t *testing.T) {
	opts, _ := serveOptions("k")
	for _, tl := range opts.Tools {
		if tl.Name() == "shell" {
			t.Fatal("serve must not give a worker the shell tool")
		}
	}
	if opts.ManagerKey != "k" {
		t.Fatal("the manager key must travel to the team")
	}
}

// The manager runs with a random key; team_submit only works if the bridge got it.
func TestServeSubmitUsesTheGeneratedKey(t *testing.T) {
	t.Setenv("MANAGER_API_KEY", "operator-key-ignored")
	c := startServe(t)
	if out := c.tool("team_submit", map[string]any{"request": "hello"}); !strings.Contains(out, "thread_id=") {
		t.Fatalf("submit: %q", out)
	}
	c.checkStdout()
}

func TestServePresentOverStdio(t *testing.T) {
	c := startServe(t)
	var found bool
	for _, x := range c.call("prompts/list", map[string]any{})["prompts"].([]any) {
		found = found || x.(map[string]any)["name"] == "present"
	}
	if !found {
		t.Fatal("prompts/list lacks present")
	}
	got := c.call("prompts/get", map[string]any{"name": "present"})
	if b, _ := json.Marshal(got); !strings.Contains(string(b), "team_present") {
		t.Fatalf("prompts/get: %s", b)
	}
	roll := c.tool("team_present", map[string]any{})
	n := len(team.Cast)
	if want := fmt.Sprintf("%d of %d present", n, n); !strings.Contains(roll, want) {
		t.Fatalf("want %q in:\n%s", want, roll)
	}
	if got := strings.Count(roll, "present: Presente! "); got != n {
		t.Errorf("%d Presente! lines, want %d:\n%s", got, n, roll)
	}
	t.Log("\n" + roll)
	c.checkStdout()
}
