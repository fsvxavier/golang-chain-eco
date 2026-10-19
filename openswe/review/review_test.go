package review_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
	"github.com/fsvxavier/golang-chain-eco/openswe/review"
)

const sampleDiff = `diff --git a/auth/login.go b/auth/login.go
index 111..222 100644
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
 
@@ -40,3 +42,4 @@ func check(u string) error {
-	return nil
+	if len(u) > 100 { return ErrLong }
+	return nil
 }
diff --git a/old.go b/new.go
similarity index 90%
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -1,2 +1,2 @@
-package old
+package newpkg
 // end
diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package gone
-func X() {}
\ No newline at end of file
`

func TestParseDiffAnchors(t *testing.T) {
	d := review.ParseDiff(sampleDiff)
	if got := strings.Join(d.Paths(), ","); got != "auth/login.go,new.go,gone.go" {
		t.Fatalf("paths %s", got)
	}
	// RIGHT: context and added lines of the first hunk are commentable (10..17), line 9 and 18 are not
	for _, ln := range []int{10, 12, 13, 14, 15, 17} {
		if !d.Contains("auth/login.go", ln, ln, "RIGHT") {
			t.Errorf("line %d must be in the diff", ln)
		}
	}
	for _, ln := range []int{1, 9, 18, 41} {
		if d.Contains("auth/login.go", ln, ln, "RIGHT") {
			t.Errorf("line %d must NOT be in the diff", ln)
		}
	}
	if !d.Contains("auth/login.go", 13, 14, "RIGHT") || d.Contains("auth/login.go", 17, 42, "RIGHT") {
		t.Fatal("a range must lie entirely inside the diff")
	}
	// second hunk: the + lines are 42,43 in the new file; the removed line is LEFT 40
	if !d.Contains("auth/login.go", 42, 43, "RIGHT") || !d.Contains("auth/login.go", 40, 40, "LEFT") || d.Contains("auth/login.go", 40, 40, "RIGHT") && !d.Contains("auth/login.go", 42, 42, "RIGHT") {
		t.Fatal("second hunk anchors")
	}
	if !d.Contains("new.go", 1, 1, "RIGHT") || d.Contains("old.go", 1, 1, "RIGHT") {
		t.Fatal("renames use the new path")
	}
	if d.Contains("gone.go", 1, 1, "RIGHT") || !d.Contains("gone.go", 1, 2, "LEFT") || !d.Files["gone.go"].Deleted {
		t.Fatal("deleted files only have LEFT lines")
	}
	if d.Contains("missing.go", 1, 1, "RIGHT") || d.Contains("auth/login.go", 0, 1, "RIGHT") || d.Contains("auth/login.go", 5, 4, "RIGHT") {
		t.Fatal("invalid ranges")
	}
	if h := d.HunkFor("auth/login.go", 13); !strings.HasPrefix(h, "@@ -10,6 +10,8 @@") || !strings.Contains(h, "log.Println") {
		t.Fatalf("hunk %q", h)
	}
	if files, added := d.Stats(); files != 3 || added != 5 {
		t.Fatalf("%d %d", files, added)
	}
}

func TestFindingHelpers(t *testing.T) {
	if review.NormalizeTitle("", "  First line of the description\nsecond") != "First line of the description" {
		t.Fatal("fallback to description")
	}
	if review.NormalizeTitle("   ", "") != review.DefaultFindingTitle {
		t.Fatal("default")
	}
	long := review.NormalizeTitle(strings.Repeat("word ", 50), "")
	if len([]rune(long)) != review.MaxTitleLength || !strings.HasSuffix(long, "...") {
		t.Fatalf("%d %q", len(long), long)
	}
	if k, dropped := review.ClipSuggestion("a\nb\nc\nd"); k == "" || dropped {
		t.Fatal("4 lines are fine")
	}
	if k, dropped := review.ClipSuggestion("a\nb\nc\nd\ne"); k != "" || !dropped {
		t.Fatal("5 lines must be dropped")
	}
	// fingerprint ignores line numbers and spacing, not the problem
	if review.Fingerprint("a.go", "bug", "Nil  deref") != review.Fingerprint("a.go", "bug", "nil deref") || review.Fingerprint("a.go", "bug", "x") == review.Fingerprint("b.go", "bug", "x") {
		t.Fatal("fingerprint")
	}
	if id, ok := review.ParseMarker("text\n" + review.Marker("abc-123") + "\n"); !ok || id != "abc-123" {
		t.Fatal("marker")
	}
	if _, ok := review.ParseMarker("no marker"); ok {
		t.Fatal("no marker")
	}
	if review.Critical.Rank() <= review.High.Rank() || review.Severity("x").Rank() != -1 {
		t.Fatal("rank")
	}
}

