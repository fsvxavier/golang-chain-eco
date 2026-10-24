package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/internal/tasktrack"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// MaxRounds is how many times a task may be returned before it is blocked.
const MaxRounds = 3

// preference is who suits a task best, in order. High-complexity work never
// goes to a junior.
var preference = map[string][]string{
	"low":    {"junior", "mid", "senior"},
	"medium": {"mid", "senior", "junior"},
	"high":   {"senior", "mid"},
}

// work is one task travelling through dev, review and QA.
type work struct {
	ID, Title, Complexity, Desc string
	Assignee, Level             string // worker id and role that built it
	Code, Feedback              string
	Prefer                      string // worker id the leader suggests for the first attempt
	Round                       int
	Status                      string
}

// manager runs the delivery flow. It is the only service that sees the whole
// request; every other agent is reached through the hub queue. It never writes
// code: its own agent only routes and reports.
type manager struct {
	hub   *HubClient
	agent *graph.Compiled
	track *tasktrack.Tracker
	calls func(requestID string) int // backend calls per request; nil: not counted
}

// move records a status change. An illegal move is a bug in the flow, not in
// the work, so it is logged and the delivery goes on.
func (m *manager) move(ctx context.Context, kind tasktrack.Kind, id string, to tasktrack.Status, agent, detail string) {
	if err := m.track.Move(ctx, kind, id, to, agent, detail); err != nil {
		log.Print(err)
	}
}

// NewManager builds the manager graph, served by the manager service through
// remote/server and called by clients with remote.Graph.
// calls counts a request's backend calls for the PO evidence rule (nil skips
// that half of the rule).
func NewManager(hub *HubClient, chat models.ChatModel, track *tasktrack.Tracker, calls func(requestID string) int) (*graph.Compiled, error) {
	prompt, err := Skill("manager")
	if err != nil {
		return nil, err
	}
	agent, err := agents.Create(agents.Options{Model: chat, SystemPrompt: prompt})
	if err != nil {
		return nil, err
	}
	m := &manager{hub: hub, agent: agent, track: track, calls: calls}

	g := graph.NewStateGraph(graph.Schema{
		"request": channels.LastValue(), "kickoff": channels.LastValue(),
		"spec": channels.LastValue(), "scope": channels.LastValue(),
		"history": graph.Append(), "results": graph.Append(),
		"report": channels.LastValue(), "status": channels.LastValue(),
	})
	g.AddNode("manager", m.intake)
	po := m.step("po", "product-owner", tasktrack.InSpecification, func(s graph.State) string {
		return fmt.Sprintf("ROLE: po\n\n## Request\n%v\n\n## Manager kickoff\n%v\n\n%s", s["request"], s["kickoff"], backendBlock(scopeOf(s)))
	}, "spec")
	g.AddNode("po", func(ctx context.Context, s graph.State) (any, error) {
		out, err := po(ctx, s)
		if err != nil {
			return nil, err
		}
		st := out.(graph.State)
		st["spec"] = verifiedSpec(fmt.Sprint(st["spec"]), scopeOf(s), m.calls, requestID(ctx))
		return st, nil
	})
	g.AddNode("build", m.build)
	g.AddNode("deliver", m.deliver)
	g.AddEdge(graph.Start, "manager").AddEdge("manager", "po").AddEdge("po", "build").
		AddEdge("build", "deliver").AddEdge("deliver", graph.End)
	return g.Compile(graph.WithName("engineering_team"), graph.WithCheckpointer(checkpoint.NewMemory()))
}

func requestID(ctx context.Context) string {
	id, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
	if id == "" {
		id = messages.NewID()
	}
	return id
}

// subject names a status subject: the request (id "") or one of its tasks.
// Ids are short so notifications stay readable; the tracker is shared by every
// request the service handles, so tasks carry their request's prefix.
func subject(ctx context.Context, task string) string {
	id := requestID(ctx)
	if len(id) > 8 {
		id = id[:8]
	}
	if task == "" {
		return id
	}
	return id + "/" + task
}

