package agentprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// apiErr is an error carrying its HTTP status.
type apiErr struct {
	status int
	code   string
	msg    string
}

func (e *apiErr) Error() string { return e.msg }

func errf(status int, code, format string, a ...any) *apiErr {
	return &apiErr{status, code, fmt.Sprintf(format, a...)}
}

func writeAPIErr(w http.ResponseWriter, err error) {
	var ae *apiErr
	if errors.As(err, &ae) {
		writeErr(w, ae.status, ae.code, ae.msg)
		return
	}
	writeErr(w, 500, "internal", err.Error())
}

const maxRetainedRuns = 5000

// startMode says what to do when the thread already has an active run.
type startMode int

const (
	rejectIfBusy  startMode = iota // REST: 409
	enqueueIfBusy                  // streaming protocol: run after the current one
)

// startRun admits a run request and executes it in the background. For
// enqueueIfBusy on a busy thread it returns (nil, nil): the input was queued.
func (s *Server) startRun(ctx context.Context, body RunCreate, in graph.State, cmd *graph.Command, mode startMode) (*runRec, error) {
	s.mu.Lock()
	var th *threadRec
	ephemeral := false
	if body.ThreadID != "" {
		th = s.lookup(ctx, body.ThreadID)
		if th == nil {
			if body.IfNotExists != "create" {
				s.mu.Unlock()
				return nil, errf(404, "not_found", "thread %q not found (set if_not_exists=create)", body.ThreadID)
			}
			th = s.newThread(body.ThreadID, nil)
		}
	} else {
		th = s.newThread(newID(), nil)
		ephemeral = true
	}
	agent, err := s.defaultAgent(body.AgentID, th)
	if err != nil {
		if ephemeral {
			delete(s.threads, th.id)
		}
		s.mu.Unlock()
		return nil, errf(404, "not_found", "%v", err)
	}
	if th.agentID != "" && th.agentID != agent.ID {
		s.mu.Unlock()
		return nil, errf(409, "conflict", "thread belongs to agent %q", th.agentID)
	}
	th.agentID = agent.ID
	if th.active != nil {
		if mode == enqueueIfBusy {
			th.queue = append(th.queue, queuedRun{body: body, input: in, cmd: cmd})
			s.mu.Unlock()
			return nil, nil
		}
		s.mu.Unlock()
		return nil, errf(409, "conflict", "thread already has an active run")
	}
	// a paused thread resumes with the new input instead of starting over
	if cmd == nil && th.status == ThreadInterrupted {
		cmd = &graph.Command{Resume: resumeValue(body, in)}
		in = nil
	}
	now := time.Now().UTC()
	rec := &runRec{
		Run:      Run{RunID: newID(), ThreadID: th.id, AgentID: agent.ID, Status: RunPending, Metadata: orEmpty(body.Metadata), CreatedAt: now, UpdatedAt: now},
		threadID: th.id, done: make(chan struct{}), ephemeral: ephemeral,
	}
	rec.cond = sync.NewCond(&rec.evMu)
	s.runs[rec.RunID] = rec
	th.active, th.status, th.updated = rec, ThreadBusy, now
	s.evictRuns()
	s.mu.Unlock()

	rctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	rec.cancel = cancel
	go s.execute(rctx, rec, th, agent, body, in, cmd)
	return rec, nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// resumeValue picks what answers a pending interrupt: input.resume if given,
// else the raw input, else the messages.
func resumeValue(body RunCreate, in graph.State) any {
	if v, ok := in["resume"]; ok {
		return v
	}
	if len(in) == 1 {
		if m, ok := in["messages"]; ok {
			return m
		}
	}
	if len(in) == 0 {
		return true
	}
	return map[string]any(in)
}

func (s *Server) evictRuns() {
	if len(s.runs) <= maxRetainedRuns {
		return
	}
	var finished []*runRec
	for _, r := range s.runs {
		select {
		case <-r.done:
			finished = append(finished, r)
		default:
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].UpdatedAt.Before(finished[j].UpdatedAt) })
	for _, r := range finished[:len(finished)/2] {
		delete(s.runs, r.RunID)
	}
}

