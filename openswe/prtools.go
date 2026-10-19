package openswe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

// PRTools are the delivery tools of the engineer: open_pull_request,
// link_pull_request, github_comment and request_pr_review.
type PRTools struct {
	GitHub *github.Client
	// DraftPRs opens new pull requests as drafts (the person's setting).
	DraftPRs bool
	Links    *PRLinks
	// Attribution is appended to every PR body (e.g. "Opened by Open SWE").
	Attribution string
	// ThreadURL, when set, adds a link back to the thread to the PR body.
	ThreadURL func(thread string) string
	// Dispatcher starts reviews for request_pr_review.
	Dispatcher *triggers.Dispatcher
}

func threadOf(ctx context.Context) string {
	t, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	return t
}

type openPRArgs struct {
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Head           string `json:"head" description:"Your branch; push it BEFORE calling this"`
	Base           string `json:"base,omitempty" description:"Target branch (default: the repository's default branch)"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	ResolvesThread bool   `json:"resolves_thread,omitempty" description:"Set unless more PRs are coming for this thread; the thread resolves when its PRs merge or close"`
}

const accessFailure = "Do not retry through gh pr create, the REST API or any other path: report this failure to the user and end the task. (Retrying this identical call once after a timeout is fine.)"

func (p PRTools) openPR() tools.Tool {
	return tools.New("open_pull_request",
		"Open a pull request for a branch you already pushed. Idempotent: if a PR exists for the branch it is returned (then edit it with gh pr edit and add commits). Never use gh pr create.",
		func(ctx context.Context, a openPRArgs) (any, error) {
			if a.Owner == "" || a.Repo == "" || a.Head == "" || strings.TrimSpace(a.Title) == "" {
				return nil, tools.Errorf("owner, repo, head and title are required")
			}
			repo := p.GitHub.Repo(a.Owner, a.Repo)
			ok, err := repo.BranchExists(ctx, a.Head)
			if err != nil {
				return nil, p.accessErr("checking branch %s", err, a.Head)
			}
			if !ok {
				return nil, tools.Errorf("branch %q is not visible on GitHub yet. Push it first (git push origin %s), then call this tool again.", a.Head, a.Head)
			}
			base := a.Base
			if base == "" {
				if base, err = repo.DefaultBranch(ctx); err != nil {
					return nil, p.accessErr("reading the default branch", err)
				}
			}
			thread := threadOf(ctx)
			pr, created, err := repo.CreatePR(ctx, github.CreatePRRequest{Title: strings.TrimSpace(a.Title), Head: a.Head, Base: base, Draft: p.DraftPRs, Body: p.body(a.Body, thread)})
			if err != nil {
				return nil, p.accessErr("opening the pull request", err)
			}
			if p.Links != nil && thread != "" {
				if err := p.Links.Link(ctx, thread, github.PRKey{Owner: a.Owner, Repo: a.Repo, Number: pr.Number}, a.ResolvesThread); err != nil {
					return nil, err
				}
			}
			out := map[string]any{"url": pr.URL, "number": pr.Number, "created": created, "draft": pr.Draft}
			if !created {
				out["note"] = "A pull request already existed for this branch. Do not open a duplicate: edit it with gh pr edit and add follow-up work as new commits."
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		})
}

func (p PRTools) accessErr(what string, err error, a ...any) error {
	msg := fmt.Sprintf(what, a...)
	if github.IsAccessError(err) {
		return tools.Errorf("%s failed with an access error (%v). %s", msg, err, accessFailure)
	}
	return tools.Errorf("%s failed: %v", msg, err)
}

// body appends the attribution and a link back to the thread once.
func (p PRTools) body(body, thread string) string {
	body = strings.TrimSpace(body)
	var refs []string
	if p.ThreadURL != nil && thread != "" && !strings.Contains(body, "## References") {
		refs = append(refs, "- Thread: "+p.ThreadURL(thread))
	}
	if len(refs) > 0 {
		body += "\n\n## References\n\n" + strings.Join(refs, "\n")
	}
	if p.Attribution != "" && !strings.Contains(body, p.Attribution) {
		body += "\n\n---\n" + p.Attribution
	}
	return body
}

type linkArgs struct {
	PRURL          string `json:"pr_url"`
	ResolvesThread bool   `json:"resolves_thread,omitempty"`
}

func (p PRTools) linkPR() tools.Tool {
	return tools.New("link_pull_request",
		"Link an existing pull request (one the user gave you) to this thread, so the thread tracks it. Call it once.",
		func(ctx context.Context, a linkArgs) (any, error) {
			k, err := github.ParsePRURL(a.PRURL)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			if _, err := p.GitHub.Repo(k.Owner, k.Repo).GetPR(ctx, k.Number); err != nil {
				return nil, p.accessErr("reading %s", err, k)
			}
			thread := threadOf(ctx)
			if p.Links == nil || thread == "" {
				return nil, tools.Errorf("cannot link: no thread or link store configured")
			}
			if err := p.Links.Link(ctx, thread, k, a.ResolvesThread); err != nil {
				return nil, err
			}
			return "linked " + k.String(), nil
		})
}

type commentArgs struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number" description:"Issue or pull request number"`
	Body   string `json:"body" description:"Markdown comment"`
}

