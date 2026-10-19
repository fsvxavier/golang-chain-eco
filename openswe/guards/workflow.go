package guards

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

// WorkflowDir is the directory whose changes need a human's approval: a
// workflow file runs with repository secrets, so an agent must not be able to
// push one by itself.
const WorkflowDir = ".github/workflows/"

// PushInfo describes a push that changes workflow files.
type PushInfo struct {
	// ID binds an approval to this exact content: repository, head commit and
	// the workflow diff. Change anything and the ID changes.
	ID          string
	Dir         string
	Remote      string
	Branch      string
	HeadSHA     string
	Files       []string
	DiffPreview string
}

// Approver records human approvals of workflow pushes.
type Approver interface {
	// Approved reports whether id was approved.
	Approved(ctx context.Context, id string) bool
	// Request asks a human to approve (post a card, send a message...). It must
	// be idempotent per id.
	Request(ctx context.Context, info PushInfo) error
}

// MemoryApprover is an in-process Approver; humans call Approve(id).
type MemoryApprover struct {
	mu        sync.Mutex
	approved  map[string]bool
	Requested []PushInfo
	// OnRequest, if set, is called once per newly requested id.
	OnRequest func(PushInfo)
}

// Approve marks id approved.
func (m *MemoryApprover) Approve(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.approved == nil {
		m.approved = map[string]bool{}
	}
	m.approved[id] = true
}

// Approved implements Approver.
func (m *MemoryApprover) Approved(_ context.Context, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.approved[id]
}

// Request implements Approver.
func (m *MemoryApprover) Request(_ context.Context, info PushInfo) error {
	m.mu.Lock()
	for _, r := range m.Requested {
		if r.ID == info.ID {
			m.mu.Unlock()
			return nil
		}
	}
	m.Requested = append(m.Requested, info)
	cb := m.OnRequest
	m.mu.Unlock()
	if cb != nil {
		cb(info)
	}
	return nil
}

// WorkflowPushGuard gates `git push` commands that would publish changes to
// .github/workflows/ on a human approval. It inspects the push before it runs
// (what commits are about to leave the sandbox), blocks when it cannot inspect
// safely, and ties each approval to the exact diff.
type WorkflowPushGuard struct {
	// Exec runs read-only git commands in the sandbox.
	Exec     backends.Executor
	Approver Approver
	// ShellTools overrides ShellTools.
	ShellTools map[string][]string
}

// Name implements agents.Middleware.
func (WorkflowPushGuard) Name() string { return "workflow_push_guard" }

// ParsedPush is a recognised `git push`.
type ParsedPush struct {
	Dir      string // from -C or a preceding cd
	Remote   string
	Refspecs []string
	Force    bool
}

var pushFlagsWithValue = map[string]bool{"-o": true, "--push-option": true, "--repo": true, "--receive-pack": true, "--exec": true}

// ParsePushes finds the git pushes in a shell line. unsafe reports pushes that
// could not be understood (substitutions) and must be blocked.
func ParsePushes(line string) (pushes []ParsedPush, unsafe bool) {
	cmds, uns := Commands(line)
	cwd := ""
	for _, c := range cmds {
		switch c.Executable() {
		case "cd":
			if a := c.Args(); len(a) > 0 {
				cwd = joinDir(cwd, a[len(a)-1])
			}
			continue
		case "git":
		default:
			continue
		}
		args := c.Args()
		dir := cwd
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "-") { // git global options
			switch args[i] {
			case "-C":
				if i+1 < len(args) {
					dir = joinDir(dir, args[i+1])
				}
				i += 2
			case "-c", "--git-dir", "--work-tree", "--namespace":
				i += 2
			default:
				i++
			}
		}
		if i >= len(args) || args[i] != "push" {
			continue
		}
		p := ParsedPush{Dir: dir}
		var pos []string
		for j := i + 1; j < len(args); j++ {
			a := args[j]
			switch {
			case a == "--":
			case a == "-f" || a == "--force" || strings.HasPrefix(a, "--force-with-lease") || a == "--force-if-includes":
				p.Force = true
			case pushFlagsWithValue[a]:
				j++
			case strings.HasPrefix(a, "-"):
			default:
				pos = append(pos, a)
			}
		}
		if len(pos) > 0 {
			p.Remote = pos[0]
			p.Refspecs = pos[1:]
		}
		pushes = append(pushes, p)
	}
	return pushes, uns && strings.Contains(line, "push")
}

