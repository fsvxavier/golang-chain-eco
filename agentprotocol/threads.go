package agentprotocol

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func threadCtx(ctx context.Context, id, checkpointID string) context.Context {
	c := map[string]any{"thread_id": id}
	if checkpointID != "" {
		c["checkpoint_id"] = checkpointID
	}
	return runnables.WithConfig(ctx, runnables.Config{Configurable: c})
}

// lookup returns the thread, rebuilding the record from a checkpointer when
// the server restarted but the state is still stored. The caller holds s.mu.
func (s *Server) lookup(ctx context.Context, id string) *threadRec {
	if th := s.threads[id]; th != nil {
		return th
	}
	for _, aid := range s.order {
		saver := s.agents[aid].Graph.Saver()
		if saver == nil {
			continue
		}
		cp, ok, err := saver.Get(ctx, checkpoint.Config{ThreadID: id})
		if err != nil || !ok {
			continue
		}
		th := s.newThread(id, nil)
		th.agentID = aid
		th.status = ThreadIdle
		for _, t := range cp.Next {
			if t.Interrupted {
				th.status = ThreadInterrupted
			}
		}
		return th
	}
	return nil
}

func (s *Server) newThread(id string, md map[string]any) *threadRec {
	now := time.Now().UTC()
	if md == nil {
		md = map[string]any{}
	}
	th := &threadRec{id: id, created: now, updated: now, metadata: md, status: ThreadIdle,
		hub: newHub(s.opts.ReplayBuffer), cpTimes: map[string]time.Time{}, memory: newStreamMemory()}
	if a, ok := md["agent_id"].(string); ok {
		th.agentID = a
	} else if a, ok := md["graph_id"].(string); ok {
		th.agentID = a
	}
	s.threads[id] = th
	return th
}

// view renders a thread, with its values and messages when it has an agent.
func (s *Server) view(ctx context.Context, th *threadRec, withState bool) Thread {
	s.mu.Lock()
	t := Thread{ThreadID: th.id, CreatedAt: th.created, UpdatedAt: th.updated, Metadata: cloneMap(th.metadata), Status: th.status}
	agent, _ := s.defaultAgent("", th)
	s.mu.Unlock()
	if withState && agent != nil && agent.Graph.Saver() != nil {
		if snap, err := agent.Graph.GetState(threadCtx(ctx, th.id, "")); err == nil && snap.Values != nil {
			t.Values = jsonState(snap.Values)
			t.Messages = messagesOf(snap.Values)
		}
	}
	return t
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	var req ThreadCreate
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	if req.ThreadID == "" {
		req.ThreadID = newID()
	}
	s.mu.Lock()
	if th := s.lookup(r.Context(), req.ThreadID); th != nil {
		s.mu.Unlock()
		if req.IfExists == "do_nothing" {
			writeJSON(w, 200, s.view(r.Context(), th, true))
			return
		}
		writeErr(w, 409, "conflict", "thread already exists")
		return
	}
	th := s.newThread(req.ThreadID, req.Metadata)
	if th.agentID != "" && s.agents[th.agentID] == nil {
		delete(s.threads, th.id)
		s.mu.Unlock()
		writeErr(w, 422, "invalid_request", "unknown agent in metadata")
		return
	}
	s.mu.Unlock()
	writeJSON(w, 200, s.view(r.Context(), th, false))
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	th := s.lookup(r.Context(), r.PathValue("thread_id"))
	s.mu.Unlock()
	if th == nil {
		writeErr(w, 404, "not_found", "thread not found")
		return
	}
	writeJSON(w, 200, s.view(r.Context(), th, true))
}

func (s *Server) searchThreads(w http.ResponseWriter, r *http.Request) {
	var req ThreadSearchRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	s.mu.Lock()
	recs := make([]*threadRec, 0, len(s.threads))
	for _, th := range s.threads {
		if req.Status != "" && th.status != req.Status {
			continue
		}
		if !matchMeta(th.metadata, req.Metadata) {
			continue
		}
		recs = append(recs, th)
	}
	s.mu.Unlock()
	sort.Slice(recs, func(i, j int) bool { return recs[i].updated.After(recs[j].updated) })
	out := []Thread{}
	for _, th := range recs {
		t := s.view(r.Context(), th, len(req.Values) > 0)
		if !matchMeta(t.Values, req.Values) {
			continue
		}
		out = append(out, t)
	}
	writeJSON(w, 200, page(out, req.Limit, req.Offset))
}