func (s *Server) execute(ctx context.Context, rec *runRec, th *threadRec, agent *AgentSpec, body RunCreate, in graph.State, cmd *graph.Command) {
	defer rec.cancel()
	cfg := runnables.Config{Configurable: map[string]any{"thread_id": th.id}}
	if body.Config != nil {
		for k, v := range body.Config.Configurable {
			cfg.Configurable[k] = v
		}
		cfg.Tags = body.Config.Tags
		cfg.RecursionLimit = body.Config.RecursionLimit
	}
	cfg.Configurable["thread_id"] = th.id
	cfg.Configurable["run_id"] = rec.RunID
	cfg.Metadata = map[string]any{"run_id": rec.RunID, "thread_id": th.id, "agent_id": agent.ID}
	ctx = runnables.WithConfig(ctx, cfg)
	tr := newTranslator(th.hub, agent.Name, th.memory)
	ctx = callbacks.WithHandlers(ctx, tr.callbacksHandler())

	saver := agent.Graph.Saver()
	if saver != nil {
		if snap, err := agent.Graph.GetState(ctx); err == nil {
			rec.rollbackTo = snap.CheckpointID
		}
	}
	s.setRun(rec, RunPending, "")
	rec.addEvent("metadata", map[string]any{"run_id": rec.RunID, "thread_id": th.id})
	tr.lifecycle("started", nil)
	tr.lifecycle("running", nil)

	var input any = in
	if cmd != nil {
		input = *cmd
	}
	var final graph.State
	var runErr error
	for ev, err := range agent.Graph.StreamEvents(ctx, input, graph.ModeValues, graph.ModeUpdates, graph.ModeMessages, graph.ModeCustom) {
		if err != nil {
			runErr = err
			break
		}
		tr.handle(ev)
		rec.addGraphEvent(ev)
		if ev.Mode == graph.ModeValues {
			if st, ok := ev.Data.(graph.State); ok {
				final = st
			}
			s.publishCheckpoint(ctx, th, agent, tr)
		}
	}
	tr.finish()

	status, errMsg := RunSuccess, ""
	var interrupts []graph.InterruptInfo
	if saver != nil {
		if snap, err := agent.Graph.GetState(ctx); err == nil {
			interrupts = snap.Interrupts
			if final == nil {
				final = snap.Values
			}
		}
	}
	if interrupts == nil {
		interrupts = graph.Interrupts(final)
	}
	switch {
	case ctx.Err() != nil:
		status = RunInterrupted
		tr.lifecycle("interrupted", map[string]any{"reason": "cancelled"})
	case runErr != nil:
		status, errMsg = RunError, runErr.Error()
		tr.lifecycle("failed", map[string]any{"error": errMsg})
		rec.addEvent("error", map[string]any{"error": "RunError", "message": errMsg})
	case len(interrupts) > 0:
		status = RunInterrupted
		for _, it := range interrupts {
			th.hub.publish("input.requested", rootNS, it.Node, map[string]any{"interruptId": it.ID, "payload": it.Value})
		}
		tr.lifecycle("interrupted", nil)
	default:
		tr.lifecycle("completed", nil)
	}

	s.mu.Lock()
	rec.values = final
	rec.Status, rec.Error, rec.UpdatedAt = status, errMsg, time.Now().UTC()
	th.active = nil
	th.updated = rec.UpdatedAt
	switch status {
	case RunInterrupted:
		th.status = ThreadInterrupted
		if ctx.Err() != nil && len(interrupts) == 0 {
			th.status = ThreadIdle
		}
	case RunError:
		th.status = ThreadError
	default:
		th.status = ThreadIdle
	}
	var next *queuedRun
	if th.status != ThreadError && len(th.queue) > 0 {
		q := th.queue[0]
		th.queue = th.queue[1:]
		next = &q
	}
	view := rec.Run
	s.mu.Unlock()
	rec.finish()

	if body.Webhook != "" {
		s.fireWebhook(body.Webhook, view, final)
	}
	if body.OnCompletion == "delete" || rec.ephemeral {
		s.mu.Lock()
		delete(s.runs, rec.RunID)
		if rec.ephemeral {
			delete(s.threads, th.id)
		}
		s.mu.Unlock()
		if rec.ephemeral && saver != nil {
			_ = saver.DeleteThread(context.Background(), th.id)
		}
	}
	if next != nil {
		if _, err := s.startRun(context.Background(), next.body, next.input, next.cmd, enqueueIfBusy); err != nil {
			th.hub.publish("lifecycle", rootNS, "", map[string]any{"event": "failed", "error": err.Error()})
		}
	}
}

