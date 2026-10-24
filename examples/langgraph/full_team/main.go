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
// "serve" runs the same team as an MCP server on stdio, for a chat (see serve.go).
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
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
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		go func() { <-ctx.Done(); stop() }() // a second signal kills the process
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

	dec, source := decider()
	rtk := compress.NewRTK(dec)
	dir, err := os.MkdirTemp("", "full_team")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	shell := shellTool(dir, rtk)
	// Keep 1 (default 4): the offline threads are short, so they need an old
	// turn to compress. NewCompression's guard is kept by wrapping it here.
	m := &meter{comp: middleware.Compression{Decider: compress.NewGuarded(dec), Keep: 1}}

	l, err := team.StartLocalWith(ctx, team.LocalOptions{
		Middleware: []agents.Middleware{m},
		Tools:      []tools.Tool{shell, tasksTool()},
		Offline:    team.OfflineWithTool(tasksToolName),
	})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(w, "decider %s; hub %s, manager %s, %d workers\n", source, l.HubURL, l.ManagerURL, len(l.Workers))

	out, err := team.Invoke(ctx, l.ManagerURL, messages.NewID(), team.DefaultRequest)
	if err != nil {
		cancel()
		l.Wait()
		return nil, err
	}
	team.Print(out)
	m.print(w)
	cancel()
	l.Wait()
	return m, nil
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

// anyLive reports whether any role runs on a real model.
func anyLive() bool {
	for _, r := range team.Roles {
		if team.ModelSpec(r) != "" {
			return true
		}
	}
	return false
}

