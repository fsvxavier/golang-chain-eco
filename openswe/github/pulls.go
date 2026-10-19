package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// PullRequest is the subset of a PR the workflows use.
type PullRequest struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"` // open | closed
	Draft     bool       `json:"draft"`
	Merged    bool       `json:"merged"`
	MergedAt  *time.Time `json:"merged_at"`
	URL       string     `json:"html_url"`
	User      User       `json:"user"`
	Head      Ref        `json:"head"`
	Base      Ref        `json:"base"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// User is a GitHub account.
type User struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// Ref is one side of a pull request.
type Ref struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}

// PRKey identifies a pull request.
type PRKey struct {
	Owner, Repo string
	Number      int
}

// String is the canonical https URL.
func (k PRKey) String() string {
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", k.Owner, k.Repo, k.Number)
}

// ParsePRURL reads https://github.com/owner/repo/pull/N.
func ParsePRURL(s string) (PRKey, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" {
		return PRKey{}, fmt.Errorf("github: %q is not a pull request URL", s)
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) < 4 || p[2] != "pull" {
		return PRKey{}, fmt.Errorf("github: %q is not a pull request URL", s)
	}
	var n int
	if _, err := fmt.Sscanf(p[3], "%d", &n); err != nil || n <= 0 {
		return PRKey{}, fmt.Errorf("github: %q has no pull request number", s)
	}
	return PRKey{p[0], p[1], n}, nil
}

// GetPR fetches a pull request.
func (r *Repo) GetPR(ctx context.Context, number int) (PullRequest, error) {
	var pr PullRequest
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/pulls/%d", number), reqOpts{}, &pr)
	return pr, err
}

// CreatePRRequest describes a new pull request.
type CreatePRRequest struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body"`
	Draft bool   `json:"draft"`
}

// FindOpenPR returns the open PR whose head is branch, if any.
func (r *Repo) FindOpenPR(ctx context.Context, branch string) (*PullRequest, error) {
	var prs []PullRequest
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/pulls"),
		reqOpts{query: url.Values{"head": {r.Owner + ":" + branch}, "state": {"open"}}}, &prs)
	if err != nil || len(prs) == 0 {
		return nil, err
	}
	return &prs[0], nil
}

// escRef escapes a branch or ref for a URL path but keeps the slashes of
// names like "feature/login", which GitHub accepts unescaped.
func escRef(s string) string { return strings.ReplaceAll(url.PathEscape(s), "%2F", "/") }

// BranchExists reports whether a branch is visible on the remote.
func (r *Repo) BranchExists(ctx context.Context, branch string) (bool, error) {
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/branches/%s", escRef(branch)), reqOpts{}, nil)
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// DefaultBranch returns the repository's default branch.
func (r *Repo) DefaultBranch(ctx context.Context) (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path(""), reqOpts{}, &repo)
	return repo.DefaultBranch, err
}

// CreatePR opens a pull request. It is idempotent per branch: when an open PR
// already exists for the head it is returned with created=false, so retries
// never produce duplicates.
func (r *Repo) CreatePR(ctx context.Context, req CreatePRRequest) (pr PullRequest, created bool, err error) {
	if existing, err := r.FindOpenPR(ctx, req.Head); err != nil {
		return PullRequest{}, false, err
	} else if existing != nil {
		return *existing, false, nil
	}
	_, _, err = r.c.do(ctx, r.Owner, r.Name, "POST", r.path("/pulls"), reqOpts{body: req}, &pr)
	return pr, err == nil, err
}

// UpdatePR edits title/body of a pull request (never the draft state: only a
// human may mark a PR ready).
func (r *Repo) UpdatePR(ctx context.Context, number int, title, body string) (PullRequest, error) {
	patch := map[string]any{}
	if title != "" {
		patch["title"] = title
	}
	if body != "" {
		patch["body"] = body
	}
	var pr PullRequest
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "PATCH", r.path("/pulls/%d", number), reqOpts{body: patch}, &pr)
	return pr, err
}

// PRDiff returns the unified diff of a pull request.
func (r *Repo) PRDiff(ctx context.Context, number int) (string, error) {
	_, b, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/pulls/%d", number), reqOpts{accept: "application/vnd.github.v3.diff"}, nil)
	return string(b), err
}

// CompareDiff returns the unified diff between two commits (used for
// re-reviews: last reviewed SHA .. new head).
func (r *Repo) CompareDiff(ctx context.Context, base, head string) (string, error) {
	_, b, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/compare/%s...%s", url.PathEscape(base), url.PathEscape(head)),
		reqOpts{accept: "application/vnd.github.v3.diff"}, nil)
	return string(b), err
}

// PRFile is a changed file of a pull request.
type PRFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// PRFiles lists the files of a pull request.
func (r *Repo) PRFiles(ctx context.Context, number int) ([]PRFile, error) {
	return paged[PRFile](ctx, r.c, r.Owner, r.Name, r.path("/pulls/%d/files", number), nil, "")
}

// FileContent reads a file at a ref ("" = default branch); not-found is (nil, nil).
func (r *Repo) FileContent(ctx context.Context, path, ref string) ([]byte, error) {
	q := url.Values{}
	if ref != "" {
		q.Set("ref", ref)
	}
	_, b, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/contents/%s", path), reqOpts{accept: "application/vnd.github.raw+json", query: q}, nil)
	if IsNotFound(err) {
		return nil, nil
	}
	return b, err
}

// Comment posts an issue/PR conversation comment and returns its id.
func (r *Repo) Comment(ctx context.Context, number int, body string) (int64, error) {
	var c struct {
		ID int64 `json:"id"`
	}
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "POST", r.path("/issues/%d/comments", number), reqOpts{body: map[string]string{"body": body}}, &c)
	return c.ID, err
}

// DeleteComment removes an issue comment (used to clear "review started" notes).
func (r *Repo) DeleteComment(ctx context.Context, id int64) error {
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "DELETE", r.path("/issues/comments/%d", id), reqOpts{}, nil)
	return err
}

// AddLabels adds labels to an issue or PR.
func (r *Repo) AddLabels(ctx context.Context, number int, labels ...string) error {
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "POST", r.path("/issues/%d/labels", number), reqOpts{body: map[string]any{"labels": labels}}, nil)
	return err
}
