package openswe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
	"github.com/fsvxavier/golang-chain-eco/openswe/guards"
	"github.com/fsvxavier/golang-chain-eco/openswe/prompts"
	"github.com/fsvxavier/golang-chain-eco/openswe/review"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

// ErrNoReview means a review run ended without publishing one.
var ErrNoReview = errors.New("openswe: the reviewer finished without publishing a review")

// ReviewerRunner runs pull-request reviews (triggers.Runner for KindReviewer).
// Each run builds a Session for the PR at its current head, reconciles
// findings with what happened on GitHub since the last review, and gives the
// reviewer agent read-only access plus the review tools.
type ReviewerRunner struct {
	Config  Config
	Reviews *review.Store
	Styles  *review.StyleStore
	// OrgGuidelines apply to every repository this reviewer covers.
	OrgGuidelines string
	// Prepare, when set, checks the PR head out in the thread's sandbox before
	// the review (clone/fetch, `git checkout`); without it the reviewer sees
	// the diff and whatever the sandbox already has.
	Prepare func(ctx context.Context, ex backends.Executor, pr github.PullRequest, key github.PRKey) error
	// Announce posts a short "review in progress" comment and removes it when
	// the run ends.
	Announce bool
	// OnPublished receives the outcome of each run (for metrics/notifications).
	OnPublished func(t triggers.Task, headSHA string)
}

