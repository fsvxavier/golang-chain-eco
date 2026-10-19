package openswe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/ciam"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
	"github.com/fsvxavier/golang-chain-eco/openswe/guards"
	"github.com/fsvxavier/golang-chain-eco/openswe/review"
	"github.com/fsvxavier/golang-chain-eco/openswe/sandbox"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

// fakeGH is a tiny stateful GitHub.
type fakeGH struct {
	mu          sync.Mutex
	prs         []map[string]any // open PRs created through the API
	prBodies    []map[string]any
	comments    []string
	reviews     []github.ReviewRequest
	pushed      map[string]bool
	diff        string
	head        string
	draft       bool
	commentSeq  int64
	reviewComms []map[string]any
}

const reviewDiff = `diff --git a/auth/login.go b/auth/login.go
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

func (g *fakeGH) server(t *testing.T) *github.Client {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		p := r.Method + " " + r.URL.Path
		if strings.HasPrefix(r.URL.Path, "/repos/") && !strings.HasPrefix(r.URL.Path, "/repos/acme/api") {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		switch {
		case p == "GET /repos/acme/api":
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case strings.HasPrefix(p, "GET /repos/acme/api/branches/"):
			name := strings.TrimPrefix(r.URL.Path, "/repos/acme/api/branches/")
			if g.pushed[name] {
				_, _ = w.Write([]byte(`{"name":"` + name + `"}`))
				return
			}
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Branch not found"}`))
		case p == "GET /repos/acme/api/pulls":
			_ = json.NewEncoder(w).Encode(g.prs)
		case p == "POST /repos/acme/api/pulls":
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			g.prBodies = append(g.prBodies, req)
			pr := map[string]any{"number": 31, "html_url": "https://github.com/acme/api/pull/31", "state": "open", "draft": req["draft"],
				"head": map[string]any{"ref": req["head"]}, "base": map[string]any{"ref": req["base"]}}
			g.prs = append(g.prs, pr)
			_ = json.NewEncoder(w).Encode(pr)
		case p == "GET /repos/acme/api/pulls/5":
			if strings.Contains(r.Header.Get("Accept"), "diff") {
				_, _ = w.Write([]byte(g.diff))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 5, "state": "open", "draft": g.draft, "html_url": "https://github.com/acme/api/pull/5",
				"head": map[string]any{"sha": g.head, "ref": "feat"}, "base": map[string]any{"ref": "main", "sha": "base0"}})
		case strings.HasSuffix(p, "/comments") && r.Method == "POST" && strings.Contains(r.URL.Path, "/issues/"):
			var b map[string]string
			_ = json.Unmarshal(body, &b)
			g.comments = append(g.comments, b["body"])
			g.commentSeq++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": g.commentSeq})
		case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/issues/comments/"):
			w.WriteHeader(204)
		case p == "POST /repos/acme/api/pulls/5/reviews":
			var rr github.ReviewRequest
			_ = json.Unmarshal(body, &rr)
			g.reviews = append(g.reviews, rr)
			for i, c := range rr.Comments {
				g.reviewComms = append(g.reviewComms, map[string]any{"id": 100 + i, "body": c.Body, "path": c.Path, "pull_request_review_id": 1, "user": map[string]any{"login": "bot", "type": "Bot"}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "state": "COMMENTED"})
		case p == "GET /repos/acme/api/pulls/5/comments":
			_ = json.NewEncoder(w).Encode(g.reviewComms)
		case p == "POST /graphql":
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`))
		case strings.HasPrefix(p, "GET /repos/acme/api/contents/"):
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		default:
			t.Errorf("unexpected GitHub call %s", p)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(ts.Close)
	c := github.NewClient(github.StaticToken("t"))
	c.BaseURL = ts.URL
	return c
}

// scripted builds a model from steps; each step sees the whole conversation.
type step func(msgs []messages.Message) messages.Message

func scripted(steps ...step) *models.Fake {
	i := 0
	var mu sync.Mutex
	return &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		mu.Lock()
		defer mu.Unlock()
		if i >= len(steps) {
			return messages.AI("done"), nil
		}
		s := steps[i]
		i++
		return s(msgs), nil
	}}
}

func call(id, name string, args map[string]any) messages.Message {
	return messages.AI("", messages.ToolCall{ID: id, Name: name, Args: args})
}

func toolResult(msgs []messages.Message, id string) string {
	for _, m := range msgs {
		if m.Role == messages.RoleTool && m.ToolCallID == id {
			return m.Text()
		}
	}
	return ""
}

func newSandbox(t *testing.T) *sandbox.Manager {
	return &sandbox.Manager{Providers: map[string]sandbox.Provider{"local": sandbox.Local{Base: t.TempDir()}}, Default: "local", Store: store.NewMemory()}
}

func TestEngineerDeliversAPullRequestThroughTheRules(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGH{pushed: map[string]bool{"openswe/fix-login": true}}
	client := gh.server(t)
	sb := newSandbox(t)
	links := &openswe.PRLinks{S: store.NewMemory()}
	thread := triggers.ThreadID(triggers.KindAgent, "acme", "api", 12)

	// the repository's AGENTS.md is part of the sandbox, so it must reach the prompt
	box, _ := sb.ForThread(ctx, thread)
	_, _ = box.Backend().Write(ctx, "/api/AGENTS.md", "Always run make lint before pushing.")

	var firstSystem string
	model := scripted(
		func(msgs []messages.Message) messages.Message {
			firstSystem = msgs[0].Text()
			return call("c1", "write_file", map[string]any{"file_path": "/api/fix.txt", "content": "fixed"})
		},
		func([]messages.Message) messages.Message {
			return call("c2", "execute", map[string]any{"command": "gh pr create --fill"}) // the shortcut the rules forbid
		},
		func([]messages.Message) messages.Message {
			return call("c3", "open_pull_request", map[string]any{"owner": "acme", "repo": "api", "head": "openswe/fix-login", "title": "Fix login crash",
				"body": "Fixes the crash on empty users.", "resolves_thread": true})
		},
		func([]messages.Message) messages.Message {
			return call("c4", "github_comment", map[string]any{"owner": "acme", "repo": "api", "number": 12, "body": "Opened https://github.com/acme/api/pull/31"})
		},
	)
	var got []messages.Message
	spy := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		got = msgs
		return model.Generate(ctx, msgs)
	}}
	agent, err := openswe.NewAgent(openswe.Config{Model: spy, GitHub: client, Sandbox: sb, Links: links, DraftPRs: true, AllowedOrgs: []string{"acme"},
		Attribution: "_Opened by Open SWE._", ThreadURL: func(th string) string { return "https://openswe.example/threads/" + th }, Checkpointer: checkpoint.NewMemory()})
	if err != nil {
		t.Fatal(err)
	}
	task := triggers.Task{Kind: triggers.KindAgent, ThreadID: thread, Source: "github", Owner: "acme", Repo: "api", Number: 12,
		Messages: []messages.Message{messages.Human("Fix the login crash from issue 12")}}
	if err := (openswe.AgentRunner{Graph: agent}).Run(ctx, task); err != nil {
		t.Fatal(err)
	}

	// the prompt was rendered for this run: source, AGENTS.md, org scope, rules
	for _, must := range []string{"You are Open SWE", "github_comment", "Always run make lint before pushing.", "acme", "Never approve pull requests as yourself", "new PRs are drafts"} {
		if !strings.Contains(firstSystem, must) {
			t.Errorf("system prompt lacks %q", must)
		}
	}
	// the work happened in this thread's sandbox
	if b, err := os.ReadFile(filepath.Join(box.Dir(), "api", "fix.txt")); err != nil || string(b) != "fixed" {
		t.Fatalf("%v %q", err, b)
	}
	// the shortcut was refused and the model was told why
	if res := toolResult(got, "c2"); !strings.Contains(res, "open_pull_request") {
		t.Fatalf("gh pr create must be blocked with guidance: %q", res)
	}
	// the PR: draft, with attribution and a link back to the thread, exactly once
	if len(gh.prBodies) != 1 {
		t.Fatalf("%d PRs opened", len(gh.prBodies))
	}
	pr := gh.prBodies[0]
	body, _ := pr["body"].(string)
	if pr["draft"] != true || pr["head"] != "openswe/fix-login" || pr["base"] != "main" || !strings.Contains(body, "Fixes the crash") ||
		!strings.Contains(body, "https://openswe.example/threads/"+thread) || !strings.Contains(body, "_Opened by Open SWE._") {
		t.Fatalf("%+v", pr)
	}
	// the PR is linked to the thread in both directions
	if th, _ := links.ThreadFor(ctx, github.PRKey{Owner: "acme", Repo: "api", Number: 31}); th != thread {
		t.Fatalf("thread for PR: %q", th)
	}
	if l, _ := links.ForThread(ctx, thread); len(l) != 1 || !l[0].ResolvesThread || l[0].URL != "https://github.com/acme/api/pull/31" {
		t.Fatalf("%+v", l)
	}
	if len(gh.comments) != 1 || !strings.Contains(gh.comments[0], "pull/31") {
		t.Fatalf("%v", gh.comments)
	}
}

func TestOpenPullRequestRefusesAnUnpushedBranchAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGH{pushed: map[string]bool{}}
	client := gh.server(t)
	tl := func() map[string]func(map[string]any) (any, error) {
		out := map[string]func(map[string]any) (any, error){}
		for _, x := range (openswe.PRTools{GitHub: client, Attribution: "_bot_"}).All() {
			x := x
			out[x.Name()] = func(a map[string]any) (any, error) { return x.Call(ctx, a) }
		}
		return out
	}()
	args := map[string]any{"owner": "acme", "repo": "api", "head": "feat", "title": "T", "body": "b"}
	if _, err := tl["open_pull_request"](args); err == nil || !strings.Contains(err.Error(), "Push it first") {
		t.Fatalf("an unpushed branch must be refused with the fix: %v", err)
	}
	gh.pushed["feat"] = true
	out1, err := tl["open_pull_request"](args)
	if err != nil || !strings.Contains(out1.(string), `"created":true`) {
		t.Fatalf("%v %v", out1, err)
	}
	out2, err := tl["open_pull_request"](args)
	if err != nil || !strings.Contains(out2.(string), `"created":false`) || !strings.Contains(out2.(string), "gh pr edit") || len(gh.prBodies) != 1 {
		t.Fatalf("a retry must not duplicate: %v %v (%d)", out2, err, len(gh.prBodies))
	}
	if _, err := tl["open_pull_request"](map[string]any{"owner": "acme"}); err == nil {
		t.Fatal("validation")
	}
	// access errors tell the agent not to improvise
	if _, err := tl["github_comment"](map[string]any{"owner": "acme", "repo": "gone", "number": 1, "body": "x"}); err == nil || !strings.Contains(err.Error(), "access error") || !strings.Contains(err.Error(), "Do not retry through gh pr create") {
		t.Fatalf("%v", err)
	}
	// drafts can never be sent to review (and the agent is told to leave undrafting to the author)
	gh.draft, gh.head = true, "h"
	if _, err := tl["request_pr_review"](map[string]any{"pr_url": "https://github.com/acme/api/pull/5"}); err == nil || !strings.Contains(err.Error(), "Ready for review") {
		t.Fatalf("%v", err)
	}
}

func TestReviewerPublishesOnceAndCannotWrite(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGH{diff: reviewDiff, head: "newsha", pushed: map[string]bool{}}
	client := gh.server(t)
	sb := newSandbox(t)
	reviews := review.NewStore(store.NewMemory())
	var pushResult, publishResult string
	findingID := func(msgs []messages.Message) string {
		var m map[string]any
		_ = json.Unmarshal([]byte(toolResult(msgs, "c3")), &m)
		id, _ := m["finding_id"].(string)
		return id
	}
	model := scripted(
		func([]messages.Message) messages.Message { return call("c1", "fetch_review_diff", map[string]any{}) },
		func(msgs []messages.Message) messages.Message {
			// the reviewer tries to push: refused
			return call("c2", "execute", map[string]any{"command": "git push origin HEAD"})
		},
		func(msgs []messages.Message) messages.Message {
			pushResult = toolResult(msgs, "c2")
			return call("c3", "add_finding", map[string]any{"severity": "high", "confidence": "high", "category": "security", "file": "auth/login.go",
				"title": "Token written to logs", "description": "The token from the environment is logged in clear text.", "start_line": 14, "end_line": 14})
		},
		func(msgs []messages.Message) messages.Message {
			return call("c4", "publish_review", map[string]any{"ranking": []any{findingID(msgs)}, "summary": "One security issue."})
		},
		func(msgs []messages.Message) messages.Message {
			publishResult = toolResult(msgs, "c4")
			return messages.AI("published")
		},
	)
	cfg := openswe.Config{Model: model, GitHub: client, Sandbox: sb}
	rr := openswe.ReviewerRunner{Config: cfg, Reviews: reviews, Styles: &review.StyleStore{S: store.NewMemory()}, Announce: true}
	task := triggers.Task{Kind: triggers.KindReviewer, ThreadID: triggers.ThreadID(triggers.KindReviewer, "acme", "api", 5), Source: "github", Owner: "acme", Repo: "api", Number: 5}
	if err := rr.Run(ctx, task); err != nil {
		t.Fatalf("%v (publish_review said: %s)", err, publishResult)
	}
	if !strings.Contains(pushResult, "read-only") {
		t.Fatalf("the reviewer must not push: %q", pushResult)
	}
	if len(gh.reviews) != 1 || gh.reviews[0].CommitID != "newsha" || gh.reviews[0].Event != "COMMENT" || len(gh.reviews[0].Comments) != 1 {
		t.Fatalf("%+v", gh.reviews)
	}
	c := gh.reviews[0].Comments[0]
	if c.Path != "auth/login.go" || c.Line != 14 || !strings.Contains(c.Body, "Token written to logs") {
		t.Fatalf("%+v", c)
	}
	key := github.PRKey{Owner: "acme", Repo: "api", Number: 5}
	st, _ := reviews.State(ctx, key)
	fs, _ := reviews.List(ctx, key)
	if st.LastReviewedSHA != "newsha" || len(fs) != 1 || fs[0].Surface != review.Surfaced || fs[0].CommentID == 0 {
		t.Fatalf("%+v %+v", st, fs)
	}
	// the "review in progress" note was posted and removed (best effort)
	if len(gh.comments) != 1 || !strings.Contains(gh.comments[0], "in progress") {
		t.Fatalf("%v", gh.comments)
	}
	// the diff file landed in the sandbox for grepping
	box, _ := sb.ForThread(ctx, task.ThreadID)
	if b, err := os.ReadFile(filepath.Join(box.Dir(), "review", "pr-5.diff")); err != nil || !strings.Contains(string(b), "log.Println") {
		t.Fatalf("%v", err)
	}

	// a reviewer that ends without publishing is an error, not a silent success
	lazy := openswe.ReviewerRunner{Config: openswe.Config{Model: scripted(func([]messages.Message) messages.Message { return messages.AI("looks fine to me") }), GitHub: client, Sandbox: newSandbox(t)},
		Reviews: review.NewStore(store.NewMemory())}
	if err := lazy.Run(ctx, task); err == nil || err != openswe.ErrNoReview {
		t.Fatalf("%v", err)
	}
}

func TestMentionToRunWithFollowUps(t *testing.T) {
	gh := &fakeGH{pushed: map[string]bool{}}
	client := gh.server(t)
	q := &guards.MemoryQueue{}
	block := make(chan struct{})
	var seen [][]string
	step := 0
	var mu sync.Mutex
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		mu.Lock()
		step++
		n := step
		var texts []string
		for _, m := range msgs {
			if m.Role == messages.RoleHuman {
				texts = append(texts, m.Text())
			}
		}
		seen = append(seen, texts)
		mu.Unlock()
		if n == 1 {
			<-block // the run is busy while the follow-up arrives
			return call("c1", "github_comment", map[string]any{"owner": "acme", "repo": "api", "number": 12, "body": "working on it"}), nil
		}
		return messages.AI("done"), nil
	}}
	agent, err := openswe.NewAgent(openswe.Config{Model: model, GitHub: client, Sandbox: newSandbox(t), Queue: q})
	if err != nil {
		t.Fatal(err)
	}
	d := &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: openswe.AgentRunner{Graph: agent}}, Queue: q}
	wh := &triggers.Webhook{Secret: "s", Dispatcher: d, Bot: "openswe", BotLogins: []string{"openswe[bot]"}, Claims: &triggers.MemoryClaims{}}
	ts := httptest.NewServer(wh)
	defer ts.Close()
	send := func(delivery, text string) {
		payload, _ := json.Marshal(map[string]any{"action": "created", "repository": map[string]any{"name": "api", "owner": map[string]any{"login": "acme"}},
			"sender": map[string]any{"login": "ana", "type": "User"}, "issue": map[string]any{"number": 12, "title": "Crash", "user": map[string]any{"login": "ana"}},
			"comment": map[string]any{"id": 1, "body": text, "user": map[string]any{"login": "ana", "type": "User"}}})
		req, _ := http.NewRequest("POST", ts.URL, strings.NewReader(string(payload)))
		req.Header.Set("X-GitHub-Event", "issue_comment")
		req.Header.Set("X-GitHub-Delivery", delivery)
		req.Header.Set("X-Hub-Signature-256", github.Sign("s", payload))
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 202 {
			t.Fatalf("%v %v", err, resp)
		}
	}
	send("d1", "@openswe fix the crash")
	wh.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for !d.Busy(triggers.ThreadID(triggers.KindAgent, "acme", "api", 12)) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	send("d2", "@openswe also add a regression test")
	wh.Wait()
	close(block)
	d.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 {
		t.Fatalf("%d model calls", len(seen))
	}
	first := strings.Join(seen[0], "\n")
	if !strings.Contains(first, "fix the crash") || !strings.Contains(first, "<untrusted_comment author=\"ana\">") {
		t.Fatalf("the mention must reach the agent, fenced as untrusted:\n%s", first)
	}
	later := strings.Join(seen[len(seen)-1], "\n")
	if !strings.Contains(later, "also add a regression test") {
		t.Fatalf("the follow-up sent while the run was busy must reach the same run:\n%s", later)
	}
	if len(gh.comments) != 1 {
		t.Fatalf("%v", gh.comments)
	}
}

func TestAgentCanUseTheContextManager(t *testing.T) {
	ctx := context.Background()
	gh := &fakeGH{pushed: map[string]bool{}}
	root := filepath.Join(t.TempDir(), "api")
	_ = os.MkdirAll(filepath.Join(root, "auth"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "auth", "login.go"), []byte("package auth\n// Login validates the JWT of a user.\nfunc Login() {}\n"), 0o644)
	docs := ciam.NewDocs(root)
	if _, err := docs.New(ciam.ADR, "Use JWT for sessions", ""); err != nil {
		t.Fatal(err)
	}
	mgr := ciam.New(embeddings.Fake{}, nil)
	if _, err := mgr.Index(ctx, root, ciam.IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	var system string
	var sawTool bool
	model := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		for _, d := range o.Tools {
			sawTool = sawTool || d.Name == "ciam_decision_context"
		}
		if r := toolResult(msgs, "c1"); r != "" {
			if !strings.Contains(r, "Use JWT for sessions") || !strings.Contains(r, "func Login()") {
				t.Errorf("decision context must bring code AND the ADR: %s", r)
			}
			return messages.AI("done"), nil
		}
		system = msgs[0].Text()
		return call("c1", "ciam_decision_context", map[string]any{"query": "JWT login sessions"}), nil
	}}
	agent, err := openswe.NewAgent(openswe.Config{Model: model, GitHub: gh.server(t), Sandbox: newSandbox(t), Context: &ciam.Toolkit{M: mgr, Project: root}})
	if err != nil {
		t.Fatal(err)
	}
	err = openswe.AgentRunner{Graph: agent}.Run(ctx, triggers.Task{ThreadID: "ctx-thread", Source: "cli", Messages: []messages.Message{messages.Human("add logout")}})
	if err != nil || !sawTool || !strings.Contains(system, "ciam_decision_context") {
		t.Fatalf("%v tool=%v", err, sawTool)
	}
}
