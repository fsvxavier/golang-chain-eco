package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func verifySig(secret, header string, body []byte) bool {
	if secret == "" || !hasPrefixFold(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(header), "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Sign computes the X-Hub-Signature-256 value for a payload (tests, tools).
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Event is a parsed webhook delivery, reduced to what the workflows need.
type Event struct {
	// Type is the X-GitHub-Event header ("issues", "pull_request", ...).
	Type   string
	Action string
	// Delivery is the X-GitHub-Delivery id (unique per delivery).
	Delivery string

	Owner, Repo string
	Private     bool
	Sender      string
	SenderIsBot bool

	Number    int // issue or PR number
	Title     string
	Body      string
	Draft     bool
	Merged    bool
	HeadSHA   string
	HeadRef   string
	BaseRef   string
	State     string
	URL       string
	Author    string
	Comment   *Comment
	CheckName string
	// Conclusion and Status of a check_run / check_suite.
	Conclusion string
	Status     string
	// Ref and After describe a push.
	Ref, After string
	// PRNumbers lists pull requests a check run belongs to.
	PRNumbers []int
	// IsPullRequestComment marks issue_comment events on a PR.
	IsPullRequestComment bool
}

// Comment is an issue or review comment.
type Comment struct {
	ID        int64
	Body      string
	Author    string
	IsBot     bool
	InReplyTo int64
	Path      string
	Line      int
}

// FullName is owner/repo.
func (e Event) FullName() string { return e.Owner + "/" + e.Repo }

type user struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type rawPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Draft  bool   `json:"draft"`
	Merged bool   `json:"merged"`
	State  string `json:"state"`
	URL    string `json:"html_url"`
	User   user   `json:"user"`
	Head   struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type rawComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	User      user   `json:"user"`
	InReplyTo int64  `json:"in_reply_to_id"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
}

// ParseEvent decodes a delivery body.
func ParseEvent(eventType, delivery string, body []byte) (Event, error) {
	var p struct {
		Action     string `json:"action"`
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Repository struct {
			Name    string `json:"name"`
			Private bool   `json:"private"`
			Owner   struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		Sender user `json:"sender"`
		Issue  *struct {
			Number      int             `json:"number"`
			Title       string          `json:"title"`
			Body        string          `json:"body"`
			State       string          `json:"state"`
			URL         string          `json:"html_url"`
			User        user            `json:"user"`
			PullRequest json.RawMessage `json:"pull_request"`
		} `json:"issue"`
		PullRequest *rawPR      `json:"pull_request"`
		Comment     *rawComment `json:"comment"`
		CheckRun    *struct {
			Name         string `json:"name"`
			Status       string `json:"status"`
			Conclusion   string `json:"conclusion"`
			HeadSHA      string `json:"head_sha"`
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
			CheckSuite struct {
				HeadBranch string `json:"head_branch"`
			} `json:"check_suite"`
		} `json:"check_run"`
		CheckSuite *struct {
			Status       string `json:"status"`
			Conclusion   string `json:"conclusion"`
			HeadSHA      string `json:"head_sha"`
			HeadBranch   string `json:"head_branch"`
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"check_suite"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, fmt.Errorf("github: invalid %s payload: %w", eventType, err)
	}
	e := Event{Type: eventType, Action: p.Action, Delivery: delivery,
		Owner: p.Repository.Owner.Login, Repo: p.Repository.Name, Private: p.Repository.Private,
		Sender: p.Sender.Login, SenderIsBot: p.Sender.Type == "Bot" || strings.HasSuffix(p.Sender.Login, "[bot]"),
		Ref: p.Ref, After: p.After}
	if p.Issue != nil {
		e.Number, e.Title, e.Body, e.State, e.URL, e.Author = p.Issue.Number, p.Issue.Title, p.Issue.Body, p.Issue.State, p.Issue.URL, p.Issue.User.Login
		e.IsPullRequestComment = len(p.Issue.PullRequest) > 0 && string(p.Issue.PullRequest) != "null"
	}
	if pr := p.PullRequest; pr != nil {
		e.Number, e.Title, e.Body, e.Draft, e.Merged, e.State, e.URL, e.Author = pr.Number, pr.Title, pr.Body, pr.Draft, pr.Merged, pr.State, pr.URL, pr.User.Login
		e.HeadSHA, e.HeadRef, e.BaseRef = pr.Head.SHA, pr.Head.Ref, pr.Base.Ref
		e.IsPullRequestComment = eventType != "pull_request"
	}
	if c := p.Comment; c != nil {
		e.Comment = &Comment{ID: c.ID, Body: c.Body, Author: c.User.Login, IsBot: c.User.Type == "Bot" || strings.HasSuffix(c.User.Login, "[bot]"),
			InReplyTo: c.InReplyTo, Path: c.Path, Line: c.Line}
	}
	if cr := p.CheckRun; cr != nil {
		e.CheckName, e.Status, e.Conclusion, e.HeadSHA, e.HeadRef = cr.Name, cr.Status, cr.Conclusion, cr.HeadSHA, cr.CheckSuite.HeadBranch
		for _, pr := range cr.PullRequests {
			e.PRNumbers = append(e.PRNumbers, pr.Number)
		}
	}
	if cs := p.CheckSuite; cs != nil {
		e.Status, e.Conclusion, e.HeadSHA, e.HeadRef = cs.Status, cs.Conclusion, cs.HeadSHA, cs.HeadBranch
		for _, pr := range cs.PullRequests {
			e.PRNumbers = append(e.PRNumbers, pr.Number)
		}
	}
	if e.Owner == "" || e.Repo == "" {
		return e, fmt.Errorf("github: %s payload has no repository", eventType)
	}
	return e, nil
}