// Run implements triggers.Runner.
func (r ReviewerRunner) Run(ctx context.Context, t triggers.Task) error {
	cfg := r.Config
	cfg.defaults()
	if cfg.ReviewerModel == nil || cfg.GitHub == nil || r.Reviews == nil {
		return errors.New("openswe: ReviewerRunner needs Config.ReviewerModel/Model, Config.GitHub and Reviews")
	}
	be, err := cfg.backend()
	if err != nil {
		return err
	}
	key := github.PRKey{Owner: t.Owner, Repo: t.Repo, Number: t.Number}
	repo := cfg.GitHub.Repo(key.Owner, key.Repo)
	pr, err := repo.GetPR(ctx, key.Number)
	if err != nil {
		return fmt.Errorf("reading %s: %w", key, err)
	}
	if pr.State != "open" {
		return nil // nothing to review on a closed PR
	}
	head := pr.Head.SHA

	state, err := r.Reviews.State(ctx, key)
	if err != nil {
		return err
	}
	style := review.Style{}
	if r.Styles != nil {
		if style, err = r.Styles.Get(ctx, key.Owner, key.Repo); err != nil {
			return err
		}
	}
	session := &review.Session{Repo: repo, PR: key, HeadSHA: head, BaseSHA: pr.Base.SHA, Findings: r.Reviews, Approval: style.Approval}
	if wr, ok := be.(interface {
		Write(context.Context, string, string) (backends.FilesUpdate, error)
	}); ok {
		session.DiffWriter = func(ctx context.Context, p, content string) error {
			_, err := wr.Write(ctx, p, content)
			if errors.Is(err, backends.ErrExists) { // a previous run left it: replace it
				if ex, ok := backends.AsExecutor(be); ok {
					_, _ = ex.Execute(ctx, "rm -f '"+strings.ReplaceAll(p, "'", `'\''`)+"'", time.Minute)
					_, err = wr.Write(ctx, p, content)
				}
			}
			return err
		}
	}
	reReview, _ := t.Metadata["re_review"].(bool)
	if reReview && state.LastReviewedSHA != "" && state.LastReviewedSHA != head {
		session.LastReviewedSHA = state.LastReviewedSHA
	}

	if r.Prepare != nil {
		if ex, ok := backends.AsExecutor(be); ok {
			if err := r.Prepare(withThread(ctx, t.ThreadID), ex, pr, key); err != nil {
				return fmt.Errorf("preparing the checkout: %w", err)
			}
		}
	}

	// what changed on GitHub since the last review
	var reconcile string
	if existing, err := r.Reviews.List(ctx, key); err == nil && len(existing) > 0 {
		rep, err := session.Reconcile(ctx)
		if err != nil {
			return fmt.Errorf("reconciling findings with GitHub: %w", err)
		}
		reconcile = describeReconcile(rep)
	}

	agentsMD, _ := repo.FileContent(ctx, "AGENTS.md", pr.Base.Ref)
	if len(agentsMD) == 0 {
		agentsMD, _ = repo.FileContent(ctx, "CLAUDE.md", pr.Base.Ref)
	}
	findingReply, _ := t.Metadata["finding_reply"].(string)
	if reconcile != "" {
		findingReply = strings.TrimSpace(findingReply + "\n\n" + reconcile)
	}
	sys, err := prompts.Reviewer(prompts.ReviewerData{Owner: key.Owner, Repo: key.Repo, PR: key.Number, WorkingDir: cfg.WorkingDir,
		ReReview: session.ReReview(), HistoricalGuidance: true, ApprovalPolicy: style.Approval == review.ApprovalOn || style.Approval == review.ApprovalDryRun,
		OrgGuidelines: r.OrgGuidelines, RepoStyle: style.CustomPrompt, AgentsMD: string(agentsMD), FindingReply: findingReply})
	if err != nil {
		return err
	}
	sub, err := prompts.Subagent()
	if err != nil {
		return err
	}

	// read-only workspace: the diff file under /review is the only thing the reviewer may write
	var announcement int64
	if r.Announce {
		if id, err := repo.Comment(ctx, key.Number, "🔎 Automated review in progress…"); err == nil {
			announcement = id
		}
	}
	defer func() {
		if announcement != 0 {
			_ = repo.DeleteComment(context.WithoutCancel(ctx), announcement)
		}
	}()

	g, err := deepagents.Create(deepagents.Options{
		Model:        cfg.ReviewerModel,
		Tools:        session.Tools(),
		SystemPrompt: sys,
		Backend:      be,
		Permissions:  []backends.Permission{{Operations: []string{backends.OpWrite}, Paths: []string{"/review/**"}}, {Operations: []string{backends.OpWrite}, Deny: true, Paths: []string{"/**"}}},
		Middleware:   cfg.Guards(be, true),
		Checkpointer: cfg.Checkpointer,
		SubAgents: []deepagents.SubAgentSpec{{
			Name:         "reviewer-helper",
			Description:  "Investigates one question for the review (reads code, traces a call path). Returns facts with file paths and line numbers. Cannot record findings or publish.",
			SystemPrompt: sub,
			Tools:        []tools.Tool{},
			Middleware:   []agents.Middleware{guards.ToolErrorHandler{}, guards.ReadOnlyGuard{}, guards.RepairOrphanedToolCalls{}},
		}},
	})
	if err != nil {
		return err
	}
	msgs := t.Messages
	if len(msgs) == 0 {
		msgs = []messages.Message{messages.Human(fmt.Sprintf("Review pull request %s (head %s).", key, head))}
	}
	rc := runnables.Config{Configurable: map[string]any{"thread_id": t.ThreadID, "source": "github", "repo": t.Repo, "owner": t.Owner, "number": t.Number, "head_sha": head}, RecursionLimit: cfg.RecursionLimit}
	if _, err := g.Invoke(runnables.WithConfig(ctx, rc), graph.State{"messages": msgs}); err != nil {
		return err
	}
	after, err := r.Reviews.State(ctx, key)
	if err != nil {
		return err
	}
	if after.LastReviewedSHA != head && findingReplyOnly(t) == "" {
		return ErrNoReview
	}
	if r.OnPublished != nil {
		r.OnPublished(t, head)
	}
	return nil
}

// findingReplyOnly returns the finding id when the run was a focused
// reassessment of one reply (which publishes nothing).
func findingReplyOnly(t triggers.Task) string {
	id, _ := t.Metadata["finding_id"].(string)
	return id
}

func withThread(ctx context.Context, thread string) context.Context {
	return runnables.WithConfig(ctx, runnables.Config{Configurable: map[string]any{"thread_id": thread}})
}

func describeReconcile(rep review.ReconcileReport) string {
	var b strings.Builder
	if len(rep.ResolvedOnGitHub) > 0 {
		fmt.Fprintf(&b, "People resolved these finding threads on GitHub (already closed here): %s.\n", strings.Join(rep.ResolvedOnGitHub, ", "))
	}
	for id, replies := range rep.NewReplies {
		for _, rp := range replies {
			fmt.Fprintf(&b, "New human reply on finding %s from @%s:\n%s\n", id, rp.Author, triggers.FenceUntrusted("reply", rp.Author, rp.Body))
		}
	}
	return strings.TrimSpace(b.String())
}