// allowedPrograms is what the shell tool may run. Offline, the scripted run
// keeps go and git (it never calls the tool, so this is for the demo shape).
// When any role has a live model (a provider key, AGENT_MODEL or
// AGENT_MODEL_<ROLE>), go and git (which run code) are withheld: ls, grep and
// echo only.
func allowedPrograms(getenv func(string) string) []string {
	if getenv("ANTHROPIC_API_KEY") != "" || getenv("OPENAI_API_KEY") != "" || anyLive() {
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

// shellEnv is all the shell's child processes see: PATH and a HOME inside the
// scratch directory, so no provider key or other secret reaches a command.
func shellEnv(dir string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
}

// errOutside is the tool error for a path argument that leaves the workspace.
func errOutside(tok string) error {
	return tools.Errorf("shell: paths outside the workspace are not allowed: %s", tok)
}

// shellSegments splits a command line into segments at ; & | and newline, and
// each segment into words, honouring simple '...' and "..." quotes (dropped).
// A separator inside quotes or an unterminated quote is an error: the library
// allowlist splits on separators before quotes, the shell after, so such a line
// would be read differently by the two.
func shellSegments(cmd string) ([][]string, error) {
	var segs [][]string
	var toks []string
	var cur strings.Builder
	var quote rune
	started := false
	endWord := func() {
		if started {
			toks = append(toks, cur.String())
			cur.Reset()
			started = false
		}
	}
	for _, r := range cmd {
		switch {
		case quote != 0:
			switch r {
			case quote:
				quote = 0
			case ';', '&', '|', '\n':
				return nil, tools.Errorf("shell: a separator (; & |) inside quotes is not allowed")
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == ' ' || r == '\t':
			endWord()
		case r == ';' || r == '&' || r == '|' || r == '\n':
			endWord()
			segs, toks = append(segs, toks), nil
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, tools.Errorf("shell: unterminated quote")
	}
	endWord()
	return append(segs, toks), nil
}

// inside reports whether p, with symlinks resolved, is within the resolved dir.
// A path that does not exist is not inside anything: callers decide what to do.
func inside(realDir, p string) (ok, exists bool) {
	if _, err := os.Lstat(p); err != nil {
		return false, false
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false, true
	}
	rel, err := filepath.Rel(realDir, r)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), true
}

// dotGlob reports whether a wildcard component that starts with a dot can match
// "." or ".." (bash before 5.2 expands .* to them): such a glob would climb out.
func dotGlob(tok string) bool {
	for _, c := range strings.Split(tok, "/") {
		if !strings.HasPrefix(c, ".") || !strings.ContainsAny(c, "*?[") {
			continue
		}
		for _, d := range []string{".", ".."} {
			if ok, err := filepath.Match(c, d); ok || err != nil {
				return true
			}
		}
	}
	return false
}

// bracketsToAny replaces every bracket expression with "?": it matches one
// character in Go and in bash, so the result matches a superset of what either
// reads. An unterminated [ is a literal in bash and is replaced too.
func bracketsToAny(tok string) string {
	var b strings.Builder
	for i := 0; i < len(tok); i++ {
		if tok[i] != '[' {
			b.WriteByte(tok[i])
			continue
		}
		j := i + 1
		if j < len(tok) && (tok[j] == '!' || tok[j] == '^') {
			j++
		}
		if j < len(tok) && tok[j] == ']' {
			j++
		}
		for j < len(tok) && tok[j] != ']' {
			if tok[j] == '[' && j+1 < len(tok) && tok[j+1] == ':' {
				if k := strings.Index(tok[j+2:], ":]"); k >= 0 {
					j += k + 4
					continue
				}
			}
			j++
		}
		b.WriteByte('?')
		if j < len(tok) {
			i = j
		}
	}
	return b.String()
}

// globPathLike reports whether a glob token names a path: it has a slash, a
// dot-leading part or a .. component. A bare pattern such as [0-9] or *.go is not.
func globPathLike(tok string) bool {
	return strings.Contains(tok, "/") || strings.HasPrefix(tok, ".") || tok == ".."
}

// globRule is the structural rule for a path-like glob, independent of how Go
// or bash read the pattern: no [ at all, no .. after the first wildcard
// component, and the literal prefix (the components before it) must be inside dir.
func globRule(dir, realDir, tok string) error {
	if strings.Contains(tok, "[") {
		return errOutside(tok)
	}
	comps := strings.Split(tok, "/")
	first := slices.IndexFunc(comps, func(c string) bool { return strings.ContainsAny(c, "*?") })
	if first < 0 || slices.Contains(comps[first:], "..") {
		return errOutside(tok)
	}
	prefix := strings.Join(comps[:first], "/")
	switch {
	case filepath.IsAbs(tok) && prefix == "":
		prefix = "/"
	case !filepath.IsAbs(tok):
		prefix = dir + "/" + prefix
	}
	prefix = filepath.Clean(prefix)
	if rel, err := filepath.Rel(dir, prefix); err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return errOutside(tok)
	}
	if ok, exists := inside(realDir, prefix); exists && !ok {
		return errOutside(tok)
	}
	return nil
}

// confine rejects a command line when a non-flag token (a bare name too, it may
// be a symlink) resolves, symlinks included, outside dir. Tokens that do not exist on disk
// (a grep pattern such as /api/coupon) pass. A path-like glob is judged by its
// literal prefix, not its matches (see globRule); a bare one (*.go, [0-9]) by its matches.
// Flag values and attached short flags are checked as paths too, and the
// callers refuse backslashes and braces, so the tokens match what the shell sees.
func confine(dir, command string) error {
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	// pathOK checks one candidate path; a name that is not on disk passes.
	pathOK := func(tok string) error {
		if strings.HasPrefix(tok, "~") {
			return errOutside(tok)
		}
		full := tok
		if !filepath.IsAbs(tok) {
			full = dir + "/" + tok // not Join: keep "link/.." for the OS to resolve
		}
		cands := []string{full, filepath.Clean(full)}
		if strings.ContainsAny(tok, "*?[") {
			if dotGlob(tok) {
				return errOutside(tok)
			}
			if globPathLike(tok) {
				if err := globRule(dir, realDir, tok); err != nil {
					return err
				}
			}
			// bash and Go read [...] differently, so also glob a superset in
			// which every bracket expression is one "?".
			for _, t := range []string{tok, bracketsToAny(tok)} {
				g := t
				if !filepath.IsAbs(t) {
					g = dir + "/" + t
				}
				for _, p := range []string{g, filepath.Clean(g)} {
					m, _ := filepath.Glob(p)
					cands = append(cands, m...)
				}
			}
		}
		for _, c := range cands {
			if ok, exists := inside(realDir, c); exists && !ok {
				return errOutside(tok)
			}
		}
		return nil
	}
	segs, err := shellSegments(command)
	if err != nil {
		return err
	}
	for _, toks := range segs {
		for len(toks) > 0 && isAssign(toks[0]) { // VAR=value words come before the program
			toks = toks[1:]
		}
		if len(toks) == 0 {
			continue
		}
		prog := filepath.Base(toks[0])
		dashdash := false
		for _, tok := range toks {
			if dashdash || !strings.HasPrefix(tok, "-") {
				if err := pathOK(tok); err != nil {
					return err
				}
				continue
			}
			if tok == "--" {
				dashdash = true
				continue
			}
			if name, val, ok := strings.Cut(tok, "="); strings.HasPrefix(tok, "--") {
				if deniedFlag(prog, name) {
					return tools.Errorf("shell: flag %s is not allowed", name)
				}
				if ok {
					if err := pathOK(val); err != nil {
						return err
					}
				}
				continue
			}
			// short flags: -f/etc/x, -C/, -nf
			if deniedFlag(prog, tok) {
				return tools.Errorf("shell: flag %s is not allowed", tok)
			}
			if i := strings.IndexAny(tok, "/~."); i > 0 {
				if err := pathOK(tok[i:]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// isAssign reports whether w is a leading VAR=value word.
func isAssign(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	return ok && name != "" && !strings.ContainsAny(name, "-/.~*?[")
}

// deniedFlag reports flags that read a file of patterns or paths, follow
// symlinks recursively, or move git/go to another directory. A long flag is
// denied when it is a prefix (3+ characters) of a denied name, since GNU
// getopt accepts abbreviations, unless it is itself a valid option (--include).
// Short grep flags match by prefix and, in an all-letter cluster such as -nf, by
// letter (-efoo is therefore denied too: it cannot be told from -f).
func deniedFlag(prog, flag string) bool {
	var short, long, keep []string
	exactShort := false
	switch prog {
	case "grep":
		short, long, keep = []string{"f", "R"}, []string{"--file", "--exclude-from", "--include-from", "--dereference-recursive"}, []string{"--include", "--exclude"}
	case "git", "go":
		short, long, exactShort = []string{"C"}, []string{"--git-dir", "--work-tree"}, true
	}
	if strings.HasPrefix(flag, "--") {
		if len(flag) < 5 || slices.Contains(keep, flag) {
			return slices.Contains(long, flag)
		}
		return slices.ContainsFunc(long, func(l string) bool { return strings.HasPrefix(l, flag) })
	}
	letters := strings.TrimPrefix(flag, "-")
	if exactShort {
		return slices.Contains(short, letters)
	}
	cluster := letters != ""
	for _, r := range letters {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			cluster = false
		}
	}
	for _, f := range short {
		if strings.HasPrefix(letters, f) || cluster && strings.Contains(letters, f) {
			return true
		}
	}
	return false
}

// shellTool runs allowlisted commands in dir. Simple ones go through rtk when
// the decider routes them; rtk.Executor records output sizes for that decision.
// The command line is refused when it holds a redirection, an expansion or a
// backtick (the library's allowlist splits on ; & | and newlines only, so
// "echo $KEY" or "echo x >> ~/.bashrc" would otherwise pass it) or a path
// argument outside dir (see confine).
func shellTool(dir string, rtk *compress.RTK) tools.Tool {
	env := shellEnv(dir)
	rtk.Env = env
	allowed := allowedPrograms(os.Getenv)
	sh := backends.LocalShell{
		Filesystem:      backends.Filesystem{Root: dir},
		Env:             env,
		AllowedCommands: allowed,
		Rewrite:         rtk.Rewrite,
	}
	run := rtk.Executor(sh)
	return tools.New("shell", "Run one shell command in the team workspace (no redirections, $, backticks, backslashes or braces, paths stay inside the workspace). Allowed programs: "+strings.Join(allowed, ", ")+".",
		func(ctx context.Context, a shellArgs) (any, error) {
			if strings.ContainsAny(a.Command, "<>$`\\{}") {
				return nil, tools.Errorf("shell: redirections (< >), $, backticks, backslashes and braces are not allowed")
			}
			if err := confine(dir, a.Command); err != nil {
				return nil, err
			}
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
