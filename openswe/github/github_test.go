package github_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

type route func(w http.ResponseWriter, r *http.Request, body []byte)

func fakeGitHub(t *testing.T, routes map[string]route) (*github.Client, *[]string) {
	t.Helper()
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		calls = append(calls, key)
		h, ok := routes[key]
		if !ok {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		h(w, r, body)
	}))
	t.Cleanup(ts.Close)
	c := github.NewClient(github.StaticToken("tok"))
	c.BaseURL = ts.URL
	return c, &calls
}

func jsonResp(v any) route {
	return func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
}

func TestCreatePRIsIdempotentPerBranch(t *testing.T) {
	ctx := context.Background()
	var open []map[string]any
	var created int
	c, calls := fakeGitHub(t, map[string]route{
		"GET /repos/o/r/pulls": func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.URL.Query().Get("head") != "o:feat" || r.URL.Query().Get("state") != "open" {
				t.Errorf("bad query %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(open)
		},
		"POST /repos/o/r/pulls": func(w http.ResponseWriter, r *http.Request, body []byte) {
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if req["head"] != "feat" || req["base"] != "main" || req["draft"] != true || r.Header.Get("Authorization") != "Bearer tok" {
				t.Errorf("bad request %v / %s", req, r.Header.Get("Authorization"))
			}
			created++
			pr := map[string]any{"number": 7, "html_url": "https://github.com/o/r/pull/7", "state": "open", "draft": true}
			open = []map[string]any{pr}
			_ = json.NewEncoder(w).Encode(pr)
		},
	})
	repo := c.Repo("o", "r")
	pr, isNew, err := repo.CreatePR(ctx, github.CreatePRRequest{Title: "t", Head: "feat", Base: "main", Draft: true})
	if err != nil || !isNew || pr.Number != 7 {
		t.Fatalf("%+v %v %v", pr, isNew, err)
	}
	again, isNew, err := repo.CreatePR(ctx, github.CreatePRRequest{Title: "t", Head: "feat", Base: "main", Draft: true})
	if err != nil || isNew || again.Number != 7 || created != 1 {
		t.Fatalf("retry must not open a duplicate: %+v new=%v created=%d %v", again, isNew, created, err)
	}
	_ = calls
}

func TestErrorsAreClassified(t *testing.T) {
	ctx := context.Background()
	c, _ := fakeGitHub(t, map[string]route{
		"GET /repos/o/r/pulls/1": func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
		},
	})
	_, err := c.Repo("o", "r").GetPR(ctx, 1)
	var ae *github.APIError
	if !github.IsAccessError(err) || !asAPI(err, &ae) || ae.RetryAfter != 30*time.Second || ae.Message != "rate limited" {
		t.Fatalf("%v", err)
	}
	_, err = c.Repo("o", "r").GetPR(ctx, 2)
	if !github.IsNotFound(err) || !github.IsAccessError(err) {
		t.Fatalf("%v", err)
	}
	if github.IsNotFound(nil) {
		t.Fatal("nil")
	}
}

func asAPI(err error, target **github.APIError) bool {
	ae, ok := err.(*github.APIError)
	if ok {
		*target = ae
	}
	return ok
}

