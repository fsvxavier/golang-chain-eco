// engineer: the Open SWE engineer end to end, offline. A scripted model plays
// an engineer fixing an issue: it edits files in the thread's own sandbox,
// tries the forbidden `gh pr create` shortcut (blocked by a guard), then
// delivers a draft pull request through the audited tool and reports back on
// the issue. A fake GitHub stands in for api.github.com.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/fakegithub"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
	"github.com/fsvxavier/golang-chain-eco/openswe/sandbox"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

func main() {
	ctx := context.Background()
	gh := fakegithub.New("acme", "api")
	defer gh.Close()
	gh.Pushed["openswe/fix-empty-user"] = true // pretend the engineer already pushed this branch

	base, _ := os.MkdirTemp("", "openswe-sandboxes-")
	defer os.RemoveAll(base)
	sb := &sandbox.Manager{Providers: map[string]sandbox.Provider{"local": sandbox.Local{Base: base}}, Default: "local", Store: store.NewMemory()}
	links := &openswe.PRLinks{S: store.NewMemory()}

	thread := triggers.ThreadID(triggers.KindAgent, "acme", "api", 12)
	box, err := sb.ForThread(ctx, thread)
	if err != nil {
		log.Fatal(err)
	}
	// the repository ships its own rules; they reach the prompt
	_, _ = box.Backend().Write(ctx, "/api/AGENTS.md", "Run `make lint` before every push. Tests live next to the code.")

	// the scripted "engineer": each reply is the next tool call
	script := []messages.Message{
		messages.AI("I'll look at the code first.", messages.ToolCall{ID: "1", Name: "write_file", Args: map[string]any{
			"file_path": "/api/auth/login.go", "content": "package auth\n\n// Login rejects empty users instead of crashing.\nfunc Login(u string) error {\n\tif u == \"\" {\n\t\treturn ErrEmpty\n\t}\n\treturn nil\n}\n"}}),
		messages.AI("Opening the PR the quick way.", messages.ToolCall{ID: "2", Name: "execute", Args: map[string]any{"command": "gh pr create --fill"}}),
		messages.AI("That was refused; using the proper tool.", messages.ToolCall{ID: "3", Name: "open_pull_request", Args: map[string]any{
			"owner": "acme", "repo": "api", "head": "openswe/fix-empty-user", "title": "fix(auth): reject empty users in Login",
			"body": "Login panicked on an empty user. It now returns ErrEmpty.\n\nCloses #12.", "resolves_thread": true}}),
		messages.AI("Reporting back.", messages.ToolCall{ID: "4", Name: "github_comment", Args: map[string]any{
			"owner": "acme", "repo": "api", "number": 12, "body": "Fixed in https://github.com/acme/api/pull/101 (draft; please review before marking it ready)."}}),
		messages.AI("Done: draft PR #101 opened."),
	}
	model := &models.Fake{Responses: script}

	agent, err := openswe.NewAgent(openswe.Config{
		Model: model, GitHub: gh.Client(), Sandbox: sb, Links: links, Checkpointer: checkpoint.NewMemory(),
		DraftPRs: true, AllowedOrgs: []string{"acme"}, DefaultRepo: "acme/api",
		ThreadURL: func(t string) string { return "https://openswe.example/threads/" + t },
	})
	if err != nil {
		log.Fatal(err)
	}

	demo.Title("run the engineer on GitHub issue acme/api#12")
	err = openswe.AgentRunner{Graph: agent}.Run(ctx, triggers.Task{
		Kind: triggers.KindAgent, ThreadID: thread, Source: "github", Owner: "acme", Repo: "api", Number: 12, Reason: "demo",
		Messages: []messages.Message{messages.Human("Fix issue #12: Login crashes on an empty user.")},
	})
	if err != nil {
		log.Fatal(err)
	}

	demo.Title("what the guard said to the shortcut")
	for _, m := range model.Calls[len(model.Calls)-1] {
		if m.Role == messages.RoleTool && m.ToolCallID == "2" {
			fmt.Println(strings.TrimSpace(m.Text()))
		}
	}
	demo.Title("what the system prompt was built from (excerpt)")
	sys := model.Calls[0][0].Text()
	for _, want := range []string{"You are Open SWE", "Run `make lint` before every push.", "new PRs are drafts", "acme"} {
		fmt.Printf("  contains %-40q %v\n", want, strings.Contains(sys, want))
	}

	demo.Title("result")
	b, _ := os.ReadFile(filepath.Join(box.Dir(), "api", "auth", "login.go"))
	fmt.Printf("file in the thread's sandbox (%s):\n%s", box.ID(), b)
	for _, p := range gh.PRs {
		fmt.Printf("\nPR #%d draft=%v head=%s base=%s\n---\n%s\n", p.Number, p.Draft, p.Head, p.Base, p.Body)
	}
	fmt.Println("\nissue comment:", gh.Comments[0])
	pr := github.PRKey{Owner: "acme", Repo: "api", Number: 101}
	owner, _ := links.ThreadFor(ctx, pr)
	fmt.Println("PR", pr, "belongs to thread", owner == thread)
}
