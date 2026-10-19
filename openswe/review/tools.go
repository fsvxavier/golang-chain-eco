package review

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

type addFindingArgs struct {
	Severity    string `json:"severity" jsonschema:"enum=low|medium|high|critical"`
	Confidence  string `json:"confidence" jsonschema:"enum=low|medium|high"`
	Category    string `json:"category" description:"e.g. bug, security, performance, concurrency, correctness, maintainability"`
	File        string `json:"file" description:"Path as it appears in the diff"`
	Title       string `json:"title" description:"One-line headline (max 120 chars)"`
	Description string `json:"description" description:"What is wrong, why it matters, how to fix it"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	Suggestion  string `json:"suggestion,omitempty" description:"Replacement code, at most 4 lines"`
	Side        string `json:"side,omitempty" jsonschema:"enum=LEFT|RIGHT,description=RIGHT (new code, default) or LEFT (removed code)"`
}

func fail(format string, a ...any) map[string]any {
	return map[string]any{"success": false, "error": fmt.Sprintf(format, a...)}
}

// Tools returns the reviewer's tools bound to this session:
// fetch_review_diff, add_finding, list_findings, update_finding,
// resolve_finding_thread and publish_review.
func (s *Session) Tools() []tools.Tool {
	return []tools.Tool{s.fetchDiffTool(), s.addFindingTool(), s.listTool(), s.updateTool(), s.resolveThreadTool(), s.publishTool()}
}

func (s *Session) fetchDiffTool() tools.Tool {
	return tools.New("fetch_review_diff",
		"Fetch the diff to review. On a first review it is the whole PR; on a re-review only the commits since the last review. It is written to a file (when a sandbox is attached) so you can grep and read it in parts.",
		func(ctx context.Context, _ struct{}) (any, error) {
			d, text, err := s.Diff(ctx)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			files, added := d.Stats()
			out := map[string]any{"files": d.Paths(), "changed_files": files, "added_lines": added, "re_review": s.ReReview(), "head_sha": s.HeadSHA}
			if s.DiffWriter != nil && text != "" {
				if err := s.DiffWriter(ctx, s.diffPath(), text); err != nil {
					return nil, tools.Errorf("writing the diff file: %v", err)
				}
				out["path"] = s.diffPath()
			} else if text != "" {
				const max = 60000
				if len(text) > max {
					text = text[:max] + "\n[diff truncated]"
				}
				out["diff"] = text
			}
			return out, nil
		})
}

func (s *Session) addFindingTool() tools.Tool {
	return tools.New("add_finding",
		"Record one review finding. It must anchor to lines inside the PR diff and be something you can defend. Duplicates of an open finding are merged.",
		func(ctx context.Context, a addFindingArgs) (any, error) {
			if a.StartLine > 0 && a.EndLine == 0 {
				a.EndLine = a.StartLine
			}
			if a.EndLine > 0 && a.StartLine == 0 {
				a.StartLine = a.EndLine
			}
			sev, conf := Severity(a.Severity), Confidence(a.Confidence)
			title := NormalizeTitle(a.Title, "")
			switch {
			case title == DefaultFindingTitle:
				return fail("title must be a non-empty generated headline"), nil
			case sev.Rank() < 0:
				return fail("invalid severity %q", a.Severity), nil
			case conf != ConfLow && conf != ConfMedium && conf != ConfHigh:
				return fail("invalid confidence %q", a.Confidence), nil
			case a.Side != "" && a.Side != "LEFT" && a.Side != "RIGHT":
				return fail("invalid side %q", a.Side), nil
			case a.StartLine <= 0 || a.EndLine < a.StartLine:
				return fail("start_line and end_line are required and end_line must be >= start_line"), nil
			case strings.TrimSpace(a.File) == "" || strings.TrimSpace(a.Description) == "":
				return fail("file and description are required"), nil
			}
			side := a.Side
			if side == "" {
				side = "RIGHT"
			}
			anchor, err := s.AnchorDiff(ctx)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			if !anchor.Contains(a.File, a.StartLine, a.EndLine, side) {
				return map[string]any{"success": false, "in_diff": false, "error": "These lines are not part of this PR's diff. Only findings anchored to lines the PR changed (or their context) are accepted. Do not re-anchor or retry."}, nil
			}
			sugg, dropped := ClipSuggestion(a.Suggestion)
			f := Finding{Severity: sev, Confidence: conf, Category: strings.TrimSpace(a.Category), File: a.File, StartLine: a.StartLine, EndLine: a.EndLine,
				Side: side, Title: title, Description: strings.TrimSpace(a.Description), Suggestion: sugg, HeadSHA: s.HeadSHA,
				DiffHunk: anchor.HunkFor(a.File, a.StartLine), Fingerprint: Fingerprint(a.File, a.Category, title)}
			saved, created, err := s.Findings.Add(ctx, s.PR, f)
			if err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "finding_id": saved.ID, "duplicate": !created, "suggestion_dropped": dropped}, nil
		})
}

type listArgs struct {
	Status string `json:"status,omitempty" jsonschema:"enum=open|resolved|dismissed,description=Filter by status (default: all)"`
}

func (s *Session) listTool() tools.Tool {
	return tools.New("list_findings", "List this PR's findings (id, status, severity, file, lines, title, surface, and human replies).",
		func(ctx context.Context, a listArgs) (any, error) {
			all, err := s.Findings.List(ctx, s.PR)
			if err != nil {
				return nil, err
			}
			out := []map[string]any{}
			for _, f := range all {
				if a.Status != "" && string(f.Status) != a.Status {
					continue
				}
				out = append(out, map[string]any{"id": f.ID, "status": f.Status, "severity": f.Severity, "category": f.Category, "file": f.File,
					"start_line": f.StartLine, "end_line": f.EndLine, "title": f.Title, "surface": f.Surface, "note": f.Note, "interactions": f.Interactions})
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		})
}

type updateArgs struct {
	ID     string `json:"id"`
	Status string `json:"status" jsonschema:"enum=open|resolved|dismissed"`
	Note   string `json:"note,omitempty" description:"Why; posted verbatim as the GitHub reply when the finding has a thread"`
}

func (s *Session) updateTool() tools.Tool {
	return tools.New("update_finding", "Change a finding's status on a re-review: resolved when the code fixed it, dismissed when it was wrong.",
		func(ctx context.Context, a updateArgs) (any, error) {
			st := Status(a.Status)
			if st != Open && st != Resolved && st != Dismissed {
				return fail("invalid status %q", a.Status), nil
			}
			f, err := s.Findings.Update(ctx, s.PR, a.ID, func(f *Finding) error {
				f.Status, f.Note = st, a.Note
				return nil
			})
			if err != nil {
				return fail("%v", err), nil
			}
			return map[string]any{"success": true, "id": f.ID, "status": f.Status}, nil
		})
}

type resolveArgs struct {
	FindingID string `json:"finding_id"`
	Status    string `json:"status" jsonschema:"enum=resolved|dismissed"`
	Note      string `json:"note" description:"Reply posted on the thread explaining the outcome"`
}

func (s *Session) resolveThreadTool() tools.Tool {
	return tools.New("resolve_finding_thread",
		"Close a finding after a human replied on its GitHub thread: posts your note as a reply, marks the finding, and resolves the thread.",
		func(ctx context.Context, a resolveArgs) (any, error) {
			st := Status(a.Status)
			if st != Resolved && st != Dismissed {
				return fail("status must be resolved or dismissed"), nil
			}
			f, err := s.Findings.Get(ctx, s.PR, a.FindingID)
			if err != nil {
				return fail("%v", err), nil
			}
			if f.CommentID != 0 && strings.TrimSpace(a.Note) != "" && !s.DryRun {
				if _, err := s.Repo.ReplyToReviewComment(ctx, s.PR.Number, f.CommentID, a.Note); err != nil {
					return fail("replying on GitHub: %v", err), nil
				}
			}
			resolved := false
			if f.ThreadID != "" && !s.DryRun {
				if err := s.Repo.ResolveThread(ctx, f.ThreadID); err != nil {
					return fail("resolving the thread: %v", err), nil
				}
				resolved = true
			}
			if _, err := s.Findings.Update(ctx, s.PR, f.ID, func(x *Finding) error {
				x.Status, x.Note = st, a.Note
				if resolved {
					x.Surface = SurfaceDone
				}
				return nil
			}); err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "thread_resolved": resolved}, nil
		})
}

type publishArgs struct {
	Ranking    []string `json:"ranking" description:"Every NEW open finding id, most important first"`
	Summary    string   `json:"summary,omitempty" description:"Two or three sentences for the review body"`
	Assessment *struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason,omitempty"`
	} `json:"assessment,omitempty" description:"Whether you would approve this PR; honoured only if the repository allows it"`
}

func (s *Session) publishTool() tools.Tool {
	return tools.New("publish_review",
		"Publish the review once, on its own (never in parallel with other tools). If it returns expected_finding_ids, rank exactly those and call again. If it returns unresolvable_findings, resolve or re-anchor them first. Only a numeric review_id (without dry_run or skipped_empty_re_review) means a review was posted.",
		func(ctx context.Context, a publishArgs) (any, error) {
			req := PublishRequest{Ranking: a.Ranking, Summary: a.Summary}
			if a.Assessment != nil {
				req.Assessment = &Assessment{Approve: a.Assessment.Approve, Reason: a.Assessment.Reason}
			}
			res, err := s.Publish(ctx, req)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			b, _ := json.Marshal(res)
			return string(b), nil
		})
}