func (s *Server) setRun(rec *runRec, status, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.Status, rec.Error, rec.UpdatedAt = status, errMsg, time.Now().UTC()
}

// publishCheckpoint announces the checkpoint a values event belongs to.
func (s *Server) publishCheckpoint(ctx context.Context, th *threadRec, agent *AgentSpec, tr *translator) {
	if agent.Graph.Saver() == nil {
		return
	}
	snap, err := agent.Graph.GetState(ctx)
	if err != nil || snap.CheckpointID == "" {
		return
	}
	s.mu.Lock()
	if _, seen := th.cpTimes[snap.CheckpointID]; seen {
		s.mu.Unlock()
		return
	}
	th.cpTimes[snap.CheckpointID] = time.Now().UTC()
	s.mu.Unlock()
	source := "loop"
	if snap.Step <= 0 {
		source = "input"
	}
	data := map[string]any{"id": snap.CheckpointID, "step": snap.Step, "source": source}
	if snap.ParentID != "" {
		data["parentId"] = snap.ParentID
	}
	th.hub.publish("checkpoints", rootNS, "", data)
}

func (s *Server) fireWebhook(url string, run Run, values graph.State) {
	b, _ := json.Marshal(map[string]any{"run": run, "values": jsonState(values), "messages": messagesOf(values)})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.opts.HTTP.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// ------------------------------------------------------------ per-run events

func (r *runRec) addEvent(name string, data any) {
	r.evMu.Lock()
	r.events = append(r.events, runEvent{name, data})
	r.evMu.Unlock()
	r.cond.Broadcast()
}

func (r *runRec) addGraphEvent(ev graph.Event) {
	switch ev.Mode {
	case graph.ModeValues:
		if st, ok := ev.Data.(graph.State); ok {
			r.addEvent("values", jsonState(st))
		}
	case graph.ModeUpdates:
		if upd := unwrapUpdate(ev); upd != nil {
			r.addEvent("updates", map[string]any{ev.Node: jsonState(upd)})
		}
	case graph.ModeMessages:
		r.addEvent("messages", map[string]any{"chunk": ev.Data, "node": ev.Node})
	case graph.ModeCustom:
		r.addEvent("custom", ev.Data)
	}
}

func (r *runRec) finish() {
	r.evMu.Lock()
	r.finished = true
	close(r.done)
	r.evMu.Unlock()
	r.cond.Broadcast()
}

// ----------------------------------------------------------------- handlers

func (s *Server) parseRun(r *http.Request) (RunCreate, graph.State, error) {
	var body RunCreate
	if err := decodeBody(r, &body); err != nil {
		return body, nil, errf(422, "invalid_request", "%v", err)
	}
	if body.IfNotExists != "" && body.IfNotExists != "create" && body.IfNotExists != "reject" {
		return body, nil, errf(422, "invalid_request", "if_not_exists must be create or reject")
	}
	in, err := buildInput(body.Input, body.Messages)
	if err != nil {
		return body, nil, errf(422, "invalid_request", "invalid input: %v", err)
	}
	return body, in, nil
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	body, in, err := s.parseRun(r)
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	rec, err := s.startRun(r.Context(), body, in, nil, rejectIfBusy)
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	writeJSON(w, 200, s.runView(rec))
}

func (s *Server) runView(rec *runRec) Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return rec.Run
}

