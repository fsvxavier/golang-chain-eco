package team

import (
	"context"
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
func NewManager(hub *HubClient, chat models.ChatModel, track *tasktrack.Tracker) (*graph.Compiled, error) {
	prompt, err := Skill("manager")
	if err != nil {
		return nil, err
	}
	agent, err := agents.Create(agents.Options{Model: chat, SystemPrompt: prompt})
	if err != nil {
		return nil, err
	}
	m := &manager{hub: hub, agent: agent, track: track}

	g := graph.NewStateGraph(graph.Schema{
		"request": channels.LastValue(), "kickoff": channels.LastValue(),
		"spec":    channels.LastValue(),
		"history": graph.Append(), "results": graph.Append(),
		"report": channels.LastValue(), "status": channels.LastValue(),
	})
	g.AddNode("manager", m.intake)
	g.AddNode("po", m.step("po", "product-owner", tasktrack.InSpecification, func(s graph.State) string {
		return fmt.Sprintf("ROLE: po\n\n## Request\n%v\n\n## Manager kickoff\n%v", s["request"], s["kickoff"])
	}, "spec"))
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
	m.move(ctx, tasktrack.Request, subject(ctx, ""), tasktrack.Received, "manager", head(fmt.Sprint(s["request"]), 80))
	out, err := m.ask(ctx, fmt.Sprintf("ROLE: manager\nMODE: intake\nREQUEST: %v", s["request"]))
	if err != nil {
		return nil, err
	}
	log.Printf("manager: request %s routed to %s", requestID(ctx), field(out, "ROUTE:"))
	return graph.State{"kickoff": field(out, "KICKOFF:"), "history": "manager -> product-owner"}, nil
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
	plan, err := m.dispatch(ctx, []string{"leader"}, "leader", "ROLE: leader\n\n"+brief, fmt.Sprint(s["request"]))
	if err != nil {
		return nil, err
	}
	var tasks []work
	for _, line := range strings.Split(plan.Output, "\n") {
		if p := strings.SplitN(strings.TrimSpace(line), "|", 5); len(p) == 5 && p[0] == "TASK" {
			tasks = append(tasks, work{ID: p[1], Title: p[2], Complexity: p[3], Desc: p[4]})
			m.move(ctx, tasktrack.Task, subject(ctx, p[1]), tasktrack.TaskAssignment, "tech-leader", p[2]+" ("+p[3]+")")
		}
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

// runTask drives one task until both gates approve or it runs out of rounds.
// A returned task goes back to the same engineer, with the feedback attached,
// on the same thread: the engineer remembers what it wrote.
func (m *manager) runTask(ctx context.Context, w work, brief string) (work, []any, error) {
	var hist []any
	prefer := preference[w.Complexity]
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

		ok, err := m.gate(ctx, &w, "specialist", "review", &hist, brief, tasktrack.InCodeReview, tasktrack.ReviewApproved, tasktrack.ReviewRejected)
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
