package agentprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Command is a client-to-server message of the streaming protocol.
type Command struct {
	ID     *uint64         `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// cmdError is a protocol error code plus message.
type cmdError struct{ code, msg string }

func (e *cmdError) Error() string { return e.code + ": " + e.msg }

func cerr(code, msg string) *cmdError { return &cmdError{code, msg} }

type response struct {
	Type    string         `json:"type"`
	ID      *uint64        `json:"id"`
	Result  any            `json:"result,omitempty"`
	Error   string         `json:"error,omitempty"`
	Message string         `json:"message,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// respond runs a command and builds the success or error response.
func (s *Server) respond(ctx context.Context, th *threadRec, cmd Command, conn *wsConn) response {
	if cmd.ID == nil || cmd.Method == "" {
		return response{Type: "error", ID: cmd.ID, Error: "invalid_argument", Message: "id and method are required"}
	}
	res, err := s.exec(ctx, th, cmd, conn)
	if err != nil {
		var ce *cmdError
		if !errors.As(err, &ce) {
			ce = cerr("unknown_error", err.Error())
		}
		return response{Type: "error", ID: cmd.ID, Error: ce.code, Message: ce.msg}
	}
	if res == nil {
		res = map[string]any{}
	}
	return response{Type: "success", ID: cmd.ID, Result: res, Meta: map[string]any{"appliedThroughSeq": th.hub.lastSeq()}}
}

func (s *Server) threadCommand(w http.ResponseWriter, r *http.Request) {
	var cmd Command
	if err := decodeBody(r, &cmd); err != nil {
		writeJSON(w, 422, response{Type: "error", Error: "invalid_argument", Message: err.Error()})
		return
	}
	th := s.threadFor(r.Context(), r.PathValue("thread_id"), cmd.Method == "run.start")
	if th == nil {
		writeJSON(w, 404, response{Type: "error", ID: cmd.ID, Error: "no_such_run", Message: "thread not found"})
		return
	}
	resp := s.respond(r.Context(), th, cmd, nil)
	status := 200
	if resp.Type == "error" {
		status = 422
		if resp.Error == "no_such_checkpoint" || resp.Error == "no_such_interrupt" || resp.Error == "no_such_run" {
			status = 404
		}
	}
	writeJSON(w, status, resp)
}

func (s *Server) exec(ctx context.Context, th *threadRec, cmd Command, conn *wsConn) (any, error) {
	switch cmd.Method {
	case "run.start":
		return s.cmdRunStart(ctx, th, cmd.Params)
	case "input.respond":
		return s.cmdInputRespond(ctx, th, cmd.Params)
	case "input.inject":
		return s.cmdInputInject(ctx, th, cmd.Params)
	case "state.get":
		return s.cmdStateGet(ctx, th, cmd.Params)
	case "state.listCheckpoints":
		return s.cmdListCheckpoints(ctx, th, cmd.Params)
	case "state.fork":
		return s.cmdFork(ctx, th, cmd.Params)
	case "agent.getTree":
		return s.cmdTree(th), nil
	case "subscription.subscribe", "subscription.unsubscribe", "subscription.reconnect":
		if conn == nil {
			return nil, cerr("not_supported", "subscriptions are connection-scoped over SSE: open POST /threads/{id}/stream; in-band subscriptions need the WebSocket transport")
		}
		return conn.subscriptionCommand(th, cmd)
	}
	return nil, cerr("unknown_command", "unknown command "+cmd.Method)
}

func parse(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return cerr("invalid_argument", err.Error())
	}
	return nil
}

// toCmdErr maps an admission error to a protocol error.
func toCmdErr(err error) error {
	var ae *apiErr
	if errors.As(err, &ae) {
		if ae.status == 404 {
			return cerr("invalid_argument", ae.msg)
		}
		return cerr("invalid_argument", ae.msg)
	}
	return err
}

func (s *Server) cmdRunStart(ctx context.Context, th *threadRec, raw json.RawMessage) (any, error) {
	var p struct {
		AssistantID string          `json:"assistantId"`
		Input       json.RawMessage `json:"input"`
		Config      map[string]any  `json:"config"`
		Metadata    map[string]any  `json:"metadata"`
	}
	if err := parse(raw, &p); err != nil {
		return nil, err
	}
	if p.AssistantID == "" {
		return nil, cerr("invalid_argument", "assistantId is required")
	}
	body := RunCreate{ThreadID: th.id, AgentID: p.AssistantID, Input: p.Input, Metadata: p.Metadata, IfNotExists: "create"}
	if p.Config != nil {
		rc := &RunConfig{}
		if c, ok := p.Config["configurable"].(map[string]any); ok {
			rc.Configurable = c
		}
		if n, ok := p.Config["recursion_limit"].(float64); ok {
			rc.RecursionLimit = int(n)
		}
		body.Config = rc
	}
	in, err := buildInput(body.Input, nil)
	if err != nil {
		return nil, cerr("invalid_argument", err.Error())
	}
	rec, err := s.startRun(ctx, body, in, nil, enqueueIfBusy)
	if err != nil {
		return nil, toCmdErr(err)
	}
	if rec == nil { // a run is active: the input was queued behind it
		return map[string]any{"queued": true}, nil
	}
	return map[string]any{"runId": rec.RunID}, nil
}