func TestDiffChecksAndRerun(t *testing.T) {
	ctx := context.Background()
	var rerun atomic.Int32
	c, _ := fakeGitHub(t, map[string]route{
		"GET /repos/o/r/pulls/3": func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if !strings.Contains(r.Header.Get("Accept"), "diff") {
				t.Errorf("accept %q", r.Header.Get("Accept"))
			}
			_, _ = w.Write([]byte("diff --git a/x b/x\n"))
		},
		"GET /repos/o/r/commits/abc/check-runs": jsonResp(map[string]any{"check_runs": []map[string]any{
			{"id": 1, "name": "lint", "status": "completed", "conclusion": "success", "app": map[string]any{"slug": "github-actions"}},
			{"id": 2, "name": "test", "status": "completed", "conclusion": "failure", "app": map[string]any{"slug": "github-actions"}},
			{"id": 3, "name": "e2e", "status": "in_progress"},
		}}),
		"POST /repos/o/r/actions/runs/99/rerun-failed-jobs": func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			rerun.Add(1)
			w.WriteHeader(201)
		},
		"GET /repos/o/r/branches/main/protection/required_status_checks": jsonResp(map[string]any{"contexts": []string{"lint", "test"}}),
	})
	repo := c.Repo("o", "r")
	d, err := repo.PRDiff(ctx, 3)
	if err != nil || !strings.HasPrefix(d, "diff --git") {
		t.Fatalf("%q %v", d, err)
	}
	checks, err := repo.ListCheckRuns(ctx, "abc")
	if err != nil || len(checks) != 3 || !checks[1].Failing() || !checks[1].IsActions() || checks[2].Completed() {
		t.Fatalf("%+v %v", checks, err)
	}
	all := github.Summarize(checks, nil)
	if all.State != "failing" || len(all.Failing) != 1 || len(all.Pending) != 1 {
		t.Fatalf("%+v", all)
	}
	req := repo.RequiredChecks(ctx, "main")
	if github.Summarize(checks, req).State != "failing" {
		t.Fatal("required failing")
	}
	// only lint required: green, e2e is informational
	if s := github.Summarize(checks, []string{"lint"}); s.State != "green" || s.Total != 1 {
		t.Fatalf("%+v", s)
	}
	// a required check that never reported is pending, not green
	if s := github.Summarize(checks[:1], []string{"lint", "deploy"}); s.State != "pending" || github.Names(s.Pending) != "deploy" {
		t.Fatalf("%+v", s)
	}
	if github.Summarize(nil, nil).State != "none" {
		t.Fatal("none")
	}
	if repo.RequiredChecks(ctx, "dev") != nil {
		t.Fatal("unprotected branch => no required checks")
	}
	if err := repo.RerunFailedJobs(ctx, 99); err != nil || rerun.Load() != 1 {
		t.Fatalf("%v", err)
	}
}

func TestReviewAndThreads(t *testing.T) {
	ctx := context.Background()
	var posted github.ReviewRequest
	var resolved string
	c, _ := fakeGitHub(t, map[string]route{
		"POST /repos/o/r/pulls/5/reviews": func(w http.ResponseWriter, _ *http.Request, body []byte) {
			_ = json.Unmarshal(body, &posted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 11, "state": "COMMENTED"})
		},
		"POST /graphql": func(w http.ResponseWriter, _ *http.Request, body []byte) {
			var req struct {
				Query     string
				Variables map[string]any
			}
			_ = json.Unmarshal(body, &req)
			if strings.Contains(req.Query, "resolveReviewThread") {
				resolved = req.Variables["id"].(string)
				_, _ = w.Write([]byte(`{"data":{}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[
				{"id":"T1","isResolved":false,"isOutdated":false,"comments":{"nodes":[{"databaseId":100},{"databaseId":101}]}},
				{"id":"T2","isResolved":true,"isOutdated":true,"comments":{"nodes":[{"databaseId":200}]}}]}}}}}`))
		},
	})
	repo := c.Repo("o", "r")
	rv, err := repo.CreateReview(ctx, 5, github.ReviewRequest{CommitID: "abc", Body: "b", Event: "COMMENT",
		Comments: []github.ReviewComment{{Path: "a.go", Body: "x", Line: 10, Side: "RIGHT"}}})
	if err != nil || rv.ID != 11 || posted.CommitID != "abc" || len(posted.Comments) != 1 || posted.Comments[0].Line != 10 {
		t.Fatalf("%+v %+v %v", rv, posted, err)
	}
	th, err := repo.ReviewThreads(ctx, 5)
	if err != nil || len(th) != 2 || th[0].Resolved || !th[1].Resolved || len(th[0].CommentIDs) != 2 || th[0].CommentIDs[1] != 101 {
		t.Fatalf("%+v %v", th, err)
	}
	if err := repo.ResolveThread(ctx, "T1"); err != nil || resolved != "T1" {
		t.Fatalf("%v %q", err, resolved)
	}
	// GraphQL errors surface
	c2, _ := fakeGitHub(t, map[string]route{"POST /graphql": func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	}})
	if err := c2.Repo("o", "r").ResolveThread(ctx, "x"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("%v", err)
	}
}

func TestAppAuthJWTAndInstallationTokens(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var minted atomic.Int32
	var jwtSeen string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/o/r/installation":
			jwtSeen = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			_, _ = w.Write([]byte(`{"id": 42}`))
		case "POST /app/installations/42/access_tokens":
			minted.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_inst", "expires_at": time.Now().Add(time.Hour)})
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer ts.Close()
	app := &github.App{AppID: 123, Key: pemKey, BaseURL: ts.URL}
	for range 3 {
		tok, err := app.Token(context.Background(), "o", "r")
		if err != nil || tok != "ghs_inst" {
			t.Fatalf("%q %v", tok, err)
		}
	}
	if minted.Load() != 1 {
		t.Fatalf("installation token must be cached, minted %d", minted.Load())
	}
	// the JWT is a valid RS256 token for the app
	parts := strings.Split(jwtSeen, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", jwtSeen)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("bad signature: %v", err)
	}
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var cl map[string]any
	_ = json.Unmarshal(claims, &cl)
	if cl["iss"] != "123" || cl["exp"].(float64)-cl["iat"].(float64) > 600 {
		t.Fatalf("claims %v", cl)
	}
	// uninstalled repo: a clear error, not a silent fallback
	if _, err := app.Token(context.Background(), "o", "other"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("%v", err)
	}
	if _, err := (&github.App{AppID: 1, Key: []byte("junk")}).JWT(); err == nil {
		t.Fatal("bad key must fail")
	}
}

