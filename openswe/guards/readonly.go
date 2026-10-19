package guards

import (
	"context"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/prebuilt"
)

// ReadOnlyGuard keeps a reviewer (or a PR-chat agent) from changing anything
// through the shell: no commits or pushes, no GitHub writes through gh, curl
// or the API (reviews, approvals, comments, merges, label edits). Reviews go
// out through the review tools, which validate and record them.
//
// Like PRCreationGuard this is a guard rail, not a sandbox: pair it with a
// token that has read-only scopes.
type ReadOnlyGuard struct {
	// ShellTools overrides ShellTools.
	ShellTools map[string][]string
}

// Name implements agents.Middleware.
func (ReadOnlyGuard) Name() string { return "read_only_guard" }

var gitWrites = map[string]bool{"commit": true, "push": true, "merge": true, "rebase": true, "cherry-pick": true, "am": true, "apply": true,
	"revert": true, "tag": true, "branch": false, "reset": true, "clean": true, "stash": true, "rm": true, "mv": true, "add": true, "restore": true, "checkout": false}

var ghMutating = map[string]map[string]bool{
	"pr":       {"create": true, "new": true, "review": true, "merge": true, "close": true, "reopen": true, "ready": true, "edit": true, "comment": true, "update-branch": true, "lock": true, "unlock": true},
	"issue":    {"create": true, "new": true, "close": true, "reopen": true, "edit": true, "comment": true, "delete": true, "lock": true, "unlock": true, "transfer": true, "pin": true, "unpin": true},
	"release":  {"create": true, "delete": true, "edit": true, "upload": true},
	"repo":     {"create": true, "delete": true, "edit": true, "fork": true, "rename": true, "archive": true, "sync": true},
	"workflow": {"run": true, "enable": true, "disable": true},
	"run":      {"rerun": true, "cancel": true, "delete": true},
	"label":    {"create": true, "edit": true, "delete": true, "clone": true},
}

// Mutation describes why a shell line is not read-only ("" when it is).
func Mutation(line string) string {
	cmds, unsafe := Commands(line)
	for _, c := range cmds {
		args := c.Args()
		switch c.Executable() {
		case "git":
			i := 0
			for i < len(args) && strings.HasPrefix(args[i], "-") {
				if args[i] == "-C" || args[i] == "-c" {
					i++
				}
				i++
			}
			if i < len(args) && gitWrites[args[i]] {
				return "git " + args[i]
			}
		case "gh":
			pos := positionals(args)
			if len(pos) >= 2 {
				if ghMutating[pos[0]][pos[1]] {
					return "gh " + pos[0] + " " + pos[1]
				}
				if pos[0] == "api" {
					method, explicit := hasFlag(args, "-X", "--method")
					if (explicit && !strings.EqualFold(method, "GET")) || (!explicit && anyFlag(args, "-f", "-F", "--field", "--raw-field", "--input")) {
						return "gh api with a write method"
					}
				}
			}
		case "curl", "wget", "http", "https":
			for _, a := range args {
				if strings.Contains(a, "api.github.com") || strings.Contains(a, "github.com/") {
					method, explicit := hasFlag(args, "-X", "--request", "--method")
					if (explicit && !strings.EqualFold(method, "GET")) || anyFlag(args, "-d", "--data", "--data-raw", "--data-binary", "--json", "--post-data", "-T", "--upload-file") {
						return "a write request to GitHub"
					}
				}
			}
		}
	}
	if unsafe && (strings.Contains(line, "git push") || strings.Contains(line, "git commit")) {
		return "a command substitution next to a git write"
	}
	return ""
}

// WrapToolCall implements agents.WrapTool.
func (g ReadOnlyGuard) WrapToolCall(ctx context.Context, call messages.ToolCall, next prebuilt.ToolCaller) (messages.Message, error) {
	st := g.ShellTools
	if st == nil {
		st = ShellTools
	}
	for _, line := range shellLines(call, st) {
		if why := Mutation(line); why != "" {
			return errorMessage(call, "This agent is read-only: "+why+" is not allowed. Do not commit, push, or write to GitHub through the shell; use the provided tools (add_finding, publish_review). Nothing ran."), nil
		}
	}
	return next(ctx, call)
}
