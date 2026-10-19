package review

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

var severityBadge = map[Severity]string{Low: "🟢 low", Medium: "🟡 medium", High: "🟠 high", Critical: "🔴 critical"}

// RenderComment formats a finding as an inline review comment.
func RenderComment(f Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** · _%s_\n\n**%s**\n\n%s", severityBadge[f.Severity], f.Category, f.Title, strings.TrimSpace(f.Description))
	if f.Suggestion != "" {
		fmt.Fprintf(&b, "\n\n```suggestion\n%s\n```", strings.TrimRight(f.Suggestion, "\n"))
	}
	b.WriteString("\n\n" + Marker(f.ID))
	return b.String()
}

// RenderReviewBody summarises a review: counts by severity and the
// reviewer's own summary.
func RenderReviewBody(summary string, findings []Finding) string {
	counts := map[Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	var parts []string
	for _, s := range []Severity{Critical, High, Medium, Low} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	var b strings.Builder
	b.WriteString("### Automated review\n\n")
	if s := strings.TrimSpace(summary); s != "" {
		b.WriteString(s + "\n\n")
	}
	if len(parts) > 0 {
		b.WriteString("**Findings:** " + strings.Join(parts, ", ") + "\n")
	} else {
		b.WriteString("No new findings.\n")
	}
	return b.String()
}

// Assessment is the reviewer's judgement on whether the PR could be approved.
type Assessment struct {
	// Approve is the reviewer's verdict; it still needs the repository's
	// approval mode to allow acting on it.
	Approve bool   `json:"approve"`
	Reason  string `json:"reason,omitempty"`
}

// PublishRequest is the input of Publish.
type PublishRequest struct {
	// Ranking lists every NEW open finding id, most important first.
	Ranking    []string
	Summary    string
	Assessment *Assessment
}

// PublishResult reports the outcome.
type PublishResult struct {
	ReviewID int64  `json:"review_id,omitempty"`
	Event    string `json:"event,omitempty"`
	Posted   int    `json:"posted"`
	DryRun   bool   `json:"dry_run,omitempty"`
	// SkippedEmptyReReview is set when a re-review had nothing new to say.
	SkippedEmptyReReview bool `json:"skipped_empty_re_review,omitempty"`
	// ExpectedFindingIDs is returned when Ranking did not match the new open
	// findings: rank exactly these and call again.
	ExpectedFindingIDs []string `json:"expected_finding_ids,omitempty"`
	// UnresolvableFindings are findings whose anchor is no longer in the diff
	// (e.g. the code changed): resolve or re-anchor them, then call again.
	UnresolvableFindings []string `json:"unresolvable_findings,omitempty"`
	ApprovalNote         string   `json:"approval_note,omitempty"`
	// Warnings are non-fatal problems after the review was posted (for
	// example, failing to link comments back to findings).
	Warnings []string `json:"warnings,omitempty"`
}

// OK reports whether a review was actually posted.
func (r PublishResult) OK() bool { return r.ReviewID != 0 && !r.DryRun && !r.SkippedEmptyReReview }

// approvalAllowed applies the repository's approval mode: approval needs the
// mode "approve", a positive assessment, no open finding of medium severity
// or higher, and a head that did not move since the review started.
func approvalAllowed(mode ApprovalMode, a *Assessment, open []Finding, headAtStart, headNow string) (bool, string) {
	switch {
	case a == nil || !a.Approve:
		return false, ""
	case mode != ApprovalOn:
		return false, fmt.Sprintf("assessment was positive but approval mode is %q; posting as a comment", orDefault(string(mode), "dry_run"))
	case headAtStart != headNow:
		return false, "the PR head moved during the review; not approving"
	}
	for _, f := range open {
		if f.Severity.Rank() >= Medium.Rank() {
			return false, "open findings of medium severity or higher block approval"
		}
	}
	return true, ""
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Publish posts the review: new open findings become inline comments in one
// GitHub review, ordered by the reviewer's ranking.
func (s *Session) Publish(ctx context.Context, req PublishRequest) (PublishResult, error) {
	all, err := s.Findings.List(ctx, s.PR)
	if err != nil {
		return PublishResult{}, err
	}
	var fresh, stillOpen []Finding
	for _, f := range all {
		if f.Status != Open {
			continue
		}
		stillOpen = append(stillOpen, f)
		if f.Surface == NotSurfaced {
			fresh = append(fresh, f)
		}
	}
	expected := make([]string, len(fresh))
	for i, f := range fresh {
		expected[i] = f.ID
	}
	sort.Strings(expected)
	given := slices.Clone(req.Ranking)
	sort.Strings(given)
	if !slices.Equal(expected, given) {
		return PublishResult{ExpectedFindingIDs: expected}, nil
	}
	if len(fresh) == 0 {
		// nothing new to say: never post an empty review over one already
		// published for this head, nor an empty re-review
		st, err := s.Findings.State(ctx, s.PR)
		if err != nil {
			return PublishResult{}, err
		}
		if s.ReReview() || st.LastReviewedSHA == s.HeadSHA {
			return PublishResult{SkippedEmptyReReview: true}, nil
		}
	}

	anchor, err := s.AnchorDiff(ctx)
	if err != nil {
		return PublishResult{}, err
	}
	byID := map[string]Finding{}
	for _, f := range fresh {
		byID[f.ID] = f
	}
	var ordered []Finding
	var bad []string
	for _, id := range req.Ranking {
		f := byID[id]
		if !anchor.Contains(f.File, f.StartLine, f.EndLine, f.Side) {
			bad = append(bad, id)
			continue
		}
		ordered = append(ordered, f)
	}
	if len(bad) > 0 {
		return PublishResult{UnresolvableFindings: bad}, nil
	}
	if s.DryRun {
		return PublishResult{DryRun: true, Posted: len(ordered)}, nil
	}

	event := "COMMENT"
	res := PublishResult{}
	currentHead := s.HeadSHA
	if req.Assessment != nil && req.Assessment.Approve && s.Approval == ApprovalOn {
		pr, err := s.Repo.GetPR(ctx, s.PR.Number)
		if err != nil {
			return PublishResult{}, fmt.Errorf("checking the PR head before approving: %w", err)
		}
		currentHead = pr.Head.SHA
	}
	if ok, note := approvalAllowed(s.Approval, req.Assessment, stillOpen, s.HeadSHA, currentHead); ok {
		event = "APPROVE"
	} else {
		res.ApprovalNote = note
	}
	rr := github.ReviewRequest{CommitID: s.HeadSHA, Body: RenderReviewBody(req.Summary, ordered), Event: event}
	for _, f := range ordered {
		c := github.ReviewComment{Path: f.File, Body: RenderComment(f), Line: f.EndLine, Side: f.Side}
		if f.StartLine != f.EndLine {
			c.StartLine, c.StartSide = f.StartLine, f.Side
		}
		rr.Comments = append(rr.Comments, c)
	}
	rv, err := s.Repo.CreateReview(ctx, s.PR.Number, rr)
	if err != nil {
		return PublishResult{}, fmt.Errorf("posting the review: %w", err)
	}
	for _, f := range ordered {
		if _, err := s.Findings.Update(ctx, s.PR, f.ID, func(x *Finding) error {
			x.Surface, x.ReviewID, x.HeadSHA = Surfaced, rv.ID, s.HeadSHA
			return nil
		}); err != nil {
			return PublishResult{}, err
		}
	}
	if _, err := s.Findings.UpdateState(ctx, s.PR, func(st *PRState) {
		st.LastReviewedSHA = s.HeadSHA
		st.Reviews = append(st.Reviews, ReviewRecord{ReviewID: rv.ID, HeadSHA: s.HeadSHA, Event: event, At: time.Now().UTC()})
	}); err != nil {
		return PublishResult{}, err
	}
	res.ReviewID, res.Event, res.Posted = rv.ID, event, len(ordered)
	if err := s.linkComments(ctx, rv.ID); err != nil {
		res.Warnings = append(res.Warnings, "comments were posted but could not be linked to findings (replies will not be tracked): "+err.Error())
	}
	return res, nil
}