// ask sends one prompt to the manager's own agent.
func (m *manager) ask(ctx context.Context, prompt string) (string, error) {
	out, err := m.agent.Invoke(ctx, graph.State{"messages": []messages.Message{messages.Human(prompt)}})
	if err != nil {
		return "", err
	}
	ms, _ := messages.Coerce(out["messages"])
	last, _ := messages.Last(ms)
	if strings.Contains(last.Text(), "```") { // the manager coordinates; it never writes code
		return "", fmt.Errorf("manager wrote code: %q", last.Text())
	}
	return last.Text(), nil
}

func (m *manager) intake(ctx context.Context, s graph.State) (any, error) {
	// Stored for every request, empty when the demand named none; workers reach
	// it through the exact job thread (Hub.ThreadScope), never by id prefix.
	if err := m.hub.SetScope(ctx, requestID(ctx), scopeOf(s)); err != nil {
		return nil, err
	}
	m.move(ctx, tasktrack.Request, subject(ctx, ""), tasktrack.Received, "manager", head(fmt.Sprint(s["request"]), 80))
	out, err := m.ask(ctx, fmt.Sprintf("ROLE: manager\nMODE: intake\nREQUEST: %v", s["request"]))
	if err != nil {
		return nil, err
	}
	log.Printf("manager: request %s routed to %s", requestID(ctx), field(out, "ROUTE:"))
	return graph.State{"kickoff": field(out, "KICKOFF:"), "history": "manager -> product-owner"}, nil
}

// scopeOf decodes the demand's scope from the graph state (a map after JSON).
func scopeOf(s graph.State) Scope {
	var sc Scope
	if raw, err := json.Marshal(s["scope"]); err == nil {
		_ = json.Unmarshal(raw, &sc)
	}
	return sc
}

// backendBlock tells the PO what the demand lets its backend tools reach.
func backendBlock(sc Scope) string {
	if len(sc.Roots)+len(sc.URLs) == 0 {
		return "## Backend named by the demand\nnone: do not call the backend tools; say so in OPEN ITEMS."
	}
	var b strings.Builder
	b.WriteString("## Backend named by the demand\n")
	for _, r := range sc.Roots {
		b.WriteString("ROOT: " + r + "\n")
	}
	for _, u := range sc.URLs {
		b.WriteString("URL: " + u + "\n")
	}
	return b.String()
}

// verifiedSpec enforces the evidence rule: when the demand named a backend, the
// spec needs an EVIDENCE block and at least one counted backend call (that half
// is skipped when calls is nil). Otherwise the spec's OPEN ITEMS get flagged.
func verifiedSpec(spec string, sc Scope, calls func(string) int, requestID string) string {
	if len(sc.Roots)+len(sc.URLs) == 0 {
		return spec
	}
	hasEvidence := false
	for _, l := range strings.Split(spec, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "EVIDENCE:") {
			hasEvidence = true
		}
	}
	if hasEvidence && (calls == nil || calls(requestID) >= 1) {
		return spec
	}
	const flag = "- UNVERIFIED: no backend evidence"
	lines := strings.Split(spec, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "OPEN ITEMS:") {
			return strings.Join(append(lines[:i+1:i+1], append([]string{flag}, lines[i+1:]...)...), "\n")
		}
	}
	return spec + "\nOPEN ITEMS:\n" + flag
}

// dispatch queues a job for one of prefer (worker ids or roles) and waits.
func (m *manager) dispatch(ctx context.Context, prefer []string, thread, prompt, query string) (Job, error) {
	return m.dispatchJob(ctx, Job{Prefer: prefer}, thread, prompt, query)
}

// dispatchJob is dispatch with the job's Prefer and Pin already set.
func (m *manager) dispatchJob(ctx context.Context, j Job, thread, prompt, query string) (Job, error) {
	scope := requestID(ctx)
	j.Thread, j.Prompt, j.Query, j.Scope = scope+"-"+thread, prompt, query, scope
	return m.hub.Run(ctx, j)
}