type respondParams struct {
	Namespace   []string        `json:"namespace"`
	InterruptID string          `json:"interruptId"`
	Response    any             `json:"response"`
	Responses   []respondEntry  `json:"responses"`
	Update      map[string]any  `json:"update"`
	Goto        json.RawMessage `json:"goto"`
	Config      map[string]any  `json:"config"`
	Metadata    map[string]any  `json:"metadata"`
}

type respondEntry struct {
	Namespace   []string `json:"namespace"`
	InterruptID string   `json:"interruptId"`
	Response    any      `json:"response"`
}

func (s *Server) cmdInputRespond(ctx context.Context, th *threadRec, raw json.RawMessage) (any, error) {
	var p respondParams
	if err := parse(raw, &p); err != nil {
		return nil, err
	}
	entries := p.Responses
	if len(entries) == 0 {
		entries = []respondEntry{{p.Namespace, p.InterruptID, p.Response}}
	}
	agent, err := s.agentOf(th)
	if err != nil {
		return nil, cerr("no_such_run", err.Error())
	}
	snap, err := agent.Graph.GetState(threadCtx(ctx, th.id, ""))
	if err != nil {
		return nil, err
	}
	pending := map[string]bool{}
	for _, it := range snap.Interrupts {
		pending[it.ID] = true
	}
	resume := map[string]any{}
	for _, e := range entries {
		if e.InterruptID == "" || !pending[e.InterruptID] {
			return nil, cerr("no_such_interrupt", "no pending interrupt "+e.InterruptID)
		}
		resume[e.InterruptID] = e.Response
	}
	cmd := graph.Command{ResumeMap: resume, Update: p.Update}
	if len(p.Goto) > 0 {
		gotos, sends, err := parseGoto(p.Goto)
		if err != nil {
			return nil, cerr("invalid_argument", err.Error())
		}
		cmd.Goto, cmd.Send = gotos, sends
	}
	body := RunCreate{ThreadID: th.id, AgentID: agent.ID, Metadata: p.Metadata}
	rec, err := s.startRun(ctx, body, nil, &cmd, enqueueIfBusy)
	if err != nil {
		return nil, toCmdErr(err)
	}
	if rec == nil {
		return map[string]any{"queued": true}, nil
	}
	return map[string]any{"runId": rec.RunID}, nil
}

// parseGoto reads the protocol's Goto: a node name, a Send, or a list of both.
func parseGoto(raw json.RawMessage) (gotos []string, sends []graph.Send, err error) {
	var items []json.RawMessage
	if len(raw) > 0 && raw[0] == '[' {
		if err = json.Unmarshal(raw, &items); err != nil {
			return
		}
	} else {
		items = []json.RawMessage{raw}
	}
	for _, it := range items {
		var name string
		if json.Unmarshal(it, &name) == nil {
			gotos = append(gotos, name)
			continue
		}
		var snd struct {
			Node  string         `json:"node"`
			Input map[string]any `json:"input"`
		}
		if err = json.Unmarshal(it, &snd); err != nil || snd.Node == "" {
			return nil, nil, errors.New("goto entries must be a node name or {node, input}")
		}
		sends = append(sends, graph.Send{Node: snd.Node, Arg: graph.State(snd.Input)})
	}
	return
}

func (s *Server) cmdInputInject(ctx context.Context, th *threadRec, raw json.RawMessage) (any, error) {
	var p struct {
		Namespace []string `json:"namespace"`
		Message   struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Name    string `json:"name"`
		} `json:"message"`
	}
	if err := parse(raw, &p); err != nil {
		return nil, err
	}
	if p.Message.Role != "user" && p.Message.Role != "system" {
		return nil, cerr("invalid_argument", "message.role must be user or system")
	}
	agent, err := s.agentOf(th)
	if err != nil {
		return nil, cerr("no_such_run", err.Error())
	}
	m := messages.Message{Role: roleOf(p.Message.Role), Content: p.Message.Content, Name: p.Message.Name}
	in := graph.State{"messages": []messages.Message{m}}
	// an active run cannot take input mid-flight in this runtime, so the
	// message is queued and runs right after it
	rec, err := s.startRun(ctx, RunCreate{ThreadID: th.id, AgentID: agent.ID}, in, nil, enqueueIfBusy)
	if err != nil {
		return nil, toCmdErr(err)
	}
	if rec == nil {
		return map[string]any{"queued": true}, nil
	}
	return map[string]any{"runId": rec.RunID}, nil
}

func (s *Server) agentOf(th *threadRec) (*AgentSpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.defaultAgent("", th)
}