func joinDir(base, d string) string {
	if strings.HasPrefix(d, "/") || base == "" {
		return d
	}
	return path.Join(base, d)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (g WorkflowPushGuard) git(ctx context.Context, dir, args string) (string, error) {
	cmd := "git " + args
	if dir != "" {
		cmd = "cd " + quote(dir) + " && " + cmd
	}
	r, err := g.Exec.Execute(ctx, cmd, time.Minute)
	if err != nil {
		return "", err
	}
	if r.ExitCode != 0 {
		return "", fmt.Errorf("git %s: exit %d: %s", args, r.ExitCode, strings.TrimSpace(r.Output))
	}
	return strings.TrimSpace(r.Output), nil
}

var refNameSafe = func(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(r == '/' || r == '.' || r == '_' || r == '-' || r == '@' || r == '+' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// Inspect works out which workflow files the push would publish. It returns a
// zero PushInfo (empty ID) when none are touched.
func (g WorkflowPushGuard) Inspect(ctx context.Context, p ParsedPush) (PushInfo, error) {
	local, remoteBranch := "HEAD", ""
	if len(p.Refspecs) > 0 {
		spec := strings.TrimPrefix(p.Refspecs[0], "+")
		src, dst, hasColon := strings.Cut(spec, ":")
		if src != "" {
			local = src
		}
		remoteBranch = strings.TrimPrefix(map[bool]string{true: dst, false: src}[hasColon], "refs/heads/")
	}
	if !refNameSafe(local) || (remoteBranch != "" && !refNameSafe(remoteBranch)) {
		return PushInfo{}, fmt.Errorf("cannot inspect refspec %q safely", strings.Join(p.Refspecs, " "))
	}
	if remoteBranch == "" {
		b, err := g.git(ctx, p.Dir, "rev-parse --abbrev-ref HEAD")
		if err != nil {
			return PushInfo{}, err
		}
		remoteBranch = b
	}
	remote := p.Remote
	if remote == "" {
		remote = "origin"
	}
	if !refNameSafe(remote) {
		return PushInfo{}, fmt.Errorf("cannot inspect remote %q safely", remote)
	}
	head, err := g.git(ctx, p.Dir, "rev-parse --verify "+quote(local))
	if err != nil {
		return PushInfo{}, err
	}
	// what leaves the sandbox: commits not yet on the remote branch; for a new
	// branch, commits not reachable from any remote ref
	var rng string
	if _, err := g.git(ctx, p.Dir, "rev-parse --verify --quiet "+quote(remote+"/"+remoteBranch)); err == nil {
		rng = quote(remote+"/"+remoteBranch) + ".." + quote(local)
	} else {
		rng = quote(local) + " --not --remotes"
	}
	names, err := g.git(ctx, p.Dir, "log --name-only --pretty=format: "+rng)
	if err != nil {
		return PushInfo{}, err
	}
	seen := map[string]bool{}
	var files []string
	for _, n := range strings.Split(names, "\n") {
		n = strings.TrimSpace(n)
		if strings.HasPrefix(n, WorkflowDir) && !seen[n] {
			seen[n] = true
			files = append(files, n)
		}
	}
	if len(files) == 0 {
		return PushInfo{}, nil
	}
	sort.Strings(files)
	diff, _ := g.git(ctx, p.Dir, "log -p --pretty=format:%h\\ %s "+rng+" -- "+quote(strings.TrimSuffix(WorkflowDir, "/")))
	if len(diff) > 20000 {
		diff = diff[:20000] + "\n[diff truncated]"
	}
	top, _ := g.git(ctx, p.Dir, "rev-parse --show-toplevel")
	sum := sha256.Sum256([]byte(strings.Join([]string{top, head, strings.Join(files, ","), diff}, "\x00")))
	return PushInfo{ID: hex.EncodeToString(sum[:10]), Dir: top, Remote: remote, Branch: remoteBranch, HeadSHA: head, Files: files, DiffPreview: diff}, nil
}

// WrapToolCall implements agents.WrapTool.
func (g WorkflowPushGuard) WrapToolCall(ctx context.Context, call messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	st := g.ShellTools
	if st == nil {
		st = ShellTools
	}
	for _, line := range shellLines(call, st) {
		pushes, unsafe := ParsePushes(line)
		if unsafe {
			return errorMessage(call, "This command contains a substitution next to a git push, so the push cannot be inspected for workflow-file changes. Run the push as a plain `git push <remote> <branch>`. Nothing ran."), nil
		}
		for _, p := range pushes {
			info, err := g.Inspect(ctx, p)
			if err != nil {
				return errorMessage(call, "Cannot inspect this push for workflow-file changes ("+err.Error()+"). Push the current branch to the same-named branch on origin from a clean repository. Nothing ran."), nil
			}
			if info.ID == "" || (g.Approver != nil && g.Approver.Approved(ctx, info.ID)) {
				continue
			}
			if g.Approver != nil {
				if err := g.Approver.Request(ctx, info); err != nil {
					return errorMessage(call, "This push changes workflow files and needs approval, but the approval request failed: "+err.Error()+". Nothing ran."), nil
				}
			}
			return errorMessage(call, fmt.Sprintf(
				"This push changes GitHub workflow files (%s) and needs human approval before it can run. An approval request (id %s) was sent. Do not retry the push or work around this; tell the user what is waiting for approval, and push again after they approve. Nothing ran.",
				strings.Join(info.Files, ", "), info.ID)), nil
		}
	}
	return next(ctx, call)
}
