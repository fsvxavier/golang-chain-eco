package team

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/internal/tasktrack"
)

//go:embed dashboard.html
var dashboardHTML []byte

// DashTokens is token consumption.
type DashTokens struct {
	In    int `json:"in"`
	Out   int `json:"out"`
	Cache int `json:"cache"` // read from the prompt cache, already counted in In
	Total int `json:"total"`
}

func (t *DashTokens) add(u *messages.Usage) {
	if u == nil {
		return
	}
	t.In += u.InputTokens
	t.Out += u.OutputTokens
	t.Cache += u.CacheReadTokens
	if u.TotalTokens > 0 {
		t.Total += u.TotalTokens
	} else {
		t.Total += u.InputTokens + u.OutputTokens
	}
}

// DashTask is one task of a request as the dashboard shows it.
type DashTask struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Agent     string     `json:"agent"`
	Detail    string     `json:"detail,omitempty"`
	Rounds    int        `json:"rounds"` // development attempts
	Tokens    DashTokens `json:"tokens"`
	Progress  int        `json:"progress"`
	StartedMS int64      `json:"started_ms"`
	EndedMS   int64      `json:"ended_ms,omitempty"`
}

// DashRequest is one demand with its tasks.
type DashRequest struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Agent     string     `json:"agent"`
	Progress  int        `json:"progress"`
	StartedMS int64      `json:"started_ms"`
	EndedMS   int64      `json:"ended_ms,omitempty"`
	Tokens    DashTokens `json:"tokens"` // tasks, request-level roles and the manager
	Tasks     []DashTask `json:"tasks"`
}

// DashPersona is one worker: who it is, what it does now, how much it has done.
type DashPersona struct {
	ID       string     `json:"id"`
	Role     string     `json:"role"`
	Model    string     `json:"model,omitempty"`
	State    string     `json:"state"` // busy, idle, offline
	Job      string     `json:"job,omitempty"`
	JobMS    int64      `json:"job_ms,omitempty"` // running time of the current job
	Skill    string     `json:"skill,omitempty"`
	Refs     []string   `json:"refs,omitempty"`
	Activity string     `json:"activity,omitempty"` // what it is doing now (reference, tool, model)
	Done     int        `json:"done"`
	Failed   int        `json:"failed"`
	BusyMS   int64      `json:"busy_ms"` // total time spent on finished and running jobs
	Tokens   DashTokens `json:"tokens"`
}

// DashState is what GET /api/state returns.
type DashState struct {
	NowMS    int64             `json:"now_ms"`
	Requests []DashRequest     `json:"requests"`
	Personas []DashPersona     `json:"personas"`
	Events   []tasktrack.Event `json:"events"` // newest first, capped
	Queued   int               `json:"queued"`
	Tokens   DashTokens        `json:"tokens"` // the whole project
	HubError string            `json:"hub_error,omitempty"`
	Project  string            `json:"project"`      // name of the workspace this team serves
	Root     string            `json:"project_root"` // its absolute path
}

// taskProgress is how far along a task is, by status. A rejection sends the
// task back to development, so its bar steps back with it.
var taskProgress = map[tasktrack.Status]int{
	tasktrack.TaskAssignment: 10, tasktrack.InDevelopment: 30, tasktrack.InCodeReview: 55,
	tasktrack.ReviewRejected: 30, tasktrack.ReviewApproved: 65, tasktrack.InQATesting: 75,
	tasktrack.QARejected: 30, tasktrack.QAApproved: 90, tasktrack.Committed: 95, tasktrack.Completed: 100,
}

