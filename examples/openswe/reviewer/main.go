// reviewer: the Open SWE pull-request reviewer, offline. A scripted model
// reviews a PR: it records a finding anchored to the diff, tries to push
// (refused: reviews are read-only), and publishes ONE review. Then the author
// pushes a fix and a human replies; the re-review reconciles the old finding
// instead of starting over.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/fakegithub"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
	"github.com/fsvxavier/golang-chain-eco/openswe/review"
	"github.com/fsvxavier/golang-chain-eco/openswe/sandbox"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

const diff = `diff --git a/auth/login.go b/auth/login.go
--- a/auth/login.go
+++ b/auth/login.go
@@ -10,6 +10,8 @@ func Login(u string) error {
 	if u == "" {
 		return ErrEmpty
 	}
+	token := os.Getenv("TOKEN")
+	log.Println("token", token)
 	return check(u)
 }
`

func tool(id, name string, args map[string]any) messages.Message {
	return messages.AI("", messages.ToolCall{ID: id, Name: name, Args: args})
}

func lastToolJSON(msgs []messages.Message, id string) map[string]any {
	for _, m := range msgs {
		if m.Role == messages.RoleTool && m.ToolCallID == id {
			var out map[string]any
			_ = json.Unmarshal([]byte(m.Text()), &out)
			return out
		}
	}
	return nil
}

