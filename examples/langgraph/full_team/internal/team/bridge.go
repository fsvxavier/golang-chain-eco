package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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
	// PresentTimeout bounds each worker's answer to team_present (default 30s).
	PresentTimeout time.Duration
	// Expected is the roster team_present checks against; nil means Cast.
	Expected []struct{ ID, Role string }

	runs sync.Map // thread id -> run id of the last team_submit, so team_status can say a run failed
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
		Request  string   `json:"request" jsonschema:"what the team must deliver"`
		DocRoots []string `json:"docs_roots,omitempty" jsonschema:"optional directories (relative to the workspace, or absolute under it) where the product owner may write markdown documents: documentation, plan, next steps. Leave it empty when the request names a folder to save a plan or document in: the folder is taken from the request text. With no folder anywhere the PO writes nothing"`
	}
	threadArgs struct {
		Thread string `json:"thread_id" jsonschema:"thread_id returned by team_submit"`
	}
	askArgs struct {
		Worker   string `json:"worker" jsonschema:"worker id from team_queue"`
		Question string `json:"question"`
	}
	graphArgs struct {
		Assistant string         `json:"assistant_id" jsonschema:"graph deployed on the server, e.g. engineering_team"`
		Thread    string         `json:"thread_id,omitempty" jsonschema:"existing thread; empty creates one"`
		Input     map[string]any `json:"input" jsonschema:"initial state values"`
		Wait      bool           `json:"wait,omitempty" jsonschema:"block until the run ends and return the final state"`
		URL       string         `json:"url,omitempty" jsonschema:"Agent Server base URL; default MANAGER_URL"`
	}
	resumeArgs struct {
		Assistant string `json:"assistant_id"`
		Thread    string `json:"thread_id"`
		Value     any    `json:"value" jsonschema:"answer to the interrupt"`
		Wait      bool   `json:"wait,omitempty"`
		URL       string `json:"url,omitempty"`
	}
	stateArgs struct {
		Thread string `json:"thread_id"`
		URL    string `json:"url,omitempty"`
	}
	memArgs struct {
		Scope string `json:"scope" jsonschema:"the request's thread_id"`
		Query string `json:"query"`
	}
)

// docDest finds the folder a request asks to save a document in: an absolute
// path after "save/write/plan ... in/to/folder". The last one wins, as the
// first path in a request is usually what to read. The PO's write guards still
// confine it to the workspace and to .md files.
var docDest = regexp.MustCompile(`(?i)(?:grav\w*|salv\w*|escrev\w*|write|save|plano|plan|documenta\w*)[^/]{0,80}?(?:pasta|folder|diret\w+|directory|em|in|to|para)\s+(/[^\s,;"')]+)`)

func inferDocRoot(request string) string {
	m := docDest.FindAllStringSubmatch(request, -1)
	if len(m) == 0 {
		return ""
	}
	return strings.TrimRight(m[len(m)-1][1], ".:")
}

// listArgs wraps a tool so that a string given where it takes a list of strings
// becomes a one-item list: some clients send a single path as plain text, and
// refusing the whole demand over that costs more than reading it.
type listArgs struct {
	tools.Tool
	keys []string
}

func (l listArgs) Call(ctx context.Context, args map[string]any) (any, error) {
	for _, k := range l.keys {
		if s, ok := args[k].(string); ok {
			args[k] = []any{s}
			if s == "" {
				args[k] = []any{}
			}
		}
	}
	return l.Tool.Call(ctx, args)
}

// Tools returns the bridge tools.
func (b *Bridge) Tools() []tools.Tool {
	ts := b.tools()
	for i, t := range ts {
		if t.Name() == "team_submit" {
			ts[i] = listArgs{t, []string{"backend_roots", "backend_urls", "docs_roots"}}
		}
	}
	return ts
}

