// full_team: the independent_engineering_team flow with the compress layer on
// every worker. Each worker's model call goes through NewCompression (old turns
// caveman-compressed, optional terse-reply directive), and the workers get a
// shell tool whose simple commands are routed through rtk when the decider says so.
//
// Offline a scripted decider answers every question, so the effect of each
// lever shows without a model (the numbers vary a little between runs).
// LAYA_URL points the decider at a real laya-serve endpoint.
//
// Two more rewrites come from the same middleware: Ponytail (a note on the
// last Human message, asking for less code) and TOON (JSON tool results
// rewritten to a shorter form). Offline, every worker calls open_tasks once per
// turn, so the list of tasks reaches the model as JSON and gets rewritten.
//
// The demand names a backend (a fake source tree and API, see
// internal/backend/fake), and the PO reads it through read-only guarded tools
// before writing the spec.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/backend"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/backend/fake"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/qa"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/team"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serve(ctx, os.Stdin, os.Stdout, os.Stderr); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := run(ctx, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run plays the demo once and prints the report to w. It returns the meter, so
// the test can check what the middleware did.
func run(ctx context.Context, w io.Writer) (*meter, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	dir, err := os.MkdirTemp("", "full_team")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	st := newStack(dir, dir)
	m, source := st.m, st.source

	// The demo demand names the fake backend: its source tree written into the
	// workspace and its API on loopback. The PO tools reach nothing else.
	root, err := fake.WriteRoot(dir)
	if err != nil {
		return nil, err
	}
	api := fake.Serve()
	defer api.Close()

	l, err := team.StartLocalWith(ctx, st.opts)
	if err != nil {
		return nil, err
	}
	st.hub.Store(team.NewHubClient(l.HubURL))
	fmt.Fprintf(w, "decider %s; hub %s, manager %s, %d workers\n", source, l.HubURL, l.ManagerURL, len(l.Workers))

	out, err := team.InvokeWithScope(ctx, l.ManagerURL, messages.NewID(), team.DefaultRequest,
		team.Scope{Roots: []string{root}, URLs: []string{api.URL}})
	if err != nil {
		cancel()
		l.Wait()
		return nil, err
	}
	// Offline the scripted PO always reads the fake backend, so a flag means the wiring broke.
	if spec := fmt.Sprint(out["spec"]); team.ModelSpec("po") == "" && strings.Contains(spec, "UNVERIFIED") {
		cancel()
		l.Wait()
		return nil, fmt.Errorf("offline PO produced no backend evidence:\n%s", spec)
	}
	team.Print(out)
	m.print(w)
	cancel()
	l.Wait()
	return m, nil
}

// stack is what the demo and serve mode share: the team's options (compression
// meter, tools, per-role tools, backend call counter, offline script).
type stack struct {
	opts   team.LocalOptions
	m      *meter
	hub    *atomic.Pointer[team.HubClient] // known once the team is up; until then no access
	source string
}

// newStack builds the options. dir is the shell tool's scratch directory;
// workspace is the backend read root ("" means WORKSPACE_ROOT, else the cwd).
func newStack(dir, workspace string) *stack {
	dec, source := decider()
	rtk := compress.NewRTK(dec)
	shell := shellTool(dir, rtk)
	// Keep 1 (default 4): the offline threads are short, so they need an old
	// turn to compress. NewCompression's guard is kept by wrapping it here.
	m := &meter{comp: middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}}
	hub := new(atomic.Pointer[team.HubClient])
	return &stack{m: m, hub: hub, source: source, opts: team.LocalOptions{
		Middleware:  []agents.Middleware{m},
		Tools:       []tools.Tool{tasksTool()},
		RoleTools:   workerTools(shell, hub.Load, workspace),
		CallCounter: backend.Calls,
		Offline:     team.OfflineWithTool(tasksToolName),
	}}
}