func main() {
	ctx := context.Background()
	gh := fakegithub.New("acme", "api")
	defer gh.Close()
	gh.AddPR(fakegithub.PR{Number: 5, Title: "Add login logging", Head: "feat", HeadSHA: "sha1", Diff: diff})

	base, _ := os.MkdirTemp("", "openswe-review-")
	defer os.RemoveAll(base)
	sb := &sandbox.Manager{Providers: map[string]sandbox.Provider{"local": sandbox.Local{Base: base}}, Default: "local", Store: store.NewMemory()}
	reviews := review.NewStore(store.NewMemory())
	styles := &review.StyleStore{S: store.NewMemory()}
	_ = styles.Put(ctx, review.Style{FullName: "acme/api", Enabled: true, CustomPrompt: "Secrets must never reach logs.", Approval: review.ApprovalDryRun})
	key := github.PRKey{Owner: "acme", Repo: "api", Number: 5}
	task := triggers.Task{Kind: triggers.KindReviewer, ThreadID: triggers.ThreadID(triggers.KindReviewer, "acme", "api", 5), Source: "github", Owner: "acme", Repo: "api", Number: 5}

	// ---- round 1: first review
	var pushAnswer string
	round1 := &models.Fake{}
	steps1 := []func([]messages.Message) messages.Message{
		func([]messages.Message) messages.Message { return tool("1", "fetch_review_diff", map[string]any{}) },
		func([]messages.Message) messages.Message {
			return tool("2", "execute", map[string]any{"command": "git push origin HEAD"})
		},
		func(m []messages.Message) messages.Message {
			for _, x := range m {
				if x.ToolCallID == "2" {
					pushAnswer = strings.TrimSpace(x.Text())
				}
			}
			return tool("3", "add_finding", map[string]any{"severity": "high", "confidence": "high", "category": "security", "file": "auth/login.go",
				"title": "Token written to the log", "description": "The token read from the environment is printed in clear text; anyone with log access can use it.",
				"start_line": 14, "end_line": 14, "suggestion": "log.Println(\"token loaded\")"})
		},
		func(m []messages.Message) messages.Message {
			id, _ := lastToolJSON(m, "3")["finding_id"].(string)
			return tool("4", "publish_review", map[string]any{"ranking": []any{id}, "summary": "One security issue to fix before merging.",
				"assessment": map[string]any{"approve": false, "reason": "secret in logs"}})
		},
		func([]messages.Message) messages.Message { return messages.AI("Review published.") },
	}
	round1.Func = scripted(steps1)
	cfg := openswe.Config{Model: round1, GitHub: gh.Client(), Sandbox: sb}
	rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: styles}

	demo.Title("round 1: first review of acme/api#5")
	if err := rr.Run(ctx, task); err != nil {
		log.Fatal(err)
	}
	fmt.Println("push attempt ->", pushAnswer)
	rv := gh.Reviews[0]
	fmt.Printf("posted review: event=%s commit=%s comments=%d\n%s\n", rv.Event, rv.CommitID, len(rv.Comments), rv.Body)
	fmt.Printf("inline comment on %s:%d:\n%s\n", rv.Comments[0].Path, rv.Comments[0].Line, rv.Comments[0].Body)

	fs, _ := reviews.List(ctx, key)
	fmt.Printf("\nfinding %s: status=%s surface=%s linked to GitHub comment %d\n", fs[0].ID, fs[0].Status, fs[0].Surface, fs[0].CommentID)

	// ---- the world moves: the author fixes the code and a human replies on the thread
	gh.Mu.Lock()
	gh.PRs[5].HeadSHA = "sha2"
	gh.PRs[5].Diff = strings.Replace(diff, "+\tlog.Println(\"token\", token)\n", "+\tlog.Println(\"token loaded\")\n", 1)
	gh.ReviewComments = append(gh.ReviewComments, map[string]any{"id": 77, "body": "Good catch, fixed in the last commit.", "in_reply_to_id": fs[0].CommentID,
		"user": map[string]any{"login": "ana", "type": "User"}})
	gh.Threads[0]["comments"] = map[string]any{"nodes": []map[string]any{{"databaseId": fs[0].CommentID}, {"databaseId": 77}}}
	gh.Mu.Unlock()

	// ---- round 2: re-review (a push on a watched PR)
	round2 := &models.Fake{}
	var prompt2 string
	steps2 := []func([]messages.Message) messages.Message{
		func(m []messages.Message) messages.Message {
			prompt2 = m[0].Text()
			return tool("1", "list_findings", map[string]any{})
		},
		func([]messages.Message) messages.Message {
			return tool("2", "resolve_finding_thread", map[string]any{"finding_id": fs[0].ID, "status": "resolved", "note": "Confirmed: the token is no longer logged. Thanks!"})
		},
		func([]messages.Message) messages.Message {
			return tool("3", "publish_review", map[string]any{"ranking": []any{}, "summary": "Previous finding fixed."})
		},
		func([]messages.Message) messages.Message { return messages.AI("Re-review done.") },
	}
	round2.Func = scripted(steps2)
	rr2 := openswe.ReviewerRunner{Config: openswe.Config{Model: round2, GitHub: gh.Client(), Sandbox: sb}, Reviews: reviews, Styles: styles}
	t2 := task
	t2.Metadata = map[string]any{"re_review": true, "head_sha": "sha2"}

	demo.Title("round 2: re-review after a push and a human reply")
	if err := rr2.Run(ctx, t2); err != nil && err != openswe.ErrNoReview {
		log.Fatal(err)
	}
	fmt.Println("re-review prompt mentions:")
	for _, s := range []string{"RE-REVIEW", "New human reply on finding", "Good catch, fixed in the last commit.", "<untrusted_reply"} {
		fmt.Printf("  %-45q %v\n", s, strings.Contains(prompt2, s))
	}
	fs, _ = reviews.List(ctx, key)
	fmt.Printf("finding is now %s; reply posted on the thread: %q\n", fs[0].Status, gh.Comments)
	fmt.Println("reviews posted in total:", len(gh.Reviews), "(an empty re-review is skipped, not spammed)")
}

func scripted(steps []func([]messages.Message) messages.Message) func([]messages.Message, models.Options) (messages.Message, error) {
	i := 0
	return func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		if i >= len(steps) {
			return messages.AI("done"), nil
		}
		s := steps[i]
		i++
		return s(msgs), nil
	}
}
