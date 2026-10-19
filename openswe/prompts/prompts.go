// Package prompts holds the Open SWE prompts as files: the engineer's system
// prompt, the reviewer's prompt and its subagent. Prompts live as embedded
// templates, never inline in Go, so they can be read, reviewed and replaced as
// text.
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var files embed.FS

var funcs = template.FuncMap{"join": strings.Join}

func parse(names ...string) (*template.Template, error) {
	t := template.New("").Funcs(funcs).Option("missingkey=error")
	for _, n := range names {
		b, err := files.ReadFile("templates/" + n)
		if err != nil {
			return nil, err
		}
		if _, err := t.New(n).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("prompts: %s: %w", n, err)
		}
	}
	return t, nil
}

func render(entry string, data any, names ...string) (string, error) {
	t, err := parse(names...)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.ExecuteTemplate(&b, entry, data); err != nil {
		return "", fmt.Errorf("prompts: rendering %s: %w", entry, err)
	}
	return strings.TrimSpace(b.String()) + "\n", nil
}

// Workspace is an optional named group of repositories with shared guidance.
type Workspace struct {
	Name         string
	Instructions string
	Repos        []string
}

// SystemData fills the engineer's system prompt.
type SystemData struct {
	BotName    string // default "Open SWE"
	WorkingDir string
	// Source is "github", "schedule", "cli" or anything else (generic).
	Source      string
	DefaultRepo string
	AllowedOrgs []string
	// DraftPRs says new pull requests are opened as drafts.
	DraftPRs bool
	// RepoInstructions is the repository's AGENTS.md content.
	RepoInstructions   string
	Workspace          Workspace
	CustomInstructions string
}

// System renders the engineer's system prompt.
func System(d SystemData) (string, error) {
	if d.BotName == "" {
		d.BotName = "Open SWE"
	}
	if d.WorkingDir == "" {
		return "", fmt.Errorf("prompts: SystemData.WorkingDir is required")
	}
	return render("system.md.tmpl", d, "system.md.tmpl", "sections.md.tmpl")
}

// ScopedDoc is an AGENTS.md that applies to a directory.
type ScopedDoc struct {
	Path, Scope, Content string
}

// ReviewerData fills the reviewer's system prompt.
type ReviewerData struct {
	Owner, Repo string
	PR          int
	WorkingDir  string
	// ReReview makes it a reconcile-then-review-new-commits run.
	ReReview         bool
	RepoCheckoutNote string
	// HistoricalGuidance adds the default review philosophy.
	HistoricalGuidance bool
	ApprovalPolicy     bool
	OrgGuidelines      string
	RepoStyle          string
	AgentsMD           string
	ScopedAgentsMD     []ScopedDoc
	// FindingReply, when set, turns the run into a focused reassessment of one
	// finding after a human replied on its thread.
	FindingReply string
}

// Reviewer renders the reviewer's system prompt.
func Reviewer(d ReviewerData) (string, error) {
	if d.Owner == "" || d.Repo == "" || d.PR <= 0 {
		return "", fmt.Errorf("prompts: ReviewerData needs Owner, Repo and PR")
	}
	if d.WorkingDir == "" {
		d.WorkingDir = "/workspace"
	}
	return render("reviewer.md.tmpl", d, "reviewer.md.tmpl")
}

// Subagent is the reviewer's investigation subagent prompt.
func Subagent() (string, error) { return render("subagent.md.tmpl", nil, "subagent.md.tmpl") }