func (b *Bridge) tools() []tools.Tool {
	return []tools.Tool{
		tools.New("team_submit", "Send a demand to the engineering team. A folder named in the request for saving a plan or document is enough for the PO to write there (no other argument needed). Returns at once with thread_id and run_id; follow it with team_status.",
			func(ctx context.Context, a submitArgs) (any, error) {
				inferred := false
				if len(a.DocRoots) == 0 {
					if r := inferDocRoot(a.Request); r != "" {
						a.DocRoots, inferred = []string{r}, true
					}
				}
				tid, err := b.Manager.CreateThread(ctx, map[string]any{"request": a.Request})
				if err != nil {
					return nil, tools.Errorf("team_submit: %v", err)
				}
				run, err := b.Manager.CreateRun(ctx, tid, remote.RunRequest{AssistantID: "engineering_team", Input: map[string]any{"request": a.Request, "scope": map[string]any{"doc_roots": a.DocRoots}}})
				if err != nil {
					return nil, tools.Errorf("team_submit: %v", err)
				}
				docs := "docs_roots=none (the PO writes nothing; pass docs_roots to let it save a plan)"
				if len(a.DocRoots) > 0 {
					docs = "docs_roots=" + strings.Join(a.DocRoots, ",")
					if inferred {
						docs += " (taken from the request)"
					}
				}
				b.runs.Store(tid, run.RunID)
				return fmt.Sprintf("thread_id=%s run_id=%s status=%s %s", tid, run.RunID, run.Status, docs), nil
			}),
		tools.New("team_status", "Show the delivery state of a request: task statuses (received ... completed), report and handoff history.",
			func(ctx context.Context, a threadArgs) (any, error) {
				s, err := b.Manager.GetState(ctx, a.Thread)
				if err != nil {
					if strings.Contains(err.Error(), "404") {
						return nil, tools.Errorf("team_status: thread %s not found: the team keeps threads in memory, so a restart of team-mcp (reconnecting the MCP, a new binary) ends them; submit the demand again", a.Thread)
					}
					return nil, tools.Errorf("team_status: %v", err)
				}
				res := map[string]any{
					"status": s.Values["status"], "report": s.Values["report"],
					"history": s.Values["history"], "running": s.Next,
				}
				// "running" is only the next node: a run that failed still lists it, so say so
				if id, ok := b.runs.Load(a.Thread); ok {
					if run, err := b.Manager.GetRun(ctx, a.Thread, id.(string)); err == nil && (run.Status == "error" || run.Status == "interrupted") {
						res["run"] = map[string]any{"status": run.Status, "error": run.Error}
						res["running"] = []string{}
					}
				}
				out, _ := json.MarshalIndent(res, "", "  ")
				return string(out), nil
			}),
		tools.New("team_cancel", "Cancel a request (end its thread): queued jobs are dropped, running agents are stopped and the delivery ends as cancelled. Use the thread_id from team_submit.",
			func(ctx context.Context, a threadArgs) (any, error) {
				n, err := b.Hub.CancelScope(ctx, a.Thread)
				if err != nil {
					return nil, tools.Errorf("team_cancel: %v", err)
				}
				return fmt.Sprintf("thread %s cancelled: %d job(s) ended; the delivery stops at its next step", a.Thread, n), nil
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
		tools.New("team_present", "Roll call: ask every registered worker to answer Presente with its id, role and model. Returns one row per worker and 'N of M present'.",
			func(ctx context.Context, _ struct{}) (any, error) { return b.rollCall(ctx) }),
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

// rollCallPrompt is what each worker receives. The markers are what the
// offline Brain keys on; a real model just follows the sentence.
func rollCallPrompt(w WorkerInfo) string {
	return "ROLL CALL: the teacher is checking attendance.\n" +
		"WORKER: " + w.ID + " ROLE: " + w.Role + "\n" +
		"Answer with ONE short line that starts with \"Presente!\" and names you (the worker id), your role and your model."
}

// rollCall asks every worker of the expected roster in parallel and merges the
// hub's view in: a rostered worker the hub no longer lists (lease expired or
// never started) is absent, a registered worker outside the roster is an
// extra and is not counted. The model column comes from the hub (what the
// worker was built with), never from the model's answer.
func (b *Bridge) rollCall(ctx context.Context) (any, error) {
	s, err := b.Hub.State(ctx)
	if err != nil {
		return nil, tools.Errorf("team_present: %v", err)
	}
	per := b.PresentTimeout
	if per <= 0 {
		per = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, per+5*time.Second) // overall bound
	defer cancel()
	roster := b.Expected
	if roster == nil {
		roster = Cast
	}
	type entry struct {
		w          WorkerInfo
		registered bool
		extra      bool
	}
	reg := map[string]WorkerInfo{}
	for _, w := range s.Workers {
		reg[w.ID] = w
	}
	var es []entry
	inRoster := map[string]bool{}
	for _, e := range roster {
		inRoster[e.ID] = true
		w, ok := reg[e.ID]
		if !ok {
			w = WorkerInfo{ID: e.ID, Role: e.Role}
		}
		es = append(es, entry{w: w, registered: ok})
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].w.ID < es[j].w.ID })
	for _, w := range s.Workers {
		if !inRoster[w.ID] {
			es = append(es, entry{w: w, registered: true, extra: true})
		}
	}
	rows := make([]string, len(es))
	ok := make([]bool, len(es))
	var wg sync.WaitGroup
	for i, e := range es {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := e.w
			model := w.Model
			if model == "" {
				model = "unknown"
			}
			var status string
			if !e.registered {
				status = "absent: not registered (lease expired or never started)"
			} else {
				ans, err := b.askOne(ctx, w, per)
				status = "present: " + head(ans, 160)
				if err != nil || strings.TrimSpace(ans) == "" {
					if err == nil {
						err = errors.New("empty answer")
					}
					status = "absent: " + head(err.Error(), 100)
				} else {
					ok[i] = true
				}
			}
			if e.extra {
				status = "extra (not in the roster, not counted): " + status
			}
			rows[i] = fmt.Sprintf("- %s (%s) [model %s] %s", w.ID, w.Role, model, status)
		}()
	}
	wg.Wait()
	var absent []string
	n := 0
	for i, e := range es {
		switch {
		case e.extra:
		case ok[i]:
			n++
		default:
			absent = append(absent, e.w.ID)
		}
	}
	out := "ROLL CALL\n" + strings.Join(rows, "\n") + fmt.Sprintf("\n%d of %d present", n, len(roster))
	if len(absent) > 0 {
		out += "\nAbsent: " + strings.Join(absent, ", ")
	}
	return out, nil
}

// askOne puts the roll-call prompt to one worker over Agent Protocol, like team_ask_worker.
func (b *Bridge) askOne(ctx context.Context, w WorkerInfo, d time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	res, err := agentprotocol.NewClient(w.URL, "").CreateAndWait(ctx, agentprotocol.RunCreate{
		AgentID: w.Role, IfNotExists: "create",
		Messages: []agentprotocol.Message{{Role: "user", Content: rollCallPrompt(w)}},
	})
	if err != nil {
		return "", err
	}
	if res.Run != nil && res.Run.Status != "success" {
		return "", fmt.Errorf("run %s: %s", res.Run.Status, res.Run.Error)
	}
	if len(res.Messages) == 0 {
		return "", nil
	}
	return fmt.Sprint(res.Messages[len(res.Messages)-1].Content), nil
}

// presentPrompt is the /present slash command: it only tells the chat to call
// team_present, since a prompt alone cannot make anyone answer.
func presentPrompt() server.Prompt {
	return server.Prompt{
		Name:        "present",
		Description: "Roll call: every persona answers Presente",
		Get: func(context.Context, map[string]string) ([]server.PromptMessage, error) {
			return []server.PromptMessage{{Role: "user", Text: "Call the team_present tool now and print its result as a school roll call: " +
				"one line per worker with its name, role, model and its answer (keep the word Presente), " +
				"then end with the summary line 'N of M present' and the list of the absent ones. " +
				"If the tool is missing or returns an error, say the team integration is not working and show the error."}}, nil
		},
	}
}

// client returns the manager client, or one for url when the call names a
// different Agent Server.
func (b *Bridge) client(url string) *remote.Client {
	if url == "" || strings.TrimRight(url, "/") == b.Manager.BaseURL {
		return b.Manager
	}
	c := remote.NewClient(url)
	c.APIKey = "" // the manager key never leaves for a URL the chat named
	return c
}

func asJSON(v any) string {
	out, _ := json.MarshalIndent(v, "", "  ")
	return string(out)
}

// GraphTools talk to any graph on any Agent Server, not only the team.
func (b *Bridge) GraphTools() []tools.Tool {
	run := func(ctx context.Context, c *remote.Client, thread string, req remote.RunRequest, wait bool) (any, error) {
		if wait {
			out, err := c.Wait(ctx, thread, req)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			return asJSON(out), nil
		}
		r, err := c.CreateRun(ctx, thread, req)
		if err != nil {
			return nil, tools.Errorf("%v", err)
		}
		return fmt.Sprintf("thread_id=%s run_id=%s status=%s", thread, r.RunID, r.Status), nil
	}
	return []tools.Tool{
		tools.New("graph_run", "Run any graph on an Agent Server. Returns thread_id and run_id, or the final state with wait=true.",
			func(ctx context.Context, a graphArgs) (any, error) {
				c := b.client(a.URL)
				if a.Thread == "" {
					tid, err := c.CreateThread(ctx, nil)
					if err != nil {
						return nil, tools.Errorf("graph_run: %v", err)
					}
					a.Thread = tid
				}
				return run(ctx, c, a.Thread, remote.RunRequest{AssistantID: a.Assistant, Input: a.Input}, a.Wait)
			}),
		tools.New("graph_state", "Show a thread's values, next nodes and pending interrupts.",
			func(ctx context.Context, a stateArgs) (any, error) {
				s, err := b.client(a.URL).GetState(ctx, a.Thread)
				if err != nil {
					return nil, tools.Errorf("graph_state: %v", err)
				}
				return asJSON(s), nil
			}),
		tools.New("graph_resume", "Resume a thread stopped at an interrupt with the user's answer.",
			func(ctx context.Context, a resumeArgs) (any, error) {
				return run(ctx, b.client(a.URL), a.Thread, remote.RunRequest{AssistantID: a.Assistant, Command: map[string]any{"resume": a.Value}}, a.Wait)
			}),
	}
}

// Proxy starts an MCP server given as a command line (e.g. "ciam mcp") and
// returns its tools unchanged, so the bridge can offer them next to its own.
func Proxy(ctx context.Context, cmdline string) ([]tools.Tool, error) {
	f := strings.Fields(cmdline)
	if len(f) == 0 {
		return nil, nil
	}
	t, err := mcp.Stdio(ctx, f[0], f[1:], nil)
	if err != nil {
		return nil, err
	}
	c, err := mcp.Connect(ctx, t)
	if err != nil {
		return nil, err
	}
	return c.Tools(ctx, "")
}

// MCPServer serves the team tools, the generic graph tools and the tools of
// the servers named by BRIDGE_CIAM_CMD and BRIDGE_KNOWLEDGE_CMD (project
// context and RAG/CAG/KAG), e.g. "ciam mcp" and "knowledge mcp". A proxy that
// fails to start is logged and skipped; run it with ServeStdio.
func (b *Bridge) MCPServer(ctx context.Context) *server.Server {
	s := server.New("engineering-team", "1.0").AddTool(b.Tools()...).AddTool(b.GraphTools()...).AddPrompt(presentPrompt())
	for _, k := range []string{"BRIDGE_CIAM_CMD", "BRIDGE_KNOWLEDGE_CMD"} {
		ts, err := Proxy(ctx, os.Getenv(k))
		if err != nil {
			log.Printf("bridge: %s: %v (skipped)", k, err)
			continue
		}
		s.AddTool(ts...)
	}
	return s
}
