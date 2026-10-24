package team

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agentprotocol"
	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/mcp"
)

// groundTool is the knowledge technique each role consults before answering.
// The right technique depends on the job: retrieval of standards for
// engineers, a whole small checklist for gates.
var groundTool = map[string]string{
	"po": "knowledge_answer", "leader": "knowledge_answer",
	"junior": "rag_answer", "mid": "rag_answer", "senior": "rag_answer",
	"specialist": "cag_answer", "qa": "cag_answer",
}

// WorkerConfig describes one worker service.
type WorkerConfig struct {
	ID   string // unique, e.g. senior-1
	Role string // po, leader, junior, mid, senior, specialist, qa
	Hub  string // base URL of the hub
	URL  string // public URL of this worker's Agent Protocol server (default: its listener)
	// Middleware and Tools are added to the agent after the hub's MCP tools.
	// Empty means none: the worker is unchanged.
	Middleware []agents.Middleware
	Tools      []tools.Tool
	// Offline is the scripted model's answer function when no provider is set.
	// Nil means the role's own offline answers (offline in text.go).
	Offline func([]messages.Message) messages.Message
}

// RunWorker serves the role's agent over Agent Protocol on ln and works the
// hub's queue until ctx ends. It can start before the hub and survive the hub
// restarting: it retries, and registers again when its lease is lost.
func RunWorker(ctx context.Context, cfg WorkerConfig, ln net.Listener) error {
	prompt, err := Skill(cfg.Role)
	if err != nil {
		return err
	}
	fake := offline
	if cfg.Offline != nil {
		fake = cfg.Offline
	}
	model, label, err := NewModel(cfg.Role, fake)
	if err != nil {
		return err
	}
	hub := NewHubClient(cfg.Hub)
	if cfg.URL == "" {
		cfg.URL = "http://" + ln.Addr().String()
	}

	mc, err := connectMCP(ctx, cfg.Hub+"/mcp")
	if err != nil {
		return err
	}
	defer mc.Close()
	ts, err := mc.Tools(ctx, "")
	if err != nil {
		return err
	}
	// Best effort and bounded: the activity is shown on the dashboard, nothing waits on it.
	setActivity := func(s string) {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		hub.SetActivity(c, cfg.ID, s)
	}
	use := &usageAcc{} // a worker runs one job at a time, so its tokens are the job's
	agent, err := agents.Create(agents.Options{
		Model: model, Tools: append(ts, cfg.Tools...), SystemPrompt: prompt,
		Middleware: append(slices.Clone(cfg.Middleware), usageMeter{fixed: use}, activityMeter{set: setActivity}),
		Compile:    []graph.CompileOption{graph.WithName(cfg.Role), graph.WithCheckpointer(checkpoint.NewMemory())},
	})
	if err != nil {
		return err
	}
	ap, err := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{
		ID: cfg.Role, Name: cfg.ID, Description: skillDir[cfg.Role], Graph: agent,
		Metadata: map[string]any{"role": cfg.Role, "worker": cfg.ID},
	}}})
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: ap, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	w := &worker{cfg: cfg, model: label, hub: hub, mc: mc, use: use, act: setActivity, ap: agentprotocol.NewClient(cfg.URL, "")}
	log.Printf("%s: %s up on %s (model %s)", cfg.ID, cfg.Role, cfg.URL, label)
	ttl := w.register(ctx)
	go w.heartbeat(ctx, ttl)

	for ctx.Err() == nil {
		j, err := hub.Claim(ctx, cfg.ID)
		switch {
		case errors.Is(err, ErrUnknownWorker):
			w.register(ctx)
		case err != nil:
			sleep(ctx, time.Second)
		case j != nil:
			w.finish(ctx, j)
		}
	}
	return nil
}

type worker struct {
	cfg   WorkerConfig
	model string // label from NewModel, reported to the hub
	hub   *HubClient
	mc    *mcp.Client
	use   *usageAcc
	act   func(string) // report what the worker is doing now

	curMu   sync.Mutex
	curJob  string             // job being run
	curStop context.CancelFunc // cancels its agent run
	ap      *agentprotocol.Client
}

