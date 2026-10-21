package compress

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
)

// RTK routes simple shell commands through the rtk output filter when the
// decider says the command's previous output was large. Wire Rewrite into
// backends.LocalShell and wrap the executor with Executor so output sizes are
// recorded.
type RTK struct {
	Decider Decider
	// LookPath finds the rtk binary. Nil searches the PATH in Env.
	LookPath func(string) (string, error)
	// Env is the shell's child environment (LocalShell's Env). Nil means the
	// host environment. Its PATH decides whether rtk exists, because the
	// shell cannot run an rtk that its own PATH does not contain.
	Env []string

	mu  sync.Mutex
	max map[string]int // largest output seen per command key
}

// NewRTK wraps d in NewGuarded. Set Env to the shell's environment if it
// differs from the host.
func NewRTK(d Decider) *RTK {
	return &RTK{Decider: NewGuarded(d)}
}

// lookPath finds name in the PATH entries of Env (or the host PATH when Env
// is nil). Empty entries are skipped; the last PATH entry wins, as in exec.
func (r *RTK) lookPath(name string) (string, error) {
	path := os.Getenv("PATH")
	if r.Env != nil {
		path = ""
		for _, kv := range r.Env {
			if v, ok := strings.CutPrefix(kv, "PATH="); ok {
				path = v
			}
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// Rewrite returns "rtk " + command when rtk is on PATH, the command is simple
// and the decider routes it. Otherwise it returns command unchanged.
func (r *RTK) Rewrite(ctx context.Context, command string) string {
	if !rtkSimple(command) {
		return command
	}
	look := r.LookPath
	if look == nil {
		look = r.lookPath
	}
	if _, err := look("rtk"); err != nil {
		return command
	}
	d := r.Decider
	if d == nil {
		d = RuleDecider{}
	}
	ans, err := d.Decide(ctx, ShellState(command, r.prevOutput(command)), []Question{ShellQuestion()})
	if err != nil || len(ans) != 1 || ans[0].ID != QRouteRTK || !ans[0].True {
		return command
	}
	return "rtk " + command
}

// Executor returns an executor that runs commands through inner and records
// the output size of each run, which feeds prev_output_bytes.
func (r *RTK) Executor(inner backends.Executor) backends.Executor {
	return rtkExecutor{inner: inner, r: r}
}

type rtkExecutor struct {
	inner backends.Executor
	r     *RTK
}

// Execute runs the command and records its output size. A leading "rtk " is
// stripped so a rewritten command counts toward the same key.
func (e rtkExecutor) Execute(ctx context.Context, command string, timeout time.Duration) (backends.ExecResult, error) {
	res, err := e.inner.Execute(ctx, command, timeout)
	e.r.record(strings.TrimPrefix(command, "rtk "), len(res.Output))
	return res, err
}

func (r *RTK) record(command string, size int) {
	key := rtkKey(command)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.max == nil {
		r.max = make(map[string]int)
	}
	if size > r.max[key] {
		r.max[key] = size
	}
}

// prevOutput returns the largest output recorded for command, or -1 if none.
func (r *RTK) prevOutput(command string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.max[rtkKey(command)]; ok {
		return v
	}
	return -1
}

// rtkKey is the first two words of the command, e.g. "go test".
func rtkKey(command string) string {
	f := strings.Fields(command)
	if len(f) > 2 {
		f = f[:2]
	}
	return strings.Join(f, " ")
}

// rtkSimple reports whether command is one plain command that "rtk " can
// prefix without changing its meaning. Pipes, lists, redirections, command
// substitution, grouping and leading VAR=value assignments are not simple.
func rtkSimple(command string) bool {
	if strings.TrimSpace(command) == "" || strings.ContainsAny(command, "|&;<>`()\n\r") || strings.Contains(command, "$(") {
		return false
	}
	return !strings.Contains(strings.Fields(command)[0], "=")
}
