package prompts_test

import (
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/openswe/prompts"
)

func TestSystemPromptSections(t *testing.T) {
	got, err := prompts.System(prompts.SystemData{WorkingDir: "/work", Source: "github", DraftPRs: true,
		AllowedOrgs: []string{"acme", "acme-labs"}, RepoInstructions: "Run make lint.", DefaultRepo: "acme/api",
		Workspace: prompts.Workspace{Name: "payments", Repos: []string{"acme/api", "acme/web"}, Instructions: "Use gofumpt."}, CustomInstructions: "Be brief."})
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{
		"You are Open SWE", "`/work`", "Never approve pull requests as yourself", "open_pull_request", "new PRs are drafts",
		"Never mark a draft PR ready", "request_pr_review", "manage_baby_sit", "Untrusted content", "github_comment",
		"acme, acme-labs", "Run make lint.", "Workspace (payments)", "acme/api, acme/web", "Use gofumpt.", "Be brief.", "work in `acme/api`",
		"`--force-with-lease`", ".github/workflows/",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("system prompt lacks %q", must)
		}
	}
	if strings.Contains(got, "{{") || strings.Contains(got, "<no value>") {
		t.Fatalf("unrendered template text:\n%s", got)
	}
	// source selects the guidance; sections are optional
	sched, _ := prompts.System(prompts.SystemData{WorkingDir: "/w", Source: "schedule"})
	if !strings.Contains(sched, "nobody is watching") || strings.Contains(sched, "github_comment") || strings.Contains(sched, "Repository scope") || strings.Contains(sched, "Workspace") || strings.Contains(sched, "new PRs are drafts") {
		t.Fatalf("schedule prompt:\n%s", sched)
	}
	cli, _ := prompts.System(prompts.SystemData{WorkingDir: "/w", Source: "cli"})
	if !strings.Contains(cli, "own machine") {
		t.Fatal("cli source")
	}
	generic, _ := prompts.System(prompts.SystemData{WorkingDir: "/w", Source: "whatever", BotName: "Robo"})
	if !strings.Contains(generic, "You are Robo") || !strings.Contains(generic, "Do not assume a particular channel") {
		t.Fatal("generic source")
	}
	if _, err := prompts.System(prompts.SystemData{}); err == nil {
		t.Fatal("working dir is required")
	}
}

func TestReviewerPromptVariants(t *testing.T) {
	base := prompts.ReviewerData{Owner: "acme", Repo: "api", PR: 42, HistoricalGuidance: true}
	first, err := prompts.Reviewer(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"`acme/api#42`", "fetch_review_diff", "publish_review", "expected_finding_ids", "read-only", "How to review", "/workspace"} {
		if !strings.Contains(first, must) {
			t.Errorf("lacks %q", must)
		}
	}
	for _, mustNot := range []string{"RE-REVIEW", "Approval assessment", "AGENTS.md", "replied to one of your findings", "Organization-wide"} {
		if strings.Contains(first, mustNot) {
			t.Errorf("first review must not include %q", mustNot)
		}
	}
	full, err := prompts.Reviewer(prompts.ReviewerData{Owner: "acme", Repo: "api", PR: 42, ReReview: true, ApprovalPolicy: true,
		OrgGuidelines: "No secrets in logs.", RepoStyle: "Prefer table tests.", AgentsMD: "Use gofmt.",
		ScopedAgentsMD: []prompts.ScopedDoc{{Path: "pkg/AGENTS.md", Scope: "pkg", Content: "No globals."}},
		FindingReply:   "ana: this is test code"})
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"RE-REVIEW", "update_finding", "Approval assessment", "No secrets in logs.", "Prefer table tests.", "Use gofmt.",
		"`pkg/AGENTS.md` (scope: `pkg/`)", "No globals.", "ana: this is test code", "resolve_finding_thread"} {
		if !strings.Contains(full, must) {
			t.Errorf("lacks %q", must)
		}
	}
	if strings.Contains(full, "How to review") {
		t.Error("historical guidance is opt-in")
	}
	if _, err := prompts.Reviewer(prompts.ReviewerData{}); err == nil {
		t.Fatal("PR identity required")
	}
	sub, err := prompts.Subagent()
	if err != nil || !strings.Contains(sub, "Do not call finding or publication tools") {
		t.Fatalf("%q %v", sub, err)
	}
}