// step is a node that hands the request to one role and stores its answer.
func (m *manager) step(role, label string, st tasktrack.Status, prompt func(graph.State) string, key string) graph.NodeFunc {
	return func(ctx context.Context, s graph.State) (any, error) {
		m.move(ctx, tasktrack.Request, subject(ctx, ""), st, label, "")
		j, err := m.dispatch(ctx, []string{role}, role, prompt(s), fmt.Sprint(s["request"]))
		if err != nil {
			return nil, err
		}
		log.Printf("manager: %s (%s) answered", label, j.Worker)
		hist := fmt.Sprintf("%s (%s) done", label, j.Worker)
		return graph.State{key: j.Output, "history": hist}, nil
	}
}

func dossier(s graph.State) string {
	return fmt.Sprintf("## Request\n%v\n\n## Specification\n%v", s["request"], s["spec"])
}

// build asks the leader for a plan, then runs every task through
// dev -> review -> QA concurrently. The hub decides who works on what and
// queues the tasks nobody can take yet.
func (m *manager) build(ctx context.Context, s graph.State) (any, error) {
	brief := dossier(s)
	m.move(ctx, tasktrack.Request, subject(ctx, ""), tasktrack.TaskAssignment, "tech-leader", "")
	plan, err := m.dispatch(ctx, []string{"leader"}, "leader", "ROLE: leader\n\n"+brief+m.workersBlock(ctx), fmt.Sprint(s["request"]))
	if err != nil {
		return nil, err
	}
	tasks := parsePlan(plan.Output)
	for _, t := range tasks {
		m.move(ctx, tasktrack.Task, subject(ctx, t.ID), tasktrack.TaskAssignment, "tech-leader", t.Title+" ("+t.Complexity+")")
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("leader produced no tasks: %q", head(plan.Output, 200))
	}
	log.Printf("manager: leader planned %d tasks", len(tasks))

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []any
		history = []any{fmt.Sprintf("tech-leader (%s) -> %d tasks", plan.Worker, len(tasks))}
		first   error
	)
	for _, t := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done, hist, err := m.runTask(ctx, t, brief)
			mu.Lock()
			defer mu.Unlock()
			history = append(history, hist...)
			if err != nil && first == nil {
				first = err
			}
			results = append(results, map[string]any{"id": done.ID, "title": done.Title, "by": done.Assignee, "rounds": done.Round, "status": done.Status})
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return graph.State{"results": results, "history": history}, nil
}

// parsePlan reads TASK|id|title|complexity|desc[|PREFER:<worker id>] lines.
func parsePlan(out string) []work {
	var tasks []work
	for _, line := range strings.Split(out, "\n") {
		p := strings.SplitN(strings.TrimSpace(line), "|", 6)
		if len(p) < 5 || p[0] != "TASK" {
			continue
		}
		t := work{ID: p[1], Title: p[2], Complexity: p[3], Desc: p[4]}
		if len(p) == 6 {
			if v, ok := strings.CutPrefix(strings.TrimSpace(p[5]), "PREFER:"); ok {
				t.Prefer = strings.TrimSpace(v)
			} else {
				t.Desc += "|" + p[5] // a "|" inside the description
			}
		}
		tasks = append(tasks, t)
	}
	return tasks
}

