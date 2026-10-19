// Package fakegithub is a small in-process GitHub for the Open SWE examples:
// one repository, pull requests, branches, checks, reviews and review
// threads, enough for the engineer, reviewer, babysit and webhook demos to run
// offline. It is not a general GitHub emulator.
package fakegithub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

// Check is a CI check of the PR head.
type Check struct {
	ID         int
	Name       string
	Status     string
	Conclusion string
	App        string
}

// GitHub is the fake server state; fields are guarded by Mu.
type GitHub struct {
	Mu     sync.Mutex
	Owner  string
	Repo   string
	Server *httptest.Server

	Pushed   map[string]bool
	PRs      map[int]*PR
	NextPR   int
	Comments []string
	Reviews  []github.ReviewRequest
	Reruns   int
	Checks   []Check
	// ReviewComments are the inline comments as GitHub lists them.
	ReviewComments []map[string]any
	Threads        []map[string]any
	nextCommentID  int64
	reviewSeq      int64
}

// PR is a pull request.
type PR struct {
	Number  int
	Title   string
	Body    string
	Head    string
	HeadSHA string
	Base    string
	Draft   bool
	State   string
	Diff    string
}

// New starts the server for owner/repo.
func New(owner, repo string) *GitHub {
	g := &GitHub{Owner: owner, Repo: repo, Pushed: map[string]bool{}, PRs: map[int]*PR{}, NextPR: 100}
	g.Server = httptest.NewServer(http.HandlerFunc(g.serve))
	return g
}

// Client returns a client pointed at the fake.
func (g *GitHub) Client() *github.Client {
	c := github.NewClient(github.StaticToken("fake-token"))
	c.BaseURL = g.Server.URL
	return c
}

// Close stops the server.
func (g *GitHub) Close() { g.Server.Close() }

// AddPR registers an existing pull request.
func (g *GitHub) AddPR(pr PR) {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	if pr.State == "" {
		pr.State = "open"
	}
	if pr.Base == "" {
		pr.Base = "main"
	}
	g.PRs[pr.Number] = &pr
}

func (g *GitHub) base() string { return "/repos/" + g.Owner + "/" + g.Repo }

func jsonOut(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

func (g *GitHub) prJSON(p *PR) map[string]any {
	return map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "draft": p.Draft,
		"html_url": fmt.Sprintf("https://github.com/%s/%s/pull/%d", g.Owner, g.Repo, p.Number),
		"head":     map[string]any{"ref": p.Head, "sha": p.HeadSHA}, "base": map[string]any{"ref": p.Base, "sha": "base-sha"}}
}