func (s *Server) deleteThread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("thread_id")
	s.mu.Lock()
	th := s.lookup(r.Context(), id)
	if th == nil {
		s.mu.Unlock()
		writeErr(w, 404, "not_found", "thread not found")
		return
	}
	if th.active != nil {
		th.active.cancel()
	}
	delete(s.threads, id)
	agent, _ := s.defaultAgent("", th)
	s.mu.Unlock()
	if agent != nil && agent.Graph.Saver() != nil {
		if err := agent.Graph.Saver().DeleteThread(r.Context(), id); err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) patchThread(w http.ResponseWriter, r *http.Request) {
	var req ThreadPatch
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	s.mu.Lock()
	th := s.lookup(r.Context(), r.PathValue("thread_id"))
	if th == nil {
		s.mu.Unlock()
		writeErr(w, 404, "not_found", "thread not found")
		return
	}
	if th.active != nil {
		s.mu.Unlock()
		writeErr(w, 409, "conflict", "thread has an active run")
		return
	}
	agent, aerr := s.defaultAgent("", th)
	for k, v := range req.Metadata {
		th.metadata[k] = v
	}
	th.updated = time.Now().UTC()
	s.mu.Unlock()

	if len(req.Values)+len(req.Messages) > 0 || req.Checkpoint != nil {
		if aerr != nil || agent.Graph.Saver() == nil {
			writeErr(w, 409, "conflict", "thread has no agent with a checkpointer to hold state")
			return
		}
		vals := graph.State{}
		for k, v := range req.Values {
			vals[k] = v
		}
		if len(req.Messages) > 0 {
			cur, _ := messages.Coerce(vals["messages"])
			vals["messages"] = append(cur, toGraphMessages(req.Messages)...)
		}
		cpID := ""
		if req.Checkpoint != nil {
			cpID = req.Checkpoint.CheckpointID
		}
		if _, err := agent.Graph.UpdateState(threadCtx(r.Context(), th.id, cpID), vals, ""); err != nil {
			code := 422
			if errors.Is(err, checkpoint.ErrNotFound) {
				code = 404
			}
			writeErr(w, code, "invalid_request", err.Error())
			return
		}
	}
	writeJSON(w, 200, s.view(r.Context(), th, true))
}

func (s *Server) snapshotToState(snap graph.StateSnapshot) ThreadState {
	return ThreadState{
		Checkpoint: ThreadCheckpoint{CheckpointID: snap.CheckpointID},
		Values:     jsonState(snap.Values),
		Messages:   messagesOf(snap.Values),
		Metadata:   map[string]any{"step": snap.Step, "parent_checkpoint_id": snap.ParentID, "next": snap.Next},
	}
}

func (s *Server) threadHistory(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	th := s.lookup(r.Context(), r.PathValue("thread_id"))
	var agent *AgentSpec
	var err error
	if th != nil {
		agent, err = s.defaultAgent("", th)
	}
	s.mu.Unlock()
	if th == nil {
		writeErr(w, 404, "not_found", "thread not found")
		return
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 0 {
			writeErr(w, 422, "invalid_request", "limit must be a non-negative integer")
			return
		}
		limit = n
	}
	out := []ThreadState{}
	if err == nil && agent.Graph.Saver() != nil {
		snaps, herr := agent.Graph.GetStateHistory(threadCtx(r.Context(), th.id, ""), 0)
		if herr != nil {
			writeErr(w, 500, "internal", herr.Error())
			return
		}
		before := r.URL.Query().Get("before")
		skipping := before != ""
		for _, snap := range snaps {
			if skipping {
				if snap.CheckpointID == before {
					skipping = false
				}
				continue
			}
			out = append(out, s.snapshotToState(snap))
			if len(out) >= limit {
				break
			}
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) copyThread(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	th := s.lookup(r.Context(), r.PathValue("thread_id"))
	if th == nil {
		s.mu.Unlock()
		writeErr(w, 404, "not_found", "thread not found")
		return
	}
	agent, _ := s.defaultAgent("", th)
	cp := s.newThread(newID(), cloneMap(th.metadata))
	cp.agentID = th.agentID
	cp.status = th.status
	if cp.status == ThreadBusy {
		cp.status = ThreadIdle
	}
	s.mu.Unlock()
	if agent != nil && agent.Graph.Saver() != nil {
		if err := copyCheckpoints(r.Context(), agent.Graph.Saver(), th.id, cp.id, ""); err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
	}
	writeJSON(w, 200, s.view(r.Context(), cp, true))
}

// copyCheckpoints copies a thread's history (oldest first, optionally only up
// to and including upTo) so the copy keeps the same lineage.
func copyCheckpoints(ctx context.Context, sv checkpoint.Saver, from, to, upTo string) error {
	cps, err := sv.List(ctx, from, 0)
	if err != nil {
		return err
	}
	if upTo != "" {
		idx := -1
		for i, c := range cps {
			if c.ID == upTo {
				idx = i
			}
		}
		if idx < 0 {
			return checkpoint.ErrNotFound
		}
		cps = cps[idx:]
	}
	for i := len(cps) - 1; i >= 0; i-- {
		c := cps[i]
		c.ThreadID = to
		if _, err := sv.Put(ctx, c); err != nil {
			return err
		}
	}
	return nil
}
