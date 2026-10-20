// engineering_team: an engineering organisation as one graph. A manager routes
// the request, a product owner writes the spec, an architect assesses risk, a
// tech leader splits the work and the engineers build it in parallel; every
// task then passes a specialist code review and QA, and goes back to its
// engineer when either gate returns it. All agents run in this process.
//
// Two things keep the work from going wrong:
//   - a registry that reserves people atomically (two tasks never get the same
//     engineer, reviewer or QA) and queues tasks while everyone is busy;
//   - context that travels with the work: every task carries the full dossier
//     (request, spec, architect constraints) and the feedback it received, and
//     the graph state keeps the handoff history on a checkpointed thread.
//
// Each agent's persona is a skill under skills/<name>/SKILL.md.
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

//go:embed skills/*/SKILL.md
var skillFS embed.FS

// skill returns the body of skills/<name>/SKILL.md without its frontmatter.
func skill(name string) string {
	b, err := skillFS.ReadFile("skills/" + name + "/SKILL.md")
	if err != nil {
		log.Fatal(err)
	}
	s := string(b)
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			return strings.TrimSpace(body)
		}
	}
	return s
}

// maxRounds is how many times a task may be returned before it is declared blocked.
const maxRounds = 3

// work is one task travelling through dev, review and QA. It is the Send
// argument, so it carries everything its next stop needs: nothing is read
// from global state.
type work struct {
	ID, Title, Complexity, Desc string
	Brief                       string // dossier: request, spec, architect constraints
	Assignee, Level             string // registry member that owns the task, and its role
	Code, Feedback              string
	Round                       int
	Status                      string
}

func (w work) arg() graph.State { return graph.State{"work": w} }

// registry reserves people. Claims are atomic under one lock, so concurrent
// nodes never get the same person.
// ponytail: process-local mutex; across processes this needs a store with
// compare-and-swap (or a lease) instead.
type registry struct {
	mu      sync.Mutex
	members []*member
	backlog []work
}

type member struct{ id, role, task string } // task != "" means reserved

func newRegistry() *registry {
	r := &registry{}
	for _, m := range []member{
		{id: "junior-1", role: "junior"}, {id: "junior-2", role: "junior"},
		{id: "mid-1", role: "mid"}, {id: "senior-1", role: "senior"},
		{id: "specialist-1", role: "specialist"}, {id: "specialist-2", role: "specialist"},
		{id: "qa-1", role: "qa"}, {id: "qa-2", role: "qa"},
	} {
		r.members = append(r.members, &member{id: m.id, role: m.role})
	}
	return r
}

// preference is who suits a task best, in order. High-complexity work never
// goes to a junior.
var preference = map[string][]string{
	"low":    {"junior", "mid", "senior"},
	"medium": {"mid", "senior", "junior"},
	"high":   {"senior", "mid"},
}

func (r *registry) claimLocked(roles []string, task string) *member {
	for _, role := range roles {
		for _, m := range r.members {
			if m.role == role && m.task == "" {
				m.task = task
				return m
			}
		}
	}
	return nil
}

// claim reserves the first free member of the first role that has one.
func (r *registry) claim(roles []string, task string) *member {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.claimLocked(roles, task)
}