func TestWebhookSignatureAndParsing(t *testing.T) {
	body := []byte(`{"action":"created","repository":{"name":"r","private":true,"owner":{"login":"o"}},"sender":{"login":"ana","type":"User"},
		"issue":{"number":9,"title":"Bug","body":"it breaks","user":{"login":"bob"},"pull_request":{"url":"x"}},
		"comment":{"id":5,"body":"@openswe fix this","user":{"login":"ana","type":"User"}}}`)
	sig := github.Sign("s3", body)
	if !github.VerifySignature("s3", sig, body) || github.VerifySignature("other", sig, body) || github.VerifySignature("s3", "sha256=00", body) ||
		github.VerifySignature("", sig, body) || github.VerifySignature("s3", "", body) || github.VerifySignature("s3", sig, append(body, ' ')) {
		t.Fatal("signature verification")
	}
	e, err := github.ParseEvent("issue_comment", "d-1", body)
	if err != nil || e.FullName() != "o/r" || e.Number != 9 || !e.Private || !e.IsPullRequestComment || e.Comment.Body != "@openswe fix this" || e.Comment.IsBot || e.Delivery != "d-1" {
		t.Fatalf("%+v %v", e, err)
	}
	cr, err := github.ParseEvent("check_run", "d-2", []byte(`{"action":"completed","repository":{"name":"r","owner":{"login":"o"}},"sender":{"login":"github-actions[bot]","type":"Bot"},
		"check_run":{"name":"test","status":"completed","conclusion":"failure","head_sha":"abc","pull_requests":[{"number":4}],"check_suite":{"head_branch":"feat"}}}`))
	if err != nil || cr.CheckName != "test" || cr.Conclusion != "failure" || cr.HeadSHA != "abc" || cr.HeadRef != "feat" || len(cr.PRNumbers) != 1 || !cr.SenderIsBot {
		t.Fatalf("%+v %v", cr, err)
	}
	pr, _ := github.ParseEvent("pull_request", "d-3", []byte(`{"action":"opened","repository":{"name":"r","owner":{"login":"o"}},"sender":{"login":"ana"},
		"pull_request":{"number":4,"title":"T","draft":true,"state":"open","user":{"login":"ana"},"head":{"sha":"h","ref":"feat"},"base":{"ref":"main"}}}`))
	if pr.Number != 4 || !pr.Draft || pr.HeadSHA != "h" || pr.BaseRef != "main" || pr.IsPullRequestComment {
		t.Fatalf("%+v", pr)
	}
	if _, err := github.ParseEvent("push", "d", []byte(`{}`)); err == nil {
		t.Fatal("payload without repository must fail")
	}
	if _, err := github.ParseEvent("push", "d", []byte(`nope`)); err == nil {
		t.Fatal("bad json must fail")
	}
}

func TestParsePRURL(t *testing.T) {
	k, err := github.ParsePRURL("https://github.com/langchain-ai/open-swe/pull/12")
	if err != nil || k != (github.PRKey{"langchain-ai", "open-swe", 12}) || k.String() != "https://github.com/langchain-ai/open-swe/pull/12" {
		t.Fatalf("%+v %v", k, err)
	}
	for _, bad := range []string{"", "nope", "https://github.com/o/r/issues/1", "https://github.com/o/r/pull/x", "https://github.com/o/r"} {
		if _, err := github.ParsePRURL(bad); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
}

func TestBranchNamesWithSlashesAreNotEscaped(t *testing.T) {
	var path string
	c, _ := fakeGitHub(t, map[string]route{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"name":"x"}`))
	}))
	defer ts.Close()
	c.BaseURL = ts.URL
	ok, err := c.Repo("o", "r").BranchExists(context.Background(), "openswe/fix empty-user")
	if err != nil || !ok || path != "/repos/o/r/branches/openswe/fix%20empty-user" {
		t.Fatalf("%v %v %q", ok, err, path)
	}
}