// ------------------------------------------------------------- fake GitHub

type ghFake struct {
	mu        sync.Mutex
	reviews   []github.ReviewRequest
	comments  []map[string]any // inline comments as GitHub would list them
	threads   []map[string]any
	replies   []string
	resolved  []string
	headSHA   string
	nextID    int64
	reviewIDs int64
}

func (g *ghFake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/o/r/pulls/5":
			if strings.Contains(r.Header.Get("Accept"), "diff") {
				_, _ = w.Write([]byte(sampleDiff))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 5, "head": map[string]any{"sha": g.headSHA}})
		case "GET /repos/o/r/compare/old...new":
			_, _ = w.Write([]byte(sampleDiff))
		case "POST /repos/o/r/pulls/5/reviews":
			var rr github.ReviewRequest
			_ = json.Unmarshal(body, &rr)
			g.reviews = append(g.reviews, rr)
			g.reviewIDs++
			for _, c := range rr.Comments {
				g.nextID++
				g.comments = append(g.comments, map[string]any{"id": g.nextID, "body": c.Body, "path": c.Path, "pull_request_review_id": g.reviewIDs, "user": map[string]any{"login": "openswe[bot]", "type": "Bot"}})
				g.threads = append(g.threads, map[string]any{"id": "T" + string(rune('0'+g.nextID)), "isResolved": false, "isOutdated": false,
					"comments": map[string]any{"nodes": []map[string]any{{"databaseId": g.nextID}}}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": g.reviewIDs, "state": "COMMENTED"})
		case "GET /repos/o/r/pulls/5/comments":
			_ = json.NewEncoder(w).Encode(g.comments)
		case "POST /graphql":
			var req struct {
				Query     string
				Variables map[string]any
			}
			_ = json.Unmarshal(body, &req)
			if strings.Contains(req.Query, "resolveReviewThread") {
				g.resolved = append(g.resolved, req.Variables["id"].(string))
				_, _ = w.Write([]byte(`{"data":{}}`))
				return
			}
			out, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewThreads": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false}, "nodes": g.threads}}}}})
			_, _ = w.Write(out)
		default:
			if strings.HasPrefix(r.URL.Path, "/repos/o/r/pulls/5/comments/") && strings.HasSuffix(r.URL.Path, "/replies") {
				var b map[string]string
				_ = json.Unmarshal(body, &b)
				g.replies = append(g.replies, b["body"])
				_, _ = w.Write([]byte(`{"id": 999}`))
				return
			}
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
}

func newSession(t *testing.T, g *ghFake) *review.Session {
	ts := httptest.NewServer(g.handler(t))
	t.Cleanup(ts.Close)
	c := github.NewClient(github.StaticToken("t"))
	c.BaseURL = ts.URL
	return &review.Session{Repo: c.Repo("o", "r"), PR: github.PRKey{Owner: "o", Repo: "r", Number: 5}, HeadSHA: "new",
		Findings: review.NewStore(store.NewMemory())}
}

func call(t *testing.T, s *review.Session, name string, args map[string]any) map[string]any {
	t.Helper()
	for _, tl := range s.Tools() {
		if tl.Name() != name {
			continue
		}
		raw, _ := json.Marshal(args) // what a model would send: plain JSON types
		var jargs map[string]any
		_ = json.Unmarshal(raw, &jargs)
		out, err := tl.Call(context.Background(), jargs)
		if tools.IsToolError(err) {
			return map[string]any{"success": false, "error": err.Error()}
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var m map[string]any
		switch v := out.(type) {
		case map[string]any:
			return v
		case string:
			if json.Unmarshal([]byte(v), &m) != nil {
				return map[string]any{"text": v}
			}
		}
		return m
	}
	t.Fatalf("no tool %s", name)
	return nil
}

func finding(file string, line int, title string) map[string]any {
	return map[string]any{"severity": "high", "confidence": "high", "category": "security", "file": file, "title": title,
		"description": "Logging a secret leaks it.", "start_line": line, "end_line": line}
}

func TestAddFindingValidatesAnchorsAndDeduplicates(t *testing.T) {
	g := &ghFake{headSHA: "new"}
	s := newSession(t, g)
	// out of diff: rejected, nothing stored
	if r := call(t, s, "add_finding", finding("auth/login.go", 30, "Out of diff")); r["success"] != false || r["in_diff"] != false {
		t.Fatalf("%v", r)
	}
	if r := call(t, s, "add_finding", finding("nope.go", 1, "Unknown file")); r["success"] != false {
		t.Fatalf("%v", r)
	}
	ok := call(t, s, "add_finding", finding("auth/login.go", 14, "Secret logged"))
	if ok["success"] != true || ok["duplicate"] != false {
		t.Fatalf("%v", ok)
	}
	// same problem on a shifted line: merged, not duplicated
	dup := call(t, s, "add_finding", finding("auth/login.go", 15, "secret  logged"))
	if dup["duplicate"] != true || dup["finding_id"] != ok["finding_id"] {
		t.Fatalf("%v", dup)
	}
	// field validation
	bad := finding("auth/login.go", 14, "x")
	bad["severity"] = "catastrophic"
	if r := call(t, s, "add_finding", bad); r["success"] != false || !strings.Contains(r["error"].(string), "severity") {
		t.Fatalf("%v", r)
	}
	bad = finding("auth/login.go", 14, "")
	if r := call(t, s, "add_finding", bad); r["success"] != false {
		t.Fatalf("empty title must fail: %v", r)
	}
	bad = finding("auth/login.go", 14, "y")
	bad["start_line"], bad["end_line"] = 15, 14
	if r := call(t, s, "add_finding", bad); r["success"] != false {
		t.Fatalf("%v", r)
	}
	// long suggestions are dropped but the finding is kept
	long := finding("auth/login.go", 13, "Use constant")
	long["suggestion"] = "1\n2\n3\n4\n5"
	if r := call(t, s, "add_finding", long); r["success"] != true || r["suggestion_dropped"] != true {
		t.Fatalf("%v", r)
	}
	all, _ := s.Findings.List(context.Background(), s.PR)
	if len(all) != 2 || all[0].DiffHunk == "" || all[0].Fingerprint == "" || all[0].Surface != review.NotSurfaced || all[1].Suggestion != "" {
		t.Fatalf("%+v", all)
	}
	// a single line given as only start_line is accepted
	one := finding("auth/login.go", 12, "Single line")
	delete(one, "end_line")
	if r := call(t, s, "add_finding", one); r["success"] != true {
		t.Fatalf("%v", r)
	}
}

func TestPublishFlow(t *testing.T) {
	g := &ghFake{headSHA: "new"}
	s := newSession(t, g)
	a := call(t, s, "add_finding", finding("auth/login.go", 14, "Secret logged"))["finding_id"].(string)
	bMap := finding("auth/login.go", 42, "Missing test")
	bMap["severity"] = "low"
	bMap["suggestion"] = "return ErrLong"
	b := call(t, s, "add_finding", bMap)["finding_id"].(string)

	// ranking must cover exactly the new open findings
	r := call(t, s, "publish_review", map[string]any{"ranking": []string{a}})
	if exp, _ := r["expected_finding_ids"].([]any); len(exp) != 2 || len(g.reviews) != 0 {
		t.Fatalf("%v", r)
	}
	r = call(t, s, "publish_review", map[string]any{"ranking": []string{b, a}, "summary": "Two things to look at."})
	if r["review_id"] == nil || r["event"] != "COMMENT" || r["posted"] != float64(2) {
		t.Fatalf("%v", r)
	}
	rev := g.reviews[0]
	if rev.CommitID != "new" || rev.Event != "COMMENT" || len(rev.Comments) != 2 || rev.Comments[0].Path != "auth/login.go" {
		t.Fatalf("%+v", rev)
	}
	if !strings.Contains(rev.Body, "1 high, 1 low") || !strings.Contains(rev.Body, "Two things to look at.") {
		t.Fatalf("body: %s", rev.Body)
	}
	// ranked order is respected: b (low) was listed first
	if !strings.Contains(rev.Comments[0].Body, "Missing test") || !strings.Contains(rev.Comments[0].Body, "```suggestion\nreturn ErrLong\n```") {
		t.Fatalf("%s", rev.Comments[0].Body)
	}
	if id, ok := review.ParseMarker(rev.Comments[1].Body); !ok || id != a {
		t.Fatal("marker missing")
	}
	// findings are now surfaced and linked to their GitHub comments/threads
	fa, _ := s.Findings.Get(context.Background(), s.PR, a)
	if fa.Surface != review.Surfaced || fa.ReviewID != 1 || fa.CommentID == 0 || fa.ThreadID == "" {
		t.Fatalf("%+v", fa)
	}
	st, _ := s.Findings.State(context.Background(), s.PR)
	if st.LastReviewedSHA != "new" || len(st.Reviews) != 1 {
		t.Fatalf("%+v", st)
	}
	// publishing again with nothing new on a first review posts nothing new but is not an error
	again := call(t, s, "publish_review", map[string]any{"ranking": []string{}})
	if again["skipped_empty_re_review"] != true || len(g.reviews) != 1 {
		t.Fatalf("must not post an empty review over a published one: %v", again)
	}
}

func TestPublishGuards(t *testing.T) {
	g := &ghFake{headSHA: "new"}
	s := newSession(t, g)
	id := call(t, s, "add_finding", finding("auth/login.go", 14, "Secret logged"))["finding_id"].(string)

	// dry run validates and ranks but posts nothing
	s.DryRun = true
	r := call(t, s, "publish_review", map[string]any{"ranking": []string{id}})
	if r["dry_run"] != true || len(g.reviews) != 0 {
		t.Fatalf("%v", r)
	}
	s.DryRun = false

	// an anchor that stopped being in the diff is reported, not posted
	if _, err := s.Findings.Update(context.Background(), s.PR, id, func(f *review.Finding) error { f.StartLine, f.EndLine = 90, 90; return nil }); err != nil {
		t.Fatal(err)
	}
	r = call(t, s, "publish_review", map[string]any{"ranking": []string{id}})
	if u, _ := r["unresolvable_findings"].([]any); len(u) != 1 || len(g.reviews) != 0 {
		t.Fatalf("%v", r)
	}

	// re-review with nothing new: skipped, no empty review spam
	s2 := newSession(t, g)
	s2.LastReviewedSHA = "old"
	r = call(t, s2, "publish_review", map[string]any{"ranking": []string{}})
	if r["skipped_empty_re_review"] != true || len(g.reviews) != 0 {
		t.Fatalf("%v", r)
	}
	// a re-review reads only the new commits but anchors against the full PR diff
	if out := call(t, s2, "fetch_review_diff", nil); out["re_review"] != true || fmt.Sprint(out["changed_files"]) != "3" {
		t.Fatalf("%v", out)
	}
}

func TestApprovalPolicy(t *testing.T) {
	run := func(mode review.ApprovalMode, sev string, approve bool, headMoves bool) (map[string]any, *ghFake) {
		g := &ghFake{headSHA: "new"}
		s := newSession(t, g)
		s.Approval = mode
		f := finding("auth/login.go", 14, "Thing")
		f["severity"] = sev
		id := call(t, s, "add_finding", f)["finding_id"].(string)
		if headMoves {
			g.headSHA = "newer"
		}
		return call(t, s, "publish_review", map[string]any{"ranking": []string{id}, "assessment": map[string]any{"approve": approve, "reason": "looks fine"}}), g
	}
	if r, _ := run(review.ApprovalOn, "low", true, false); r["event"] != "APPROVE" {
		t.Fatalf("a low finding with approval on and a positive assessment approves: %v", r)
	}
	if r, _ := run(review.ApprovalOn, "high", true, false); r["event"] != "COMMENT" || !strings.Contains(r["approval_note"].(string), "medium") {
		t.Fatalf("%v", r)
	}
	if r, _ := run(review.ApprovalDryRun, "low", true, false); r["event"] != "COMMENT" || !strings.Contains(r["approval_note"].(string), "dry_run") {
		t.Fatalf("%v", r)
	}
	if r, _ := run("", "low", true, false); r["event"] != "COMMENT" {
		t.Fatalf("default mode must not approve: %v", r)
	}
	if r, _ := run(review.ApprovalOn, "low", false, false); r["event"] != "COMMENT" {
		t.Fatalf("negative assessment: %v", r)
	}
	if r, _ := run(review.ApprovalOn, "low", true, true); r["event"] != "COMMENT" || !strings.Contains(r["approval_note"].(string), "moved") {
		t.Fatalf("a head that moved during review must not be approved: %v", r)
	}
}

func TestReconcileRepliesAndResolvedThreads(t *testing.T) {
	g := &ghFake{headSHA: "new"}
	s := newSession(t, g)
	a := call(t, s, "add_finding", finding("auth/login.go", 14, "Secret logged"))["finding_id"].(string)
	b := call(t, s, "add_finding", finding("auth/login.go", 42, "Magic number"))["finding_id"].(string)
	call(t, s, "publish_review", map[string]any{"ranking": []string{a, b}})
	ctx := context.Background()
	fa, _ := s.Findings.Get(ctx, s.PR, a)
	fb, _ := s.Findings.Get(ctx, s.PR, b)

	// a human replies on a's thread; b's thread is resolved by a human
	g.mu.Lock()
	g.comments = append(g.comments, map[string]any{"id": 77, "body": "this is test code, ignore", "in_reply_to_id": fa.CommentID, "user": map[string]any{"login": "ana", "type": "User"}})
	g.threads[0]["comments"] = map[string]any{"nodes": []map[string]any{{"databaseId": fa.CommentID}, {"databaseId": 77}}}
	g.threads[1]["isResolved"] = true
	g.mu.Unlock()

	rep, err := s.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ResolvedOnGitHub) != 1 || rep.ResolvedOnGitHub[0] != b {
		t.Fatalf("%+v", rep)
	}
	if rs := rep.NewReplies[a]; len(rs) != 1 || rs[0].Author != "ana" || rs[0].Kind != "human_reply" {
		t.Fatalf("%+v", rep)
	}
	fb, _ = s.Findings.Get(ctx, s.PR, b)
	if fb.Status != review.Resolved || fb.Surface != review.SurfaceDone {
		t.Fatalf("%+v", fb)
	}
	// reconciling again reports nothing new
	if rep2, _ := s.Reconcile(ctx); len(rep2.NewReplies) != 0 || len(rep2.ResolvedOnGitHub) != 0 {
		t.Fatalf("%+v", rep2)
	}

	// the agent dismisses a's finding: reply is posted verbatim and the thread resolved
	out := call(t, s, "resolve_finding_thread", map[string]any{"finding_id": a, "status": "dismissed", "note": "Agreed, test code."})
	if out["success"] != true || out["thread_resolved"] != true || len(g.replies) != 1 || g.replies[0] != "Agreed, test code." || len(g.resolved) != 1 {
		t.Fatalf("%v replies=%v resolved=%v", out, g.replies, g.resolved)
	}
	fa, _ = s.Findings.Get(ctx, s.PR, a)
	if fa.Status != review.Dismissed || len(fa.Interactions) != 1 {
		t.Fatalf("%+v", fa)
	}
	if bad := call(t, s, "resolve_finding_thread", map[string]any{"finding_id": a, "status": "open", "note": "x"}); bad["success"] != false {
		t.Fatalf("%v", bad)
	}
	if bad := call(t, s, "update_finding", map[string]any{"id": "ghost", "status": "resolved"}); bad["success"] != false {
		t.Fatalf("%v", bad)
	}
}

func TestSurfaceOnlyMovesForward(t *testing.T) {
	st := review.NewStore(store.NewMemory())
	k := github.PRKey{Owner: "o", Repo: "r", Number: 1}
	f, _, _ := st.Add(context.Background(), k, review.Finding{Title: "x", Fingerprint: "fp", Severity: review.Low})
	_, _ = st.Update(context.Background(), k, f.ID, func(x *review.Finding) error { x.Surface = review.SurfaceDone; return nil })
	got, _ := st.Update(context.Background(), k, f.ID, func(x *review.Finding) error { x.Surface = review.NotSurfaced; return nil })
	if got.Surface != review.SurfaceDone {
		t.Fatalf("surface went backwards: %s", got.Surface)
	}
	// another PR is isolated
	if l, _ := st.List(context.Background(), github.PRKey{Owner: "o", Repo: "r", Number: 2}); len(l) != 0 {
		t.Fatal("PRs share findings")
	}
}

func TestLearnStyleFromHumanComments(t *testing.T) {
	var merged = `[{"number": 1, "merged_at": "2026-01-01T00:00:00Z", "state": "closed"}, {"number": 2, "state": "closed"}]`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls":
			_, _ = w.Write([]byte(merged))
		case "/repos/o/r/pulls/1/comments":
			_, _ = w.Write([]byte(`[{"id":1,"body":"Please wrap errors with %w","path":"a.go","user":{"login":"ana","type":"User"}},
				{"id":2,"body":"bot noise","path":"a.go","user":{"login":"ci[bot]","type":"Bot"}},
				{"id":3,"body":"thanks!","path":"a.go","in_reply_to_id":1,"user":{"login":"bob","type":"User"}},
				{"id":4,"body":"ours <!-- openswe-finding:abc -->","path":"a.go","user":{"login":"ana","type":"User"}}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	c := github.NewClient(github.StaticToken("t"))
	c.BaseURL = ts.URL
	fake := models.NewFake(messages.AI("- Wrap errors with %w"))
	got, err := review.LearnStyle(context.Background(), fake, c.Repo("o", "r"), 10)
	if err != nil || got != "- Wrap errors with %w" {
		t.Fatalf("%q %v", got, err)
	}
	prompt := fake.Calls[0][0].Text()
	if !strings.Contains(prompt, "Please wrap errors with %w") || strings.Contains(prompt, "bot noise") || strings.Contains(prompt, "thanks!") || strings.Contains(prompt, "openswe-finding") {
		t.Fatalf("only human top-level comments belong in the sample:\n%s", prompt)
	}
	// nothing to learn from is an error, not an empty style
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer empty.Close()
	c.BaseURL = empty.URL
	if _, err := review.LearnStyle(context.Background(), fake, c.Repo("o", "r"), 10); err == nil {
		t.Fatal("no comments must be an error")
	}
	// style store: defaults are disabled, round trip works
	ss := &review.StyleStore{S: store.NewMemory()}
	if st, _ := ss.Get(context.Background(), "O", "R"); st.Enabled || st.FullName != "o/r" {
		t.Fatalf("%+v", st)
	}
	_ = ss.Put(context.Background(), review.Style{FullName: "O/R", CustomPrompt: "x", Enabled: true, Approval: review.ApprovalOn})
	if st, _ := ss.Get(context.Background(), "o", "r"); !st.Enabled || st.CustomPrompt != "x" || st.Approval != review.ApprovalOn {
		t.Fatalf("%+v", st)
	}
	_ = tools.Errorf
}