// workersBlock tells the leader who the developers are and what they are doing.
// Without the registry the leader still plans, just blind to the load.
func (m *manager) workersBlock(ctx context.Context) string {
	st, err := m.hub.State(ctx)
	if err != nil {
		log.Printf("manager: planning without the workers block: %v", err)
		return ""
	}
	var b strings.Builder
	for _, w := range st.Workers {
		if w.Role != "junior" && w.Role != "mid" && w.Role != "senior" {
			continue
		}
		job := "idle"
		if w.Job != "" {
			job = "busy with job " + w.Job
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", w.ID, w.Role, job)
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\n## Workers\n" + b.String()
}

// runTask drives one task until both gates approve or it runs out of rounds.
// A returned task goes back to the same engineer, with the feedback attached,
// on the same thread: the engineer remembers what it wrote.
func (m *manager) runTask(ctx context.Context, w work, brief string) (work, []any, error) {
	var hist []any
	prefer := preference[w.Complexity]
	if w.Prefer != "" {
		prefer = append([]string{w.Prefer}, prefer...)
	}
	for w.Round < MaxRounds {
		prompt := fmt.Sprintf("ROLE: engineer\n\n%s\n\n## Your task\nTASK: %s\nTITLE: %s\nCOMPLEXITY: %s\nDESCRIPTION: %s\nATTEMPT: %d\n",
			brief, w.ID, w.Title, w.Complexity, w.Desc, w.Round+1)
		if w.Feedback != "" {
			prompt += "\n## Feedback to address\n" + w.Feedback + "\n"
		}
		m.move(ctx, tasktrack.Task, subject(ctx, w.ID), tasktrack.InDevelopment, strings.Join(prefer, "/"), fmt.Sprintf("attempt %d", w.Round+1))
		j, err := m.dispatchJob(ctx, Job{Prefer: prefer, Pin: w.Round > 0}, w.ID+"-dev", prompt, w.Title+" "+w.Desc)
		if err != nil {
			return w, hist, err
		}
		w.Assignee, w.Level, w.Code, w.Feedback = j.Worker, j.Role, j.Output, ""
		prefer = []string{w.Assignee, w.Level} // rework goes back to the same person
		log.Printf("manager: %s built %s (attempt %d)", w.Assignee, w.ID, w.Round+1)
		hist = append(hist, fmt.Sprintf("%s: %s -> review", w.ID, w.Assignee))

		ok, err := m.jointReview(ctx, &w, &hist, brief)
		if err != nil {
			return w, hist, err
		}
		if ok {
			if ok, err = m.gate(ctx, &w, "qa", "qa", &hist, brief, tasktrack.InQATesting, tasktrack.QAApproved, tasktrack.QARejected); err != nil {
				return w, hist, err
			} else if ok {
				w.Status = "approved"
				// ponytail: "committed" means the approved code is recorded as the task's
				// deliverable; a real run would commit it in the sandbox first.
				m.move(ctx, tasktrack.Task, subject(ctx, w.ID), tasktrack.Committed, w.Assignee, "approved by review and QA")
				m.move(ctx, tasktrack.Task, subject(ctx, w.ID), tasktrack.Completed, w.Assignee, "")
				hist = append(hist, w.ID+": approved")
				return w, hist, nil
			}
		}
	}
	w.Status = "blocked"
	hist = append(hist, w.ID+": blocked")
	return w, hist, nil
}

// jointReview has the specialist and the tech lead judge the same submission in
// one gate. Both always run, so the developer gets every objection at once; the
// task passes only if both approve. A rejection costs one round, whoever made it.
func (m *manager) jointReview(ctx context.Context, w *work, hist *[]any, brief string) (bool, error) {
	m.move(ctx, tasktrack.Task, subject(ctx, w.ID), tasktrack.InCodeReview, "tech-leader/specialist", "built by "+w.Assignee)
	body := fmt.Sprintf("MODE: review\n\n%s\n\n## Task under review\nTASK: %s\nTITLE: %s\nDESCRIPTION: %s\nATTEMPT: %d\n\n## Submitted work\n%s\n",
		brief, w.ID, w.Title, w.Desc, w.Round+1, w.Code)
	reviewers := []struct{ role, label string }{{"specialist", "specialist"}, {"leader", "tech-leader"}}
	jobs := make([]Job, len(reviewers))
	errs := make([]error, len(reviewers))
	var wg sync.WaitGroup
	for i, r := range reviewers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			jobs[i], errs[i] = m.dispatch(ctx, []string{r.role}, w.ID+"-review-"+r.role, "ROLE: "+r.role+"\n"+body, w.Title+" "+w.Desc)
		}()
	}
	wg.Wait()
	var rejected, who, feedback []string
	for i, r := range reviewers {
		if errs[i] != nil {
			return false, errs[i]
		}
		who = append(who, jobs[i].Worker)
		if !strings.HasPrefix(field(jobs[i].Output, "VERDICT:"), "APPROVE") {
			rejected = append(rejected, jobs[i].Worker)
			feedback = append(feedback, r.label+": "+after(jobs[i].Output, "FEEDBACK:"))
		}
	}
	if len(rejected) == 0 {
		m.move(ctx, tasktrack.Task, subject(ctx, w.ID), tasktrack.ReviewApproved, strings.Join(who, "+"), "")
		log.Printf("manager: %s review approved %s", strings.Join(who, "+"), w.ID)
		*hist = append(*hist, fmt.Sprintf("%s: review approved by %s", w.ID, strings.Join(who, " and ")))
		return true, nil
	}
	w.Round++
	w.Feedback = strings.Join(feedback, "\n")
	m.move(ctx, tasktrack.Task, subject(ctx, w.ID), tasktrack.ReviewRejected, strings.Join(rejected, "+"), head(w.Feedback, 100))
	log.Printf("manager: review RETURNED %s by %s (round %d)", w.ID, strings.Join(rejected, "+"), w.Round)
	*hist = append(*hist, fmt.Sprintf("%s: review returned by %s -> %s | %s", w.ID, strings.Join(rejected, " and "), w.Assignee, head(w.Feedback, 160)))
	return false, nil
}

