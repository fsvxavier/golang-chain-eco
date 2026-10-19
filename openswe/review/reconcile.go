package review

import (
	"context"
	"time"
)

// linkComments records, for each finding of a review, the id of its inline
// comment and its review thread, found through the hidden marker.
func (s *Session) linkComments(ctx context.Context, reviewID int64) error {
	comments, err := s.Repo.ListReviewComments(ctx, s.PR.Number)
	if err != nil {
		return err
	}
	threads, terr := s.Repo.ReviewThreads(ctx, s.PR.Number)
	threadOf := map[int64]string{}
	if terr == nil {
		for _, t := range threads {
			for _, id := range t.CommentIDs {
				threadOf[id] = t.ID
			}
		}
	}
	for _, c := range comments {
		if c.ReviewID != reviewID {
			continue
		}
		fid, ok := ParseMarker(c.Body)
		if !ok {
			continue
		}
		cid, tid := c.ID, threadOf[c.ID]
		if _, err := s.Findings.Update(ctx, s.PR, fid, func(f *Finding) error {
			f.CommentID, f.ThreadID = cid, tid
			return nil
		}); err != nil {
			return err
		}
	}
	return terr
}

// ReconcileReport says what changed on GitHub since the review was posted.
type ReconcileReport struct {
	// ResolvedOnGitHub are findings whose thread a human resolved.
	ResolvedOnGitHub []string `json:"resolved_on_github,omitempty"`
	// NewReplies are findings with human replies not seen before; the agent
	// should reassess them (update_finding / resolve_finding_thread).
	NewReplies map[string][]Interaction `json:"new_replies,omitempty"`
}

// Reconcile brings findings in line with their GitHub threads: a thread
// resolved by a person closes the finding, and replies are recorded as
// interactions so the reviewer can answer or dismiss them.
func (s *Session) Reconcile(ctx context.Context) (ReconcileReport, error) {
	rep := ReconcileReport{NewReplies: map[string][]Interaction{}}
	all, err := s.Findings.List(ctx, s.PR)
	if err != nil {
		return rep, err
	}
	comments, err := s.Repo.ListReviewComments(ctx, s.PR.Number)
	if err != nil {
		return rep, err
	}
	threads, err := s.Repo.ReviewThreads(ctx, s.PR.Number)
	if err != nil {
		return rep, err
	}
	threadByComment := map[int64]int{}
	for i, t := range threads {
		for _, id := range t.CommentIDs {
			threadByComment[id] = i
		}
	}
	for _, f := range all {
		if f.CommentID == 0 || f.Surface == NotSurfaced {
			continue
		}
		known := map[int64]bool{}
		for _, in := range f.Interactions {
			known[in.CommentID] = true
		}
		var fresh []Interaction
		for _, c := range comments {
			if c.ID == f.CommentID || known[c.ID] {
				continue
			}
			ti, inThread := threadByComment[c.ID]
			ft, fOK := threadByComment[f.CommentID]
			if !(c.InReplyToID == f.CommentID || (inThread && fOK && ti == ft)) {
				continue
			}
			if _, isMarked := ParseMarker(c.Body); isMarked {
				continue
			}
			kind := "human_reply"
			if c.User.Type == "Bot" {
				kind = "bot_reply"
			}
			fresh = append(fresh, Interaction{Kind: kind, Author: c.User.Login, Body: c.Body, CommentID: c.ID, At: time.Now().UTC()})
		}
		threadResolved := false
		if ti, ok := threadByComment[f.CommentID]; ok {
			threadResolved = threads[ti].Resolved
		}
		if len(fresh) == 0 && !(threadResolved && f.Status == Open) {
			continue
		}
		if _, err := s.Findings.Update(ctx, s.PR, f.ID, func(x *Finding) error {
			x.Interactions = append(x.Interactions, fresh...)
			if threadResolved && x.Status == Open {
				x.Status, x.Surface, x.Note = Resolved, SurfaceDone, "thread resolved on GitHub"
			}
			return nil
		}); err != nil {
			return rep, err
		}
		if threadResolved && f.Status == Open {
			rep.ResolvedOnGitHub = append(rep.ResolvedOnGitHub, f.ID)
		}
		var human []Interaction
		for _, in := range fresh {
			if in.Kind == "human_reply" {
				human = append(human, in)
			}
		}
		if len(human) > 0 && !(threadResolved && f.Status == Open) {
			rep.NewReplies[f.ID] = human
		}
	}
	return rep, nil
}