func (g *GitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path
	notFound := func() {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}
	if path == "/graphql" {
		g.graphql(w, body)
		return
	}
	if !strings.HasPrefix(path, g.base()) {
		notFound()
		return
	}
	rest := strings.TrimPrefix(path, g.base())
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case rest == "" && r.Method == "GET":
		jsonOut(w, map[string]any{"default_branch": "main"})
	case parts[0] == "branches" && strings.HasSuffix(rest, "/required_status_checks"):
		notFound() // no branch protection in the fake
	case parts[0] == "branches" && r.Method == "GET":
		name := strings.Join(parts[1:], "/") // branch names may contain slashes
		if g.Pushed[name] || name == "main" {
			jsonOut(w, map[string]any{"name": name})
			return
		}
		notFound()
	case rest == "/pulls" && r.Method == "GET":
		head := strings.TrimPrefix(r.URL.Query().Get("head"), g.Owner+":")
		out := []map[string]any{}
		for _, p := range g.PRs {
			if p.Head == head && p.State == "open" {
				out = append(out, g.prJSON(p))
			}
		}
		jsonOut(w, out)
	case rest == "/pulls" && r.Method == "POST":
		var req struct {
			Title, Head, Base, Body string
			Draft                   bool
		}
		_ = json.Unmarshal(body, &req)
		g.NextPR++
		p := &PR{Number: g.NextPR, Title: req.Title, Body: req.Body, Head: req.Head, Base: req.Base, Draft: req.Draft, State: "open", HeadSHA: "sha-" + req.Head}
		g.PRs[p.Number] = p
		jsonOut(w, g.prJSON(p))
	case len(parts) == 2 && parts[0] == "pulls" && r.Method == "GET":
		n, _ := strconv.Atoi(parts[1])
		p := g.PRs[n]
		if p == nil {
			notFound()
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "diff") {
			_, _ = w.Write([]byte(p.Diff))
			return
		}
		jsonOut(w, g.prJSON(p))
	case len(parts) == 3 && parts[0] == "pulls" && parts[2] == "reviews" && r.Method == "POST":
		var rr github.ReviewRequest
		_ = json.Unmarshal(body, &rr)
		g.Reviews = append(g.Reviews, rr)
		g.reviewSeq++
		for _, c := range rr.Comments {
			g.nextCommentID++
			id := g.nextCommentID
			g.ReviewComments = append(g.ReviewComments, map[string]any{"id": id, "body": c.Body, "path": c.Path, "pull_request_review_id": g.reviewSeq,
				"user": map[string]any{"login": "openswe[bot]", "type": "Bot"}})
			g.Threads = append(g.Threads, map[string]any{"id": fmt.Sprintf("THREAD_%d", id), "isResolved": false, "isOutdated": false,
				"comments": map[string]any{"nodes": []map[string]any{{"databaseId": id}}}})
		}
		jsonOut(w, map[string]any{"id": g.reviewSeq, "state": "COMMENTED"})
	case len(parts) == 3 && parts[0] == "pulls" && parts[2] == "comments" && r.Method == "GET":
		jsonOut(w, g.ReviewComments)
	case len(parts) == 5 && parts[0] == "pulls" && parts[2] == "comments" && parts[4] == "replies" && r.Method == "POST":
		var b map[string]string
		_ = json.Unmarshal(body, &b)
		g.Comments = append(g.Comments, "(reply) "+b["body"])
		jsonOut(w, map[string]any{"id": 999})
	case len(parts) == 3 && parts[0] == "issues" && parts[2] == "comments" && r.Method == "POST":
		var b map[string]string
		_ = json.Unmarshal(body, &b)
		g.Comments = append(g.Comments, b["body"])
		g.nextCommentID++
		jsonOut(w, map[string]any{"id": g.nextCommentID})
	case len(parts) == 3 && parts[0] == "issues" && parts[1] == "comments" && r.Method == "DELETE":
		w.WriteHeader(204)
	case len(parts) == 3 && parts[0] == "commits" && parts[2] == "check-runs":
		var runs []map[string]any
		for _, c := range g.Checks {
			runs = append(runs, map[string]any{"id": c.ID, "name": c.Name, "status": c.Status, "conclusion": c.Conclusion,
				"html_url": fmt.Sprintf("https://github.com/%s/%s/actions/runs/%d/job/1", g.Owner, g.Repo, 7000+c.ID), "app": map[string]any{"slug": c.App}})
		}
		jsonOut(w, map[string]any{"check_runs": runs})
	case len(parts) == 4 && parts[0] == "actions" && parts[1] == "runs" && parts[3] == "rerun-failed-jobs":
		g.Reruns++
		w.WriteHeader(201)
	case parts[0] == "contents":
		notFound()
	default:
		notFound()
	}
}

func (g *GitHub) graphql(w http.ResponseWriter, body []byte) {
	var req struct {
		Query     string
		Variables map[string]any
	}
	_ = json.Unmarshal(body, &req)
	if strings.Contains(req.Query, "resolveReviewThread") {
		id, _ := req.Variables["id"].(string)
		for _, t := range g.Threads {
			if t["id"] == id {
				t["isResolved"] = true
			}
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
		return
	}
	out, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewThreads": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false}, "nodes": g.Threads}}}}})
	_, _ = w.Write(out)
}

// Send delivers a webhook to url with a valid signature.
func Send(url, secret, event, delivery string, payload map[string]any) (int, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", github.Sign(secret, body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