// gate runs one verdict step (review or QA). It reports whether the task passed;
// on a return it stores the feedback and counts the round.
func (m *manager) gate(ctx context.Context, w *work, role, label string, hist *[]any, brief string, start, approved, rejected tasktrack.Status) (bool, error) {
	m.move(ctx, tasktrack.Task, subject(ctx, w.ID), start, role, "built by "+w.Assignee)
	prompt := fmt.Sprintf("ROLE: %s\n\n%s\n\n## Task under %s\nTASK: %s\nTITLE: %s\nDESCRIPTION: %s\nATTEMPT: %d\n\n## Submitted work\n%s\n",
		role, brief, label, w.ID, w.Title, w.Desc, w.Round+1, w.Code)
	j, err := m.dispatch(ctx, []string{role}, w.ID+"-"+label, prompt, w.Title+" "+w.Desc)
	if err != nil {
		return false, err
	}
	if strings.HasPrefix(field(j.Output, "VERDICT:"), "APPROVE") {
		m.move(ctx, tasktrack.Task, subject(ctx, w.ID), approved, j.Worker, "")
		log.Printf("manager: %s %s approved %s", j.Worker, label, w.ID)
		*hist = append(*hist, fmt.Sprintf("%s: %s approved by %s", w.ID, label, j.Worker))
		return true, nil
	}
	w.Round++
	w.Feedback = after(j.Output, "FEEDBACK:")
	m.move(ctx, tasktrack.Task, subject(ctx, w.ID), rejected, j.Worker, head(w.Feedback, 100))
	log.Printf("manager: %s %s RETURNED %s (round %d)", j.Worker, label, w.ID, w.Round)
	*hist = append(*hist, fmt.Sprintf("%s: %s returned by %s -> %s", w.ID, label, j.Worker, w.Assignee))
	return false, nil
}

func (m *manager) deliver(ctx context.Context, s graph.State) (any, error) {
	var b strings.Builder
	rs, _ := s["results"].([]any)
	for _, r := range rs {
		x := r.(map[string]any)
		fmt.Fprintf(&b, "TASK %v | %v | by %v | rounds %v | %v\n", x["id"], x["title"], x["by"], x["rounds"], x["status"])
	}
	out, err := m.ask(ctx, "ROLE: manager\nMODE: report\n\n"+b.String())
	if err != nil {
		return nil, err
	}
	m.move(ctx, tasktrack.Request, subject(ctx, ""), tasktrack.Completed, "manager", "")
	status := map[string]any{}
	for k, v := range m.track.Snapshot() {
		if strings.HasPrefix(k, subject(ctx, "")) {
			status[strings.TrimPrefix(strings.TrimPrefix(k, subject(ctx, "")), "/")] = string(v)
		}
	}
	return graph.State{"report": out, "history": "manager -> requester", "status": status}, nil
}
