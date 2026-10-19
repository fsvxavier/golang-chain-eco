package github

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// CheckRun is one CI check of a commit.
type CheckRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued | in_progress | completed
	Conclusion string `json:"conclusion"` // success | failure | ...
	HTMLURL    string `json:"html_url"`
	DetailsURL string `json:"details_url"`
	App        struct {
		Slug string `json:"slug"`
	} `json:"app"`
	CompletedAt *time.Time `json:"completed_at"`
}

// Completed reports a finished check.
func (c CheckRun) Completed() bool { return c.Status == "completed" }

// FailingConclusions are the conclusions that make a check a failure.
var FailingConclusions = map[string]bool{
	"failure": true, "timed_out": true, "cancelled": true, "action_required": true, "startup_failure": true, "stale": true,
}

// Failing reports a finished, failed check.
func (c CheckRun) Failing() bool { return c.Completed() && FailingConclusions[c.Conclusion] }

// IsActions reports a GitHub Actions check (the only kind that may be re-run).
func (c CheckRun) IsActions() bool { return c.App.Slug == "github-actions" }

// ListCheckRuns returns every check run of a commit.
func (r *Repo) ListCheckRuns(ctx context.Context, ref string) ([]CheckRun, error) {
	return paged[CheckRun](ctx, r.c, r.Owner, r.Name, r.path("/commits/%s/check-runs", escRef(ref)), nil, "check_runs")
}

// RequiredChecks returns the branch-protection required check names; a branch
// without protection (or a token that cannot read it) yields nil.
func (r *Repo) RequiredChecks(ctx context.Context, branch string) []string {
	var res struct {
		Contexts []string `json:"contexts"`
		Checks   []struct {
			Context string `json:"context"`
		} `json:"checks"`
	}
	if _, _, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/branches/%s/protection/required_status_checks", escRef(branch)), reqOpts{}, &res); err != nil {
		return nil
	}
	out := append([]string(nil), res.Contexts...)
	for _, c := range res.Checks {
		out = append(out, c.Context)
	}
	return out
}

// WorkflowRun is a GitHub Actions run.
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	RunAttempt int    `json:"run_attempt"`
}

// ListWorkflowRuns lists the Actions runs of a commit.
func (r *Repo) ListWorkflowRuns(ctx context.Context, sha string) ([]WorkflowRun, error) {
	return paged[WorkflowRun](ctx, r.c, r.Owner, r.Name, r.path("/actions/runs"), url.Values{"head_sha": {sha}}, "workflow_runs")
}

// Job is a job of a workflow run.
type Job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

// ListJobs lists the jobs of a workflow run.
func (r *Repo) ListJobs(ctx context.Context, runID int64) ([]Job, error) {
	return paged[Job](ctx, r.c, r.Owner, r.Name, r.path("/actions/runs/%d/jobs", runID), nil, "jobs")
}

// JobLog returns the log of a job, capped at maxBytes from the END (failures
// are at the end of a log).
func (r *Repo) JobLog(ctx context.Context, jobID int64, maxBytes int) (string, error) {
	_, b, err := r.c.do(ctx, r.Owner, r.Name, "GET", r.path("/actions/jobs/%d/logs", jobID), reqOpts{}, nil)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && len(b) > maxBytes {
		b = b[len(b)-maxBytes:]
	}
	return string(b), nil
}

// RerunFailedJobs re-runs only the failed jobs of a workflow run (never the
// whole run, and never a cancel or a different workflow).
func (r *Repo) RerunFailedJobs(ctx context.Context, runID int64) error {
	_, _, err := r.c.do(ctx, r.Owner, r.Name, "POST", r.path("/actions/runs/%d/rerun-failed-jobs", runID), reqOpts{}, nil)
	return err
}

// ChecksSummary classifies a commit's checks.
type ChecksSummary struct {
	// State is "none", "pending", "green" or "failing".
	State   string
	Total   int
	Failing []CheckRun
	Pending []CheckRun
}

// Summarize classifies checks. With required non-empty, only those names
// decide the state (other checks are informational), and a required check that
// has not reported yet counts as pending.
func Summarize(checks []CheckRun, required []string) ChecksSummary {
	req := map[string]bool{}
	for _, n := range required {
		req[n] = true
	}
	var s ChecksSummary
	seen := map[string]bool{}
	for _, c := range checks {
		if len(req) > 0 && !req[c.Name] {
			continue
		}
		seen[c.Name] = true
		s.Total++
		switch {
		case c.Failing():
			s.Failing = append(s.Failing, c)
		case !c.Completed():
			s.Pending = append(s.Pending, c)
		}
	}
	for _, n := range required {
		if !seen[n] {
			s.Total++
			s.Pending = append(s.Pending, CheckRun{Name: n, Status: "queued"})
		}
	}
	switch {
	case s.Total == 0:
		s.State = "none"
	case len(s.Failing) > 0:
		s.State = "failing"
	case len(s.Pending) > 0:
		s.State = "pending"
	default:
		s.State = "green"
	}
	return s
}

// Names lists check names.
func Names(cs []CheckRun) string {
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}
