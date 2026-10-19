package review

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

// Style is a repository's review profile.
type Style struct {
	FullName string `json:"full_name"`
	// CustomPrompt is the repository-specific review guidance, hand-written
	// or learned from past reviews with LearnStyle.
	CustomPrompt string       `json:"custom_prompt,omitempty"`
	Approval     ApprovalMode `json:"approval_mode,omitempty"`
	// Enabled turns automatic reviews on for the repository (off by default).
	Enabled bool `json:"enabled"`
	// ReviewDrafts allows automatic review of draft PRs.
	ReviewDrafts bool `json:"review_drafts,omitempty"`
}

// StyleStore persists styles per repository.
type StyleStore struct {
	S  store.Store
	mu sync.Mutex
}

func styleNS() []string { return []string{"openswe", "reviewstyle"} }

func styleKey(owner, repo string) string { return strings.ToLower(owner + "/" + repo) }

// Get returns the style of a repository; the zero style (disabled) if unset.
func (s *StyleStore) Get(ctx context.Context, owner, repo string) (Style, error) {
	it, err := s.S.Get(ctx, styleNS(), styleKey(owner, repo))
	if err != nil || it == nil {
		return Style{FullName: styleKey(owner, repo)}, err
	}
	var st Style
	return st, fromMap(it.Value, &st)
}

// Put saves a style.
func (s *StyleStore) Put(ctx context.Context, st Style) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.FullName = strings.ToLower(st.FullName)
	m, err := toMap(st)
	if err != nil {
		return err
	}
	return s.S.Put(ctx, styleNS(), st.FullName, m)
}

// LearnStyle distills a repository's review habits from its merged PRs: what
// human reviewers comment on, how they phrase it, what they ignore. The result
// is guidance for the reviewer prompt ("learns repository-specific review
// preferences"). Only human, non-reply comments are used. It reads at most
// maxPRs merged PRs.
func LearnStyle(ctx context.Context, model models.ChatModel, repo *github.Repo, maxPRs int) (string, error) {
	prs, err := repo.MergedPRs(ctx, maxPRs)
	if err != nil {
		return "", err
	}
	var samples []string
	for _, pr := range prs {
		comments, err := repo.ListReviewComments(ctx, pr.Number)
		if err != nil {
			return "", err
		}
		for _, c := range comments {
			if c.User.Type == "Bot" || strings.HasSuffix(c.User.Login, "[bot]") || c.InReplyToID != 0 {
				continue
			}
			if _, ours := ParseMarker(c.Body); ours {
				continue
			}
			samples = append(samples, fmt.Sprintf("- (%s, PR #%d) %s", c.Path, pr.Number, oneLine(c.Body, 400)))
		}
	}
	if len(samples) == 0 {
		return "", fmt.Errorf("review: no human review comments found in the last %d merged PRs of %s/%s", maxPRs, repo.Owner, repo.Name)
	}
	if len(samples) > 200 {
		samples = samples[:200]
	}
	prompt := "Below are review comments human maintainers left on merged pull requests of one repository.\n" +
		"Write concise review guidelines for an automated reviewer so that it comments like these maintainers: " +
		"what they care about (and what they never mention), the conventions they enforce, the tone, and typical severity. " +
		"Use bullet points, no preamble. Do not invent rules the comments do not support.\n\n<comments>\n" + strings.Join(samples, "\n") + "\n</comments>"
	out, err := model.Generate(ctx, []messages.Message{messages.Human(prompt)})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text()), nil
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