func (p PRTools) comment() tools.Tool {
	return tools.New("github_comment",
		"Post a comment on a GitHub issue or pull request: the way to answer or report back when you were triggered from GitHub.",
		func(ctx context.Context, a commentArgs) (any, error) {
			if a.Owner == "" || a.Repo == "" || a.Number <= 0 || strings.TrimSpace(a.Body) == "" {
				return nil, tools.Errorf("owner, repo, number and a non-empty body are required")
			}
			id, err := p.GitHub.Repo(a.Owner, a.Repo).Comment(ctx, a.Number, strings.TrimSpace(a.Body))
			if err != nil {
				return nil, p.accessErr("commenting on %s/%s#%d", err, a.Owner, a.Repo, a.Number)
			}
			return fmt.Sprintf("commented (id %d)", id), nil
		})
}

type reviewArgs struct {
	PRURL string `json:"pr_url"`
}

func (p PRTools) requestReview() tools.Tool {
	return tools.New("request_pr_review",
		"Start the automated reviewer on a pull request. Call it once, report that it started, and stop; do not clone, edit or push. Refuses draft PRs.",
		func(ctx context.Context, a reviewArgs) (any, error) {
			k, err := github.ParsePRURL(a.PRURL)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			pr, err := p.GitHub.Repo(k.Owner, k.Repo).GetPR(ctx, k.Number)
			if err != nil {
				return nil, p.accessErr("reading %s", err, k)
			}
			if pr.Draft {
				return nil, tools.Errorf("%s is a draft. Never mark a draft ready yourself: give the author its URL and ask them to review the changes and click \"Ready for review\"; then the review can start.", k)
			}
			if pr.State != "open" {
				return nil, tools.Errorf("%s is %s; only open pull requests can be reviewed", k, pr.State)
			}
			if p.Dispatcher == nil {
				return nil, tools.Errorf("no reviewer is configured")
			}
			msg := fmt.Sprintf("Review pull request %s (head %s, base %s).", k, pr.Head.SHA, pr.Base.Ref)
			out, err := p.Dispatcher.Dispatch(ctx, triggers.Task{Kind: triggers.KindReviewer, Source: "github", Owner: k.Owner, Repo: k.Repo, Number: k.Number,
				ThreadID: triggers.ThreadID(triggers.KindReviewer, k.Owner, k.Repo, k.Number), Messages: []messages.Message{messages.Human(msg)},
				Metadata: map[string]any{"head_sha": pr.Head.SHA, "base_ref": pr.Base.Ref, "requested_by_thread": threadOf(ctx)}, Reason: "requested by agent"})
			if err != nil {
				return nil, err
			}
			if out == triggers.Queued {
				return "a review of " + k.String() + " is already running; your request was queued behind it", nil
			}
			return "review of " + k.String() + " started", nil
		})
}

// All returns the four tools.
func (p PRTools) All() []tools.Tool {
	return []tools.Tool{p.openPR(), p.linkPR(), p.comment(), p.requestReview()}
}