func (s *Server) cmdStateGet(ctx context.Context, th *threadRec, raw json.RawMessage) (any, error) {
	var p struct {
		Namespace []string `json:"namespace"`
		Keys      []string `json:"keys"`
	}
	if err := parse(raw, &p); err != nil {
		return nil, err
	}
	agent, err := s.agentOf(th)
	if err != nil {
		return nil, cerr("no_such_run", err.Error())
	}
	snap, err := agent.Graph.GetState(threadCtx(ctx, th.id, ""))
	if err != nil {
		return nil, err
	}
	vals := jsonState(snap.Values)
	if len(p.Keys) > 0 {
		sub := map[string]any{}
		for _, k := range p.Keys {
			if v, ok := vals[k]; ok {
				sub[k] = v
			}
		}
		vals = sub
	}
	res := map[string]any{"values": vals}
	if snap.CheckpointID != "" {
		res["checkpoint"] = map[string]any{"id": snap.CheckpointID}
	}
	return res, nil
}

func (s *Server) checkpointTime(th *threadRec, id string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := th.cpTimes[id]; ok {
		return t
	}
	return th.created
}

func (s *Server) cmdListCheckpoints(ctx context.Context, th *threadRec, raw json.RawMessage) (any, error) {
	var p struct {
		Limit  int    `json:"limit"`
		Before string `json:"before"`
	}
	if err := parse(raw, &p); err != nil {
		return nil, err
	}
	agent, err := s.agentOf(th)
	if err != nil {
		return nil, cerr("no_such_run", err.Error())
	}
	snaps, err := agent.Graph.GetStateHistory(threadCtx(ctx, th.id, ""), 0)
	if err != nil {
		return nil, err
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}
	skipping := p.Before != ""
	out := []map[string]any{}
	for _, sn := range snaps {
		if skipping {
			if sn.CheckpointID == p.Before {
				skipping = false
			}
			continue
		}
		e := map[string]any{"id": sn.CheckpointID, "timestamp": s.checkpointTime(th, sn.CheckpointID).Format(time.RFC3339), "step": max(sn.Step, 0)}
		if len(sn.Next) > 0 {
			e["nodeName"] = sn.Next[0]
		}
		out = append(out, e)
		if len(out) >= p.Limit {
			break
		}
	}
	if skipping {
		return nil, cerr("no_such_checkpoint", "checkpoint "+p.Before+" not found")
	}
	return map[string]any{"checkpoints": out}, nil
}

func (s *Server) cmdFork(ctx context.Context, th *threadRec, raw json.RawMessage) (any, error) {
	var p struct {
		CheckpointID string          `json:"checkpointId"`
		Input        json.RawMessage `json:"input"`
		Config       map[string]any  `json:"config"`
	}
	if err := parse(raw, &p); err != nil {
		return nil, err
	}
	agent, err := s.agentOf(th)
	if err != nil {
		return nil, cerr("no_such_run", err.Error())
	}
	if p.CheckpointID == "" {
		return nil, cerr("invalid_argument", "checkpointId is required")
	}
	if agent.Graph.Saver() == nil {
		return nil, cerr("not_supported", "agent has no checkpointer")
	}
	s.mu.Lock()
	fork := s.newThread(newID(), cloneMap(th.metadata))
	fork.agentID = agent.ID
	s.mu.Unlock()
	if err := copyCheckpoints(ctx, agent.Graph.Saver(), th.id, fork.id, p.CheckpointID); err != nil {
		s.mu.Lock()
		delete(s.threads, fork.id)
		s.mu.Unlock()
		if errors.Is(err, checkpoint.ErrNotFound) {
			return nil, cerr("no_such_checkpoint", "checkpoint "+p.CheckpointID+" not found")
		}
		return nil, err
	}
	res := map[string]any{"threadId": fork.id, "runId": ""}
	if len(p.Input) > 0 && string(p.Input) != "null" {
		in, err := buildInput(p.Input, nil)
		if err != nil {
			return nil, cerr("invalid_argument", err.Error())
		}
		rec, err := s.startRun(ctx, RunCreate{ThreadID: fork.id, AgentID: agent.ID}, in, nil, enqueueIfBusy)
		if err != nil {
			return nil, toCmdErr(err)
		}
		if rec != nil {
			res["runId"] = rec.RunID
		}
	}
	return res, nil
}

func (s *Server) cmdTree(th *threadRec) any {
	agent, _ := s.agentOf(th)
	s.mu.Lock()
	status := "completed"
	switch th.status {
	case ThreadBusy:
		status = "running"
	case ThreadInterrupted:
		status = "interrupted"
	case ThreadError:
		status = "failed"
	}
	s.mu.Unlock()
	name := ""
	if agent != nil {
		name = agent.Name
	}
	return map[string]any{"tree": map[string]any{"namespace": []string{}, "status": status, "graphName": name, "children": []any{}}}
}
