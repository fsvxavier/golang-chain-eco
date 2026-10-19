package guards

import (
	"context"
	"regexp"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

// ShellTools lists the tools whose arguments run as shell in the sandbox and
// which argument holds the command line.
var ShellTools = map[string][]string{
	"execute":            {"command"},
	"background_execute": {"command"},
	"monitor":            {"command", "filter"},
}

// shellLines returns every shell string a tool call would run.
func shellLines(call messages.ToolCall, shellTools map[string][]string) []string {
	var out []string
	for _, arg := range shellTools[call.Name] {
		if s, ok := call.Args[arg].(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func errorMessage(call messages.ToolCall, text string) messages.Message {
	m := messages.Tool(text, call.ID)
	m.Name, m.Status = call.Name, "error"
	return m
}

// PRCreationGuard blocks shell fallbacks that open pull requests outside the
// open_pull_request tool (`gh pr create`, `gh api .../pulls`, `curl` to the
// pulls endpoint, `hub pull-request`). The tool attributes the PR to the right
// person, adds references, applies the draft policy and is idempotent; a PR
// created by hand skips all of that. If the tool failed, the failure must be
// surfaced instead of worked around.
//
// This is a guard rail for an agent that tries shortcuts, not a security
// boundary: a script written to a file and then run is not inspected. Back it
// with credentials that cannot create PRs from the sandbox.
type PRCreationGuard struct {
	// ShellTools overrides ShellTools.
	ShellTools map[string][]string
}

// Name implements agents.Middleware.
func (PRCreationGuard) Name() string { return "pr_creation_guard" }

const prBlocked = "New pull requests must be opened with the open_pull_request tool so the PR is attributed to the triggering user. " +
	"If open_pull_request failed, report that failure instead of falling back to gh pr create, gh api /pulls, curl, or another direct PR creation path."

var pullsEndpoint = regexp.MustCompile(`(?:^|/)repos/[^/\s]+/[^/\s]+/pulls/?$`)
var pullsURL = regexp.MustCompile(`api\.github\.com/repos/[^/\s]+/[^/\s]+/pulls/?(?:$|[?#])`)

// CreatesPullRequest reports whether a shell line would open a PR.
func CreatesPullRequest(line string) bool {
	cmds, unsafe := Commands(line)
	for _, c := range cmds {
		if opensPR(c) {
			return true
		}
	}
	// $(...) / backticks hide the real command: only flag when the visible
	// text mentions a PR creation path at all
	if unsafe {
		l := strings.ToLower(line)
		return strings.Contains(l, "pr create") || strings.Contains(l, "/pulls")
	}
	return false
}

// ghValueFlags are gh flags that consume the next word.
var ghValueFlags = map[string]bool{"-X": true, "--method": true, "-H": true, "--header": true, "-f": true, "-F": true, "--field": true,
	"--raw-field": true, "--input": true, "-q": true, "--jq": true, "-t": true, "--template": true, "--hostname": true, "-R": true, "--repo": true, "--cache": true}

// positionals returns the non-flag words, skipping the values of flags that
// take one (so `gh -R o/r pr create` is pr/create, not o/r/pr/create).
func positionals(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case ghValueFlags[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			out = append(out, a)
		}
	}
	return out
}

func hasFlag(args []string, names ...string) (string, bool) {
	for i, a := range args {
		for _, n := range names {
			if a == n && i+1 < len(args) {
				return args[i+1], true
			}
			if strings.HasPrefix(n, "--") && strings.HasPrefix(a, n+"=") {
				return strings.TrimPrefix(a, n+"="), true
			}
		}
	}
	return "", false
}

func anyFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}

func opensPR(c Command) bool {
	args := c.Args()
	switch c.Executable() {
	case "gh":
		sub := positionals(args)
		if len(sub) >= 2 && sub[0] == "pr" && (sub[1] == "create" || sub[1] == "new") {
			return true
		}
		if len(sub) >= 2 && sub[0] == "api" {
			endpoint := sub[1]
			method, explicit := hasFlag(args, "-X", "--method")
			posts := (explicit && strings.EqualFold(method, "POST")) || (!explicit && anyFlag(args, "-f", "-F", "--field", "--raw-field", "--input"))
			return pullsEndpoint.MatchString(strings.TrimPrefix(endpoint, "/")) && posts
		}
	case "hub":
		return len(args) > 0 && (args[0] == "pull-request" || (len(args) > 1 && args[0] == "pr" && args[1] == "create"))
	case "curl", "wget", "http", "https":
		var url string
		for _, a := range args {
			if pullsURL.MatchString(a) {
				url = a
			}
		}
		if url == "" {
			return false
		}
		method, explicit := hasFlag(args, "-X", "--request", "--method")
		return (explicit && strings.EqualFold(method, "POST")) || anyFlag(args, "-d", "--data", "--data-raw", "--data-binary", "--json", "--post-data")
	}
	return false
}

// WrapToolCall implements agents.WrapTool.
func (g PRCreationGuard) WrapToolCall(ctx context.Context, call messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	st := g.ShellTools
	if st == nil {
		st = ShellTools
	}
	for _, line := range shellLines(call, st) {
		if CreatesPullRequest(line) {
			return errorMessage(call, prBlocked), nil
		}
	}
	return next(ctx, call)
}