// BuildDash derives the dashboard state from the tracker's moves, the hub's
// registry and its jobs. It is pure so tests can feed it fixed data.
func BuildDash(now time.Time, events []tasktrack.Event, hub State, jobs []Job, mgr map[string]messages.Usage) DashState {
	reqs := map[string]*DashRequest{}
	var order []string
	get := func(id string) *DashRequest {
		r := reqs[id]
		if r == nil {
			r = &DashRequest{ID: id}
			reqs[id] = r
			order = append(order, id)
		}
		return r
	}
	for _, e := range events {
		ms := e.Time.UnixMilli()
		rid, tid, isTask := strings.Cut(e.Subject, "/")
		r := get(rid)
		if r.StartedMS == 0 {
			r.StartedMS = ms
		}
		if !isTask {
			if r.Title == "" {
				r.Title = e.Detail
			}
			r.Status, r.Agent = string(e.To), e.Agent
			if e.To == tasktrack.Completed || e.To == tasktrack.Cancelled {
				r.EndedMS = ms
			}
			continue
		}
		i := slices.IndexFunc(r.Tasks, func(t DashTask) bool { return t.ID == tid })
		if i < 0 {
			r.Tasks = append(r.Tasks, DashTask{ID: tid, Title: e.Detail, StartedMS: ms})
			i = len(r.Tasks) - 1
		}
		t := &r.Tasks[i]
		t.Status, t.Agent, t.Detail, t.Progress = string(e.To), e.Agent, e.Detail, taskProgress[e.To]
		if e.To == tasktrack.InDevelopment {
			t.Rounds++
		}
		if e.To == tasktrack.Completed {
			t.EndedMS = ms
		}
	}
	for _, r := range reqs { // a cancelled request leaves its unfinished tasks cancelled
		if r.Status == string(tasktrack.Cancelled) {
			for i := range r.Tasks {
				if r.Tasks[i].Status != string(tasktrack.Completed) {
					r.Tasks[i].Status = string(tasktrack.Cancelled)
					if r.Tasks[i].EndedMS == 0 {
						r.Tasks[i].EndedMS = r.EndedMS
					}
				}
			}
		}
	}
	out := DashState{NowMS: now.UnixMilli(), Requests: []DashRequest{}, Events: []tasktrack.Event{}, Personas: []DashPersona{}, Queued: len(hub.Queued)}
	for _, id := range order {
		r := reqs[id]
		if r.Tasks == nil {
			r.Tasks = []DashTask{} // JSON [] not null: the page loops over it
		}
		sum := 0
		for _, t := range r.Tasks {
			sum += t.Progress
		}
		switch {
		case r.Status == string(tasktrack.Completed):
			r.Progress = 100
		case len(r.Tasks) > 0:
			r.Progress = 20 + 75*sum/(100*len(r.Tasks))
		case r.Status == string(tasktrack.InSpecification):
			r.Progress = 15
		default:
			r.Progress = 5
		}
		out.Requests = append(out.Requests, *r)
	}
	slices.Reverse(out.Requests) // newest request first

	byID := map[string]*DashPersona{}
	add := func(id, role, model, state, job, activity string) {
		byID[id] = &DashPersona{ID: id, Role: role, Model: model, State: state, Job: job, Activity: activity, Skill: skillDir[role], Refs: skillRefs(role)}
		out.Personas = append(out.Personas, *byID[id])
	}
	for _, w := range hub.Workers {
		st := "idle"
		if w.Job != "" {
			st = "busy"
		}
		add(w.ID, w.Role, w.Model, st, w.Job, w.Activity)
	}
	for _, c := range Cast {
		if byID[c.ID] == nil {
			add(c.ID, c.Role, "", "offline", "", "")
		}
	}
	idx := map[string]int{}
	for i, p := range out.Personas {
		idx[p.ID] = i
	}
	// Tokens: a job belongs to its request (Scope) and, when its thread starts
	// with a task id, to that task; the manager's own tokens go to the request.
	reqAt := map[string]int{}
	for i, r := range out.Requests {
		reqAt[r.ID] = i
	}
	rid := func(full string) string { return full[:min(len(full), 8)] }
	for _, j := range jobs {
		out.Tokens.add(j.Usage)
		ri, ok := reqAt[rid(j.Scope)]
		if !ok || j.Scope == "" {
			continue
		}
		r := &out.Requests[ri]
		r.Tokens.add(j.Usage)
		rest := strings.TrimPrefix(j.Thread, j.Scope+"-")
		for ti := range r.Tasks {
			if id := r.Tasks[ti].ID; rest == id || strings.HasPrefix(rest, id+"-") {
				r.Tasks[ti].Tokens.add(j.Usage)
			}
		}
	}
	mgrTotal := DashTokens{}
	for id, u := range mgr {
		u := u
		out.Tokens.add(&u)
		mgrTotal.add(&u)
		if ri, ok := reqAt[rid(id)]; ok {
			out.Requests[ri].Tokens.add(&u)
		}
	}
	if mgrTotal.Total > 0 { // the manager is no worker, but it spends tokens too
		out.Personas = append(out.Personas, DashPersona{ID: "manager", Role: "manager", State: "-", Skill: skillDir["manager"], Refs: skillRefs("manager"), Tokens: mgrTotal})
	}
	for _, j := range jobs {
		i, ok := idx[j.Worker]
		if !ok || j.StartedMS == 0 {
			continue
		}
		p := &out.Personas[i]
		p.Tokens.add(j.Usage)
		switch {
		case j.Status == "done":
			p.Done++
			p.BusyMS += j.DoneMS - j.StartedMS
		case j.Status == "failed":
			p.Failed++
			p.BusyMS += j.DoneMS - j.StartedMS
		case j.Status == "running" && j.ID == p.Job:
			p.JobMS = now.UnixMilli() - j.StartedMS
			p.BusyMS += p.JobMS
		}
	}
	for n, e := 0, len(events); n < 200 && n < e; n++ {
		out.Events = append(out.Events, events[e-1-n])
	}
	return out
}

// project is what one team process knows about the project it serves.
type project struct {
	track *tasktrack.Tracker
	hub   *HubClient
	root  string
}

func (p project) state(ctx context.Context) DashState {
	st, err1 := p.hub.State(ctx)
	jobs, err2 := p.hub.Jobs(ctx)
	mgr, err3 := p.hub.MgrUsage(ctx)
	d := BuildDash(time.Now(), p.track.History(), st, jobs, mgr)
	d.Root, d.Project = p.root, filepath.Base(p.root)
	if err := errors.Join(err1, err2, err3); err != nil {
		d.HubError = err.Error()
	}
	return d
}

// DashboardHandler serves one project's read-only progress page and its JSON
// feed. It shows statuses, timings and token counts only, never prompts or
// outputs.
func DashboardHandler(track *tasktrack.Tracker, hub *HubClient, root string) http.Handler {
	return project{track, hub, root}.handler()
}

func (p project) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(dashboardHTML)
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(p.state(ctx))
	})
	return mux
}