// decider is the real one when LAYA_URL is set, else the scripted offline one.
func decider() (compress.Decider, string) {
	if u := os.Getenv("LAYA_URL"); u != "" {
		return compress.HTTPDecider{URL: u, APIKey: os.Getenv("LAYA_API_KEY"), Model: os.Getenv("LAYA_MODEL")}, "laya-serve " + u
	}
	return compress.FuncDecider(scripted), "scripted (offline)"
}

// scripted answers as a confident model would, whatever the state. Compression,
// rtk routing and TOON are on (the default case answers yes); the reply
// directive is off. Ponytail is not a blanket yes: the rule decides it, so a
// note appears only where a conversation has a real share of code. The rest of
// the thresholds are the rules' own, but every turn qualifies, so the offline run
// shows the effect.
func scripted(ctx context.Context, st compress.State, qs []compress.Question) ([]compress.Answer, error) {
	out := make([]compress.Answer, 0, len(qs))
	for _, q := range qs {
		switch q.ID {
		case compress.QLevel:
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, Pick: string(compress.Full), Dist: map[string]float64{string(compress.Full): 0.95}})
		case compress.QCompressReply:
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, P: 0.05})
		case compress.QPonytail:
			ans, err := compress.RuleDecider{}.Decide(ctx, st, []compress.Question{q})
			if err != nil {
				return nil, err
			}
			out = append(out, ans...)
		default:
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, P: 0.95, True: true})
		}
	}
	return out, nil
}

// allowedPrograms is what the shell tool may run. Offline, the scripted run
// keeps go and git (it never calls the tool, so this is for the demo shape).
// With a real provider key the model is a live one that can write files, so
// go and git (which run code) are withheld: ls, grep and echo only.
func allowedPrograms(getenv func(string) string) []string {
	if getenv("ANTHROPIC_API_KEY") != "" || getenv("OPENAI_API_KEY") != "" {
		return []string{"ls", "grep", "echo"}
	}
	return []string{"go", "git", "ls", "grep", "echo"}
}

type shellArgs struct {
	Command string `json:"command"`
}

// tasksToolName is the read-only tool that returns the backlog as JSON.
const tasksToolName = "open_tasks"

// task is one row of the backlog. Every field is a primitive, so the list is a
// uniform array of objects and TOON writes it as a table.
type task struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Owner      string `json:"owner"`
	Complexity string `json:"complexity"`
}

// backlog is the static list open_tasks returns. The demo has no tracker of its own.
var backlog = []task{
	{"T-101", "Coupon model with a single-use constraint", "in_progress", "senior-1", "high"},
	{"T-102", "Atomic redeem on checkout", "todo", "", "high"},
	{"T-103", "Concurrent payment race test", "todo", "", "high"},
	{"T-104", "Payment failure rolls the coupon back", "todo", "", "medium"},
	{"T-105", "Coupon admin endpoint", "todo", "", "low"},
	{"T-106", "Discount line on the receipt", "todo", "", "low"},
	{"T-107", "Audit log for redemptions", "todo", "", "medium"},
	{"T-108", "Load test at 500 checkouts per second", "blocked", "qa-1", "medium"},
}

// tasksTool serves the backlog as a JSON array (the tool result is its JSON text).
func tasksTool() tools.Tool {
	return tools.New(tasksToolName, "List the open tasks of the coupon backlog as JSON.",
		func(context.Context, struct{}) (any, error) { return backlog, nil })
}

// shellTool runs allowlisted commands in dir. Simple ones go through rtk when
// the decider routes them; rtk.Executor records output sizes for that decision.
func shellTool(dir string, rtk *compress.RTK) tools.Tool {
	env := os.Environ()
	rtk.Env = env
	allowed := allowedPrograms(os.Getenv)
	sh := backends.LocalShell{
		Filesystem:      backends.Filesystem{Root: dir},
		Env:             env,
		AllowedCommands: allowed,
		Rewrite:         rtk.Rewrite,
	}
	run := rtk.Executor(sh)
	return tools.New("shell", "Run one shell command in the team workspace. Allowed programs: "+strings.Join(allowed, ", ")+".",
		func(ctx context.Context, a shellArgs) (any, error) {
			res, err := run.Execute(ctx, a.Command, 30*time.Second)
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("exit %d\n%s", res.ExitCode, res.Output), nil
		})
}

