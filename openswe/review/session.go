package review

import (
	"context"
	"fmt"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

// ApprovalMode says what a positive approval assessment does on a repository.
type ApprovalMode string

// Approval modes. A repository with no mode set is DryRun.
const (
	ApprovalOff    ApprovalMode = "off"
	ApprovalDryRun ApprovalMode = "dry_run"
	ApprovalOn     ApprovalMode = "approve"
)

// Session is the context of one review run: which PR, at which commits, with
// which store and GitHub access. The reviewer's tools are bound to it.
type Session struct {
	Repo    *github.Repo
	PR      github.PRKey
	HeadSHA string
	BaseSHA string
	// LastReviewedSHA, when set, makes this a re-review of only the new commits.
	LastReviewedSHA string
	Findings        *Store
	// Approval decides what a positive assessment may do (default dry_run).
	Approval ApprovalMode
	// DryRun makes publish_review validate and rank but post nothing (evals).
	DryRun bool
	// DiffWriter, when set, receives the fetched diff (e.g. a sandbox file
	// writer) so the agent can grep it instead of reading it through the model.
	DiffWriter func(ctx context.Context, path, content string) error
	// DiffPath is where DiffWriter stores it (default /review/pr-<n>.diff).
	DiffPath string

	mu     sync.Mutex
	diff   *Diff
	anchor *Diff
}

// Diff returns the review diff, fetching it once: the whole PR on a first
// review, only the new commits on a re-review.
func (s *Session) Diff(ctx context.Context) (*Diff, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diff != nil {
		return s.diff, "", nil
	}
	var text string
	var err error
	if s.LastReviewedSHA != "" && s.LastReviewedSHA != s.HeadSHA {
		text, err = s.Repo.CompareDiff(ctx, s.LastReviewedSHA, s.HeadSHA)
	} else {
		text, err = s.Repo.PRDiff(ctx, s.PR.Number)
	}
	if err != nil {
		return nil, "", fmt.Errorf("fetching the PR diff: %w", err)
	}
	s.diff = ParseDiff(text)
	return s.diff, text, nil
}

// AnchorDiff is the full PR diff (base..head). Inline comments must anchor
// to it even on a re-review, because that is what GitHub validates against.
func (s *Session) AnchorDiff(ctx context.Context) (*Diff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.anchor != nil {
		return s.anchor, nil
	}
	text, err := s.Repo.PRDiff(ctx, s.PR.Number)
	if err != nil {
		return nil, fmt.Errorf("fetching the PR diff: %w", err)
	}
	s.anchor = ParseDiff(text)
	return s.anchor, nil
}

func (s *Session) diffPath() string {
	if s.DiffPath != "" {
		return s.DiffPath
	}
	return fmt.Sprintf("/review/pr-%d.diff", s.PR.Number)
}

// ReReview reports whether this run covers only new commits.
func (s *Session) ReReview() bool { return s.LastReviewedSHA != "" && s.LastReviewedSHA != s.HeadSHA }