// wait blocks until a member of role is free.
// ponytail: polling; a sync.Cond or channel per role if the pool gets large.
func (r *registry) wait(ctx context.Context, role, task string) (*member, error) {
	for {
		if m := r.claim([]string{role}, task); m != nil {
			return m, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (r *registry) release(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.members {
		if m.id == id {
			m.task = ""
		}
	}
}

func (r *registry) enqueue(w work) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backlog = append(r.backlog, w)
}

// next assigns the first queued task that now has a suitable free engineer.
func (r *registry) next() (work, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, w := range r.backlog {
		if m := r.claimLocked(preference[w.Complexity], w.ID); m != nil {
			r.backlog = append(r.backlog[:i], r.backlog[i+1:]...)
			w.Assignee, w.Level = m.id, m.role
			return w, true
		}
	}
	return work{}, false
}

// --- text helpers: agents answer in the formats their skills define.

// field returns the rest of the line that starts with key.
func field(text, key string) string {
	for _, l := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), key); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// after returns everything from key (inclusive of the rest of its line) to the end.
func after(text, key string) string {
	if _, v, ok := strings.Cut(text, key); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func say(format string, a ...any) { fmt.Printf(format+"\n", a...) }

// ask sends one prompt to an agent and returns its last message.
func ask(ctx context.Context, a *graph.Compiled, prompt string) (string, error) {
	out, err := a.Invoke(ctx, graph.State{"messages": []messages.Message{messages.Human(prompt)}})
	if err != nil {
		return "", err
	}
	return demo.Last(out), nil
}

func newAgent(model agents.Options, name string) *graph.Compiled {
	model.SystemPrompt = skill(name)
	a, err := agents.Create(model)
	if err != nil {
		log.Fatal(err)
	}
	return a
}

func dossier(s graph.State) string {
	return fmt.Sprintf("## Request\n%v\n\n## Specification\n%v\n\n## Architect assessment\n%v", s["request"], s["spec"], s["arch"])
}

func main() {
	// One model for the whole team; the persona comes from each agent's skill.
	// Offline it is a scripted fake that answers by the ROLE in the prompt.
	opts := agents.Options{Model: demo.Smart(func(msgs []messages.Message) messages.Message {
		return messages.AI(brain(demo.LastHuman(msgs)))
	})}
	manager := newAgent(opts, "engineering-manager")
	po := newAgent(opts, "product-owner")
	architect := newAgent(opts, "architect")
	leader := newAgent(opts, "tech-leader")
	engineers := map[string]*graph.Compiled{
		"junior": newAgent(opts, "software-engineer-junior"),
		"mid":    newAgent(opts, "software-engineer-mid"),
		"senior": newAgent(opts, "software-engineer-senior"),
	}
	specialist := newAgent(opts, "specialist-reviewer")
	qa := newAgent(opts, "qa-engineer")
	reg := newRegistry()

	// gate runs a review or QA step: it reserves a free person, asks the agent
	// for a verdict and either forwards the task or returns it to its engineer
	// with the feedback attached.
	gate := func(agent *graph.Compiled, role, label, onApprove string) graph.NodeFunc {
		return func(ctx context.Context, s graph.State) (any, error) {
			w := s["work"].(work)
			m, err := reg.wait(ctx, role, w.ID)
			if err != nil {
				return nil, err
			}
			defer reg.release(m.id)
			prompt := fmt.Sprintf("ROLE: %s\nWHO: %s\n\n%s\n\n## Task under %s\nTASK: %s\nTITLE: %s\nDESCRIPTION: %s\nATTEMPT: %d\n\n## Submitted work\n%s\n",
				role, m.id, w.Brief, label, w.ID, w.Title, w.Desc, w.Round+1, w.Code)
			out, err := ask(ctx, agent, prompt)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(field(out, "VERDICT:"), "APPROVE") {
				say("  %-12s %s approved %s", m.id, label, w.ID)
				return graph.Command{
					Update: graph.State{"history": fmt.Sprintf("%s: %s approved by %s", w.ID, label, m.id)},
					Send:   []graph.Send{{Node: onApprove, Arg: w.arg()}},
				}, nil
			}
			w.Round++
			w.Feedback = after(out, "FEEDBACK:")
			say("  %-12s %s RETURNED %s (round %d)", m.id, label, w.ID, w.Round)
			hist := fmt.Sprintf("%s: %s returned by %s -> %s", w.ID, label, m.id, w.Assignee)
			next := "dev"
			if w.Round >= maxRounds {
				w.Status, next = "blocked", "done"
			}
			return graph.Command{Update: graph.State{"history": hist}, Send: []graph.Send{{Node: next, Arg: w.arg()}}}, nil
		}
	}

	g := graph.NewStateGraph(graph.Schema{
		"request": channels.LastValue(), "kickoff": channels.LastValue(),
		"spec": channels.LastValue(), "arch": channels.LastValue(),
		"total":   channels.LastValue(),
		"history": graph.Append(), "results": graph.Append(),
		"report": channels.LastValue(),
	})

	g.AddNode("manager", func(ctx context.Context, s graph.State) (any, error) {
		out, err := ask(ctx, manager, fmt.Sprintf("ROLE: manager\nMODE: intake\nREQUEST: %v", s["request"]))
		if err != nil {
			return nil, err
		}
		if strings.Contains(out, "```") { // the manager coordinates; it never writes code
			return nil, fmt.Errorf("manager wrote code: %q", out)
		}
		say("manager      -> %s", field(out, "ROUTE:"))
		return graph.State{"kickoff": field(out, "KICKOFF:"), "history": "manager -> product-owner"}, nil
	})

	g.AddNode("po", func(ctx context.Context, s graph.State) (any, error) {
		out, err := ask(ctx, po, fmt.Sprintf("ROLE: po\n\n## Request\n%v\n\n## Manager kickoff\n%v", s["request"], s["kickoff"]))
		if err != nil {
			return nil, err
		}
		say("po           wrote the spec (%d acceptance criteria)", strings.Count(out, "\nAC"))
		return graph.State{"spec": out, "history": "product-owner -> architect"}, nil
	})

	g.AddNode("architect", func(ctx context.Context, s graph.State) (any, error) {
		out, err := ask(ctx, architect, fmt.Sprintf("ROLE: architect\n\n## Request\n%v\n\n## Specification\n%v", s["request"], s["spec"]))
		if err != nil {
			return nil, err
		}
		say("architect    risk=%s", field(out, "RISK:"))
		return graph.State{"arch": out, "history": "architect -> tech-leader"}, nil
	})

	g.AddNode("leader", func(ctx context.Context, s graph.State) (any, error) {
		brief := dossier(s)
		out, err := ask(ctx, leader, "ROLE: leader\n\n"+brief)
		if err != nil {
			return nil, err
		}
		var sends []graph.Send
		total := 0
		for _, line := range strings.Split(out, "\n") {
			p := strings.SplitN(strings.TrimSpace(line), "|", 5)
			if len(p) != 5 || p[0] != "TASK" {
				continue
			}
			total++
			w := work{ID: p[1], Title: p[2], Complexity: p[3], Desc: p[4], Brief: brief}
			if m := reg.claim(preference[w.Complexity], w.ID); m != nil {
				w.Assignee, w.Level = m.id, m.role
				say("leader       %s (%s) -> %s", w.ID, w.Complexity, m.id)
				sends = append(sends, graph.Send{Node: "dev", Arg: w.arg()})
			} else {
				say("leader       %s (%s) -> queued, nobody suitable is free", w.ID, w.Complexity)
				reg.enqueue(w)
			}
		}
		return graph.Command{Update: graph.State{"total": total, "history": fmt.Sprintf("tech-leader -> %d tasks", total)}, Send: sends}, nil
	})

	g.AddNode("dev", func(ctx context.Context, s graph.State) (any, error) {
		w := s["work"].(work)
		prompt := fmt.Sprintf("ROLE: engineer-%s\nWHO: %s\n\n%s\n\n## Your task\nTASK: %s\nTITLE: %s\nCOMPLEXITY: %s\nDESCRIPTION: %s\nATTEMPT: %d\n",
			w.Level, w.Assignee, w.Brief, w.ID, w.Title, w.Complexity, w.Desc, w.Round+1)
		if w.Feedback != "" {
			prompt += "\n## Feedback to address\n" + w.Feedback + "\n"
		}
		out, err := ask(ctx, engineers[w.Level], prompt)
		if err != nil {
			return nil, err
		}
		w.Code, w.Feedback = out, ""
		say("  %-12s built %s (attempt %d)", w.Assignee, w.ID, w.Round+1)
		return graph.Command{
			Update: graph.State{"history": fmt.Sprintf("%s: %s -> review", w.ID, w.Assignee)},
			Send:   []graph.Send{{Node: "review", Arg: w.arg()}},
		}, nil
	})

	g.AddNode("review", gate(specialist, "specialist", "review", "qa"))
	g.AddNode("qa", gate(qa, "qa", "qa", "done"))

	// done frees the engineer and hands the next queued task to whoever fits.
	g.AddNode("done", func(_ context.Context, s graph.State) (any, error) {
		w := s["work"].(work)
		if w.Status == "" {
			w.Status = "approved"
		}
		reg.release(w.Assignee)
		var sends []graph.Send
		for {
			nw, ok := reg.next()
			if !ok {
				break
			}
			say("  registry     %s freed; queued %s -> %s", w.Assignee, nw.ID, nw.Assignee)
			sends = append(sends, graph.Send{Node: "dev", Arg: nw.arg()})
		}
		return graph.Command{
			Update: graph.State{
				"results": map[string]any{"id": w.ID, "title": w.Title, "by": w.Assignee, "rounds": w.Round, "status": w.Status},
				"history": fmt.Sprintf("%s: %s", w.ID, w.Status),
			},
			Send: sends,
		}, nil
	})

	// deliver is deferred: it runs once every task has left the pipeline.
	g.AddNode("deliver", func(ctx context.Context, s graph.State) (any, error) {
		results, _ := s["results"].([]any)
		if total, _ := s["total"].(int); len(results) < total {
			return nil, nil
		}
		var b strings.Builder
		for _, r := range results {
			m := r.(map[string]any)
			fmt.Fprintf(&b, "TASK %v | %v | by %v | rounds %v | %v\n", m["id"], m["title"], m["by"], m["rounds"], m["status"])
		}
		out, err := ask(ctx, manager, "ROLE: manager\nMODE: report\n\n"+b.String())
		if err != nil {
			return nil, err
		}
		if strings.Contains(out, "```") {
			return nil, fmt.Errorf("manager wrote code: %q", out)
		}
		return graph.State{"report": out, "history": "manager -> requester"}, nil
	}, graph.Deferred())

	g.AddEdge(graph.Start, "manager").AddEdge("manager", "po").AddEdge("po", "architect").
		AddEdge("architect", "leader").AddEdge("done", "deliver").AddEdge("deliver", graph.End)

	app, err := g.Compile(graph.WithName("engineering_team"), graph.WithCheckpointer(checkpoint.NewMemory()))
	if err != nil {
		log.Fatal(err)
	}

	demo.Title("engineering team")
	ctx := runnables.WithConfig(context.Background(), graph.Thread("coupons"))
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := app.Invoke(ctx, graph.State{"request": "Let shoppers apply a discount coupon at checkout. The last coupon must never be consumed twice, even when two payments race or a payment fails."})
	if err != nil {
		log.Fatal(err)
	}

	demo.Title("report")
	fmt.Println(out["report"])
	demo.Title("handoff history (kept on the checkpointed thread)")
	for i, h := range out["history"].([]any) {
		fmt.Printf("%2d. %v\n", i+1, h)
	}
	snap, err := app.GetState(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nthread %q persisted %d handoffs; %d tasks finished\n", "coupons", len(snap.Values["history"].([]any)), len(out["results"].([]any)))
}