// meter wraps the compression middleware and counts what it does to each call.
// It sees the per-call copy the middleware hands on, so it counts the rewrites
// without changing the library. Tokens are estimated as bytes/4, the same
// estimate the compress package uses.
type meter struct {
	comp middleware.Compression

	mu               sync.Mutex
	calls, rewritten int
	before, after    int
	ponytail         int // model calls whose last Human message carries the Ponytail note
	toonResults      int // tool results rewritten to TOON
	toonSaved        int // bytes those rewrites saved
}

func (*meter) Name() string { return "compression_meter" }

func (m *meter) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	in := textBytes(req.Messages)
	orig := req.Messages // the middleware copies the slice, so this stays as it was
	return m.comp.WrapModelCall(ctx, req, func(ctx context.Context, r *agents.ModelRequest) (messages.Message, error) {
		out := textBytes(r.Messages)
		m.mu.Lock()
		m.calls++
		m.before += in
		m.after += out
		if out != in {
			m.rewritten++
		}
		if hasPonytailNote(r.Messages) {
			m.ponytail++
		}
		for i := range r.Messages {
			if i >= len(orig) || orig[i].Role != messages.RoleTool || r.Messages[i].Content == orig[i].Content {
				continue
			}
			m.toonResults++
			m.toonSaved += len(orig[i].Content) - len(r.Messages[i].Content)
		}
		m.mu.Unlock()
		return next(ctx, r)
	})
}

func (m *meter) print(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, rtkErr := exec.LookPath("rtk")
	fmt.Fprintln(w, "== compression ==")
	fmt.Fprintf(w, "model calls: %d, rewritten by compression: %d\n", m.calls, m.rewritten)
	fmt.Fprintf(w, "estimated tokens: %d before, %d after\n", m.before/4, m.after/4)
	fmt.Fprintf(w, "calls with the Ponytail note: %d\n", m.ponytail)
	fmt.Fprintf(w, "tool results rewritten to TOON: %d, bytes saved: %d\n", m.toonResults, m.toonSaved)
	fmt.Fprintf(w, "rtk on PATH: %v\n", rtkErr == nil)
}

// hasPonytailNote reports whether the last Human message carries one of the
// Ponytail directives, whatever level it was written at.
func hasPonytailNote(msgs []messages.Message) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != messages.RoleHuman {
			continue
		}
		text := msgs[i].Text()
		for _, lvl := range []compress.Level{compress.Lite, compress.Full, compress.Ultra} {
			if strings.Contains(text, compress.PonytailDirective(lvl)) {
				return true
			}
		}
		return false
	}
	return false
}

// textBytes sums the text of every message, content and text parts.
func textBytes(msgs []messages.Message) int {
	n := 0
	for _, msg := range msgs {
		n += len(msg.Content)
		for _, p := range msg.Parts {
			n += len(p.Text)
		}
	}
	return n
}

// workerTools is each role's own tools: the shell for everyone but the PO,
// which is read-only (a shell would be a side door around the backend guards),
// the PO's backend tools and QA's tools.
func workerTools(shell tools.Tool, hub func() *team.HubClient, dir string) func(role string) []tools.Tool {
	notPO := func(role string) []tools.Tool {
		if role == "po" {
			return nil
		}
		return []tools.Tool{shell}
	}
	return roleTools(notPO, backend.RoleTools(hub, backend.Config{WorkspaceRoot: dir}), qa.RoleTools(hub, os.Getenv("PLAYWRIGHT_CMD")))
}

// roleTools joins per-role tool sets: a role gets the tools of every set.
func roleTools(sets ...func(role string) []tools.Tool) func(role string) []tools.Tool {
	return func(role string) []tools.Tool {
		var out []tools.Tool
		for _, f := range sets {
			out = append(out, f(role)...)
		}
		return out
	}
}