// connectMCP retries until the hub is up.
func connectMCP(ctx context.Context, url string) (*mcp.Client, error) {
	for {
		c, err := mcp.Connect(ctx, mcp.HTTP(url, nil))
		if err == nil {
			return c, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		sleep(ctx, time.Second)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// register joins the pool, retrying until the hub answers, and returns the lease TTL.
func (w *worker) register(ctx context.Context) time.Duration {
	for {
		ttl, err := w.hub.RegisterInfo(ctx, WorkerInfo{ID: w.cfg.ID, Role: w.cfg.Role, URL: w.cfg.URL, Model: w.model, Skill: skillDir[w.cfg.Role], Refs: skillRefs(w.cfg.Role)})
		if err == nil {
			return ttl
		}
		if ctx.Err() != nil {
			return 0
		}
		sleep(ctx, time.Second)
	}
}

// heartbeat renews the lease while a long model call runs.
func (w *worker) heartbeat(ctx context.Context, ttl time.Duration) {
	t := time.NewTicker(max(ttl/3, 100*time.Millisecond))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cancelled, err := w.hub.HeartbeatCancel(ctx, w.cfg.ID)
			if errors.Is(err, ErrUnknownWorker) {
				w.register(ctx)
			}
			if cancelled != "" {
				w.cancelJob(cancelled)
			}
		}
	}
}

// cancelJob stops the agent run of job id if it is still the one running.
func (w *worker) cancelJob(id string) {
	w.curMu.Lock()
	defer w.curMu.Unlock()
	if w.curJob == id && w.curStop != nil {
		log.Printf("%s: job %s cancelled by the hub", w.cfg.ID, id)
		w.curStop()
	}
}

func (w *worker) finish(ctx context.Context, j *Job) {
	w.use.take() // drop anything a previous, abandoned run left
	rctx, stop := context.WithCancel(ctx)
	w.curMu.Lock()
	w.curJob, w.curStop = j.ID, stop
	w.curMu.Unlock()
	defer func() {
		w.curMu.Lock()
		w.curJob, w.curStop = "", nil
		w.curMu.Unlock()
		stop()
	}()
	out, err := w.run(rctx, j)
	u := w.use.take()
	msg := ""
	if err != nil {
		msg = err.Error()
		log.Printf("%s: job %s failed: %v", w.cfg.ID, j.ID, err)
	}
	if err := w.hub.CompleteUsage(ctx, w.cfg.ID, j.ID, out, msg, u.ptr()); errors.Is(err, ErrNotOwner) {
		log.Printf("%s: job %s was reassigned while running; result dropped", w.cfg.ID, j.ID)
	}
}

// run enriches the prompt with knowledge and team memory (both through MCP),
// runs the agent on the job's thread through its own Agent Protocol endpoint,
// and records the outcome in the team memory.
// One path serves both this queue and any Agent Protocol client that calls the
// worker directly, and every run is visible as an Agent Protocol thread.
func (w *worker) run(ctx context.Context, j *Job) (string, error) {
	prompt := "WHO: " + w.cfg.ID + "\n" + j.Prompt // identity is known only after the hub reserves this worker
	if tool := groundTool[w.cfg.Role]; tool != "" && j.Query != "" {
		w.act("reference: " + tool + " (" + head(j.Query, 60) + ")")
		if txt, err := w.mc.CallTool(ctx, tool, map[string]any{"question": j.Query}); err == nil {
			prompt += fmt.Sprintf("\n\n## Knowledge (%s)\n%v\n", tool, txt)
		} else {
			log.Printf("%s: %s: %v", w.cfg.ID, tool, err)
		}
	}
	if j.Scope != "" {
		w.act("reference: team memory")
		if txt, err := w.mc.CallTool(ctx, "memory_search", map[string]any{"scope": j.Scope, "query": j.Query + " " + w.cfg.Role}); err == nil {
			prompt += fmt.Sprintf("\n\n## Team memory\n%v\n", txt)
		}
	}
	w.act("skill " + skillDir[w.cfg.Role] + ": model")
	res, err := w.ap.CreateAndWait(ctx, agentprotocol.RunCreate{
		ThreadID: j.Thread, AgentID: w.cfg.Role, IfNotExists: "create", OnDisconnect: "cancel",
		Messages: []agentprotocol.Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	if res.Run != nil && res.Run.Status != "success" {
		return "", fmt.Errorf("run %s: %s", res.Run.Status, res.Run.Error)
	}
	if len(res.Messages) == 0 {
		return "", errors.New("agent returned no messages")
	}
	out := fmt.Sprint(res.Messages[len(res.Messages)-1].Content)
	if j.Scope != "" {
		_, _ = w.mc.CallTool(ctx, "memory_put", map[string]any{
			"scope": j.Scope, "key": j.ID, "text": fmt.Sprintf("%s (%s): %s", w.cfg.ID, w.cfg.Role, head(out, 400)),
		})
	}
	return out, nil
}
