// full_team: the independent_engineering_team flow with the compress layer on
// every worker. Each worker's model call goes through NewCompression (old turns
// caveman-compressed, optional terse-reply directive), and the workers get a
// shell tool whose simple commands are routed through rtk when the decider says so.
//
// Offline a scripted decider answers every question, so the savings show
// without a model. LAYA_URL points the decider at a real laya-serve endpoint.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/agents/middleware"
	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/internal/team"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dec, source := decider()
	rtk := compress.NewRTK(dec)
	dir, err := os.MkdirTemp("", "full_team")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	shell := shellTool(dir, rtk)
	// Keep 1 (default 4): the offline threads are short, so they need an old
	// turn to compress. NewCompression's guard is kept by wrapping it here.
	m := &meter{comp: middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}}

	l, err := team.StartLocalWith(ctx, team.LocalOptions{
		Middleware: []agents.Middleware{m},
		Tools:      []tools.Tool{shell},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("decider %s; hub %s, manager %s, %d workers\n", source, l.HubURL, l.ManagerURL, len(l.Workers))

	out, err := team.Invoke(ctx, l.ManagerURL, messages.NewID(), team.DefaultRequest)
	if err != nil {
		log.Fatal(err)
	}
	team.Print(out)
	m.print()
	cancel()
	l.Wait()
}

// decider is the real one when LAYA_URL is set, else the scripted offline one.
func decider() (compress.Decider, string) {
	if u := os.Getenv("LAYA_URL"); u != "" {
		return compress.HTTPDecider{URL: u, APIKey: os.Getenv("LAYA_API_KEY"), Model: os.Getenv("LAYA_MODEL")}, "laya-serve " + u
	}
	return compress.FuncDecider(scripted), "scripted (offline)"
}

// scripted answers as a confident model would, whatever the state. Compression
// and rtk routing are on, the reply directive is off; the thresholds are the
// rules' own, but every turn qualifies, so the offline run shows the effect.
// ponytail: no real judgement offline; the laya decider makes the real calls.
func scripted(_ context.Context, _ compress.State, qs []compress.Question) ([]compress.Answer, error) {
	out := make([]compress.Answer, 0, len(qs))
	for _, q := range qs {
		switch q.ID {
		case compress.QLevel:
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, Pick: string(compress.Full), Dist: map[string]float64{string(compress.Full): 0.95}})
		case compress.QCompressReply:
			out = append(out, compress.Answer{ID: q.ID, Kind: q.Kind, P: 0.05})
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
// Tokens are estimated as bytes/4, the same estimate the compress package uses.
type meter struct {
	comp middleware.Compression

	mu                   sync.Mutex
	calls, rewritten     int
	before, after, bytes int
}

func (*meter) Name() string { return "compression_meter" }

func (m *meter) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	in := textBytes(req.Messages)
	return m.comp.WrapModelCall(ctx, req, func(ctx context.Context, r *agents.ModelRequest) (messages.Message, error) {
		out := textBytes(r.Messages)
		m.mu.Lock()
		m.calls++
		m.before += in
		m.after += out
		if out != in {
			m.rewritten++
		}
		m.mu.Unlock()
		return next(ctx, r)
	})
}

func (m *meter) print() {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, rtkErr := exec.LookPath("rtk")
	fmt.Println("== compression ==")
	fmt.Printf("model calls: %d, rewritten by compression: %d\n", m.calls, m.rewritten)
	fmt.Printf("estimated tokens: %d before, %d after\n", m.before/4, m.after/4)
	fmt.Printf("rtk on PATH: %v\n", rtkErr == nil)
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
