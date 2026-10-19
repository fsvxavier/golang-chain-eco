package github

import (
	"context"
)

// ReviewComment is an inline comment to publish with a review.
type ReviewComment struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Line      int    `json:"line"`
	Side      string `json:"side,omitempty"` // RIGHT (default) | LEFT
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
}

// ReviewRequest creates a pull request review.
type ReviewRequest struct {
	CommitID string          `json:"commit_id,omitempty"`
	Body     string          `json:"body"`
	Event    string          `json:"event"` // COMMENT | APPROVE | REQUEST_CHANGES
	Comments []ReviewComment `json:"comments,omitempty"`
}

// Review is a created review.
type Review struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
	URL   string `json:"html_url"`
}

// CreateReview posts a review with inline comments in one request.
func (r *Repo) CreateReview(ctx context.Context, number int, req ReviewRequest) (Review, error) {
	var rv Review
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "POST", r.path("/pulls/%d/reviews", number), reqOpts{body: req}, &rv)
	return rv, err
}

// PRComment is an inline review comment on a pull request.
type PRComment struct {
	ID          int64  `json:"id"`
	Body        string `json:"body"`
	Path        string `json:"path"`
	Line        int    `json:"line"`
	InReplyToID int64  `json:"in_reply_to_id"`
	User        User   `json:"user"`
	ReviewID    int64  `json:"pull_request_review_id"`
	CommitID    string `json:"commit_id"`
}

// ListReviewComments lists inline comments of a PR.
func (r *Repo) ListReviewComments(ctx context.Context, number int) ([]PRComment, error) {
	return paged[PRComment](ctx, r.c, r.Owner, r.Name, r.path("/pulls/%d/comments", number), nil, "")
}

// ReplyToReviewComment replies in a review thread.
func (r *Repo) ReplyToReviewComment(ctx context.Context, number int, commentID int64, body string) (int64, error) {
	var c struct {
		ID int64 `json:"id"`
	}
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "POST", r.path("/pulls/%d/comments/%d/replies", number, commentID), reqOpts{body: map[string]string{"body": body}}, &c)
	return c.ID, err
}

// Thread is a review thread (GraphQL), with the REST ids of its comments.
type Thread struct {
	ID         string
	Resolved   bool
	Outdated   bool
	CommentIDs []int64
}

const threadsQuery = `query($owner:String!,$name:String!,$number:Int!,$after:String){
  repository(owner:$owner,name:$name){ pullRequest(number:$number){
    reviewThreads(first:100, after:$after){ pageInfo{hasNextPage endCursor}
      nodes{ id isResolved isOutdated comments(first:100){ nodes{ databaseId } } } } } } }`

// ReviewThreads lists the review threads of a PR with their resolved state.
func (r *Repo) ReviewThreads(ctx context.Context, number int) ([]Thread, error) {
	var out []Thread
	after := ""
	for {
		vars := map[string]any{"owner": r.Owner, "name": r.Name, "number": number}
		if after != "" {
			vars["after"] = after
		}
		var res struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							IsOutdated bool   `json:"isOutdated"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int64 `json:"databaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		if err := r.c.GraphQL(ctx, r.Owner, r.Name, threadsQuery, vars, &res); err != nil {
			return nil, err
		}
		rt := res.Repository.PullRequest.ReviewThreads
		for _, n := range rt.Nodes {
			t := Thread{ID: n.ID, Resolved: n.IsResolved, Outdated: n.IsOutdated}
			for _, c := range n.Comments.Nodes {
				t.CommentIDs = append(t.CommentIDs, c.DatabaseID)
			}
			out = append(out, t)
		}
		if !rt.PageInfo.HasNextPage {
			return out, nil
		}
		after = rt.PageInfo.EndCursor
	}
}

// ResolveThread marks a review thread resolved.
func (r *Repo) ResolveThread(ctx context.Context, threadID string) error {
	return r.c.GraphQL(ctx, r.Owner, r.Name, `mutation($id:ID!){ resolveReviewThread(input:{threadId:$id}){ thread{ id isResolved } } }`,
		map[string]any{"id": threadID}, nil)
}

// MergedPRs lists recently merged PR numbers (for learning review style).
func (r *Repo) MergedPRs(ctx context.Context, limit int) ([]PullRequest, error) {
	prs, err := paged[PullRequest](ctx, r.c, r.Owner, r.Name, r.path("/pulls"), map[string][]string{"state": {"closed"}, "sort": {"updated"}, "direction": {"desc"}}, "")
	if err != nil {
		return nil, err
	}
	var out []PullRequest
	for _, p := range prs {
		if p.Merged || p.MergedAt != nil {
			out = append(out, p)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