func (s *Server) waitResponse(rec *runRec) RunWaitResponse {
	s.mu.Lock()
	run := rec.Run
	vals := rec.values
	s.mu.Unlock()
	return RunWaitResponse{Run: &run, Values: jsonState(vals), Messages: messagesOf(vals)}
}

func (s *Server) createAndWait(w http.ResponseWriter, r *http.Request) {
	body, in, err := s.parseRun(r)
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	rec, err := s.startRun(r.Context(), body, in, nil, rejectIfBusy)
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	select {
	case <-rec.done:
	case <-r.Context().Done():
		if body.OnDisconnect == "cancel" {
			rec.cancel()
		}
		return
	}
	writeJSON(w, 200, s.waitResponse(rec))
}

func (s *Server) findRun(id string) *runRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[id]
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	rec := s.findRun(r.PathValue("run_id"))
	if rec == nil {
		writeErr(w, 404, "not_found", "run not found")
		return
	}
	writeJSON(w, 200, s.runView(rec))
}

func (s *Server) waitRun(w http.ResponseWriter, r *http.Request) {
	rec := s.findRun(r.PathValue("run_id"))
	if rec == nil {
		writeErr(w, 404, "not_found", "run not found")
		return
	}
	select {
	case <-rec.done:
	case <-r.Context().Done():
		return
	}
	writeJSON(w, 200, s.waitResponse(rec))
}

func (s *Server) deleteRun(w http.ResponseWriter, r *http.Request) {
	rec := s.findRun(r.PathValue("run_id"))
	if rec == nil {
		writeErr(w, 404, "not_found", "run not found")
		return
	}
	rec.cancel()
	<-rec.done
	s.mu.Lock()
	delete(s.runs, rec.RunID)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) searchRuns(w http.ResponseWriter, r *http.Request) {
	var req RunSearchRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	s.mu.Lock()
	out := []Run{}
	for _, rec := range s.runs {
		if req.Status != "" && rec.Status != req.Status || req.ThreadID != "" && rec.ThreadID != req.ThreadID ||
			req.AgentID != "" && rec.AgentID != req.AgentID || !matchMeta(rec.Metadata, req.Metadata) {
			continue
		}
		out = append(out, rec.Run)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	writeJSON(w, 200, page(out, req.Limit, req.Offset))
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	rec := s.findRun(r.PathValue("run_id"))
	if rec == nil {
		writeErr(w, 404, "not_found", "run not found")
		return
	}
	action := r.URL.Query().Get("action")
	if action != "" && action != "interrupt" && action != "rollback" {
		writeErr(w, 422, "invalid_request", "action must be interrupt or rollback")
		return
	}
	rec.cancel()
	if r.URL.Query().Get("wait") == "true" || action == "rollback" {
		select {
		case <-rec.done:
		case <-r.Context().Done():
			return
		}
	}
	if action == "rollback" {
		s.rollback(r.Context(), rec)
	}
	w.WriteHeader(http.StatusNoContent)
}

// rollback restores the thread to the checkpoint before the run by forking it
// as the new head, and forgets the run.
func (s *Server) rollback(ctx context.Context, rec *runRec) {
	s.mu.Lock()
	th := s.threads[rec.threadID]
	agent, _ := s.defaultAgent("", th)
	delete(s.runs, rec.RunID)
	s.mu.Unlock()
	if agent != nil && agent.Graph.Saver() != nil && rec.rollbackTo != "" {
		_, _ = agent.Graph.UpdateState(threadCtx(ctx, rec.threadID, rec.rollbackTo), graph.State{}, "")
	}
}
