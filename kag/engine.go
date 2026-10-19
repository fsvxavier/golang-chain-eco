package kag

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
)

// Plan is the multi-hop decomposition of a question.
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// PlanStep is one hop. Entity may be "$<id>" to start from the entities
// reached by an earlier step; DependsOn does the same when Entity is empty.
type PlanStep struct {
	ID        string   `json:"id"`
	Goal      string   `json:"goal"`
	Entity    string   `json:"entity"`
	Relations []string `json:"relations"`
	Dir       string   `json:"dir"`
	DependsOn string   `json:"depends_on"`
}

// Verdict is the outcome of the verification step.
type Verdict struct {
	Supported   bool     `json:"supported"`
	Unsupported []string `json:"unsupported"`
}

// Engine answers questions by planning hops over a GraphStore, collecting the
// triples it reaches as evidence and synthesising a sourced answer. It is a
// knowledge.Answerer with Strategy "kag".
type Engine struct {
	Model    models.ChatModel
	Graph    GraphStore
	Embedder embeddings.Embedder // optional: vector entity linking
	Chunks   knowledge.Answerer  // optional: fallback to vector search over chunks
	MaxSteps int                 // default 4
	MaxHops  int                 // default 3
	Verify   bool                // second model call checks the answer against the evidence
	System   string              // synthesis instruction; default knowledge.DefaultSystem
}

var _ knowledge.Answerer = (*Engine)(nil)

const (
	defaultMaxSteps = 4
	defaultMaxHops  = 3
	seedLimit       = 5
	planSystem      = "Você planeja a resposta a uma pergunta sobre um grafo de conhecimento. " +
		"Decomponha-a em no máximo %d passos encadeados. Cada passo tem id curto, goal, entity " +
		"(nome da entidade de partida, ou \"$<id>\" para partir das entidades alcançadas por um passo anterior), " +
		"relations (tipos a percorrer; vazio = todos) e dir (out, in ou both)."
	verifySystem = "Verifique se cada afirmação da resposta é sustentada pela evidência numerada. " +
		"Responda supported=false se alguma não for, listando-a em unsupported."
)

// Answer implements knowledge.Answerer.
func (e *Engine) Answer(ctx context.Context, q knowledge.Question) (knowledge.Answer, error) {
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return knowledge.Answer{}, knowledge.ErrEmptyQuestion
	}
	if e.Model == nil || e.Graph == nil {
		return knowledge.Answer{}, errors.New("kag: engine needs Model and Graph")
	}
	maxSteps, maxHops := e.MaxSteps, e.MaxHops
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	if maxHops <= 0 {
		maxHops = defaultMaxHops
	}

	plan, err := e.plan(ctx, text, maxSteps)
	if err != nil {
		return knowledge.Answer{}, err
	}

	r := &run{e: e, q: q, maxHops: maxHops, outputs: map[string][]string{}, seen: map[string]bool{}, ids: map[string]bool{}}
	for i, s := range plan.Steps {
		if s.ID == "" {
			s.ID = strconv.Itoa(i + 1)
		}
		if err := r.step(ctx, s); err != nil {
			return knowledge.Answer{}, err
		}
	}

	ans := knowledge.Answer{Strategy: "kag", Sources: r.sources, Trace: r.trace}
	sys := e.System
	if sys == "" {
		sys = knowledge.DefaultSystem
	}
	msg, err := e.Model.Generate(ctx, knowledge.Prompt(sys, r.sources, q))
	if err != nil {
		return knowledge.Answer{}, fmt.Errorf("kag: synthesis: %w", err)
	}
	ans.Text, ans.Usage = msg.Text(), msg.Usage

	if e.Verify {
		if err := e.verify(ctx, text, &ans); err != nil {
			return knowledge.Answer{}, err
		}
	}
	return ans, nil
}

func (e *Engine) plan(ctx context.Context, question string, maxSteps int) (p Plan, err error) {
	ctx, end := callbacks.Start(ctx, callbacks.ChainStart, "kag.plan", question)
	defer func() { end(p, err) }()
	p, err = models.Structured[Plan](e.Model, models.MethodTool).Invoke(ctx, []messages.Message{
		messages.System(fmt.Sprintf(planSystem, maxSteps)),
		messages.Human(question),
	})
	if err != nil {
		return Plan{}, fmt.Errorf("kag: plan: %w", err)
	}
	if len(p.Steps) == 0 {
		return Plan{}, errors.New("kag: plan has no steps")
	}
	if len(p.Steps) > maxSteps {
		p.Steps = p.Steps[:maxSteps]
	}
	return p, nil
}

// run is the state of one Answer call.
type run struct {
	e       *Engine
	q       knowledge.Question
	maxHops int
	outputs map[string][]string // step id -> entities it reached
	seen    map[string]bool     // triples already collected as evidence
	ids     map[string]bool     // source ids already attached
	sources []knowledge.Source
	trace   []knowledge.Step
}

func (r *run) step(ctx context.Context, s PlanStep) (err error) {
	ctx, end := callbacks.Start(ctx, callbacks.ChainStart, "kag.step:"+s.ID, s)
	defer func() { end(nil, err) }()

	dir, err := parseDir(s.Dir)
	if err != nil {
		return fmt.Errorf("kag: step %q: %w", s.ID, err)
	}
	seeds, err := r.seeds(ctx, s)
	if err != nil {
		return fmt.Errorf("kag: step %q: %w", s.ID, err)
	}
	reached, fresh, err := r.traverse(ctx, seeds, s.Relations, dir)
	if err != nil {
		return fmt.Errorf("kag: step %q: %w", s.ID, err)
	}
	r.outputs[s.ID] = reached

	detail := "sem triplas no grafo"
	if len(fresh) > 0 {
		parts := make([]string, len(fresh))
		for i, rel := range fresh {
			r.addSource(tripleSource(rel))
			parts[i] = tripleText(rel)
		}
		detail = strings.Join(parts, "; ")
	}
	if len(fresh) == 0 && r.e.Chunks != nil {
		goal := s.Goal
		if goal == "" {
			goal = r.q.Text
		}
		ca, err := r.e.Chunks.Answer(ctx, knowledge.Question{Text: goal, Filter: r.q.Filter})
		if err != nil {
			return fmt.Errorf("kag: step %q chunks: %w", s.ID, err)
		}
		for _, src := range ca.Sources {
			r.addSource(src)
		}
		detail += fmt.Sprintf("; completado com %d fontes de chunks", len(ca.Sources))
	}
	r.trace = append(r.trace, knowledge.Step{Name: "step:" + s.ID, Detail: detail})
	return nil
}

// seeds resolves the starting entities of a step: a reference to an earlier
// step, or entity linking on the entity text (falling back to the goal).
func (r *run) seeds(ctx context.Context, s PlanStep) ([]string, error) {
	ref := strings.TrimSpace(s.Entity)
	if ref == "" && s.DependsOn != "" {
		ref = "$" + strings.TrimPrefix(s.DependsOn, "$")
	}
	if id, ok := strings.CutPrefix(ref, "$"); ok {
		ids, known := r.outputs[id]
		if !known {
			return nil, fmt.Errorf("references unknown or later step %q", id)
		}
		return ids, nil
	}
	text := ref
	if text == "" {
		text = s.Goal
	}
	if text == "" {
		return nil, nil
	}
	var vec []float32
	if r.e.Embedder != nil {
		v, err := r.e.Embedder.EmbedQuery(ctx, text)
		if err != nil {
			return nil, err
		}
		vec = v
	}
	ents, err := r.e.Graph.FindEntities(ctx, text, vec, seedLimit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(ents))
	for i, en := range ents {
		ids[i] = en.ID
	}
	return ids, nil
}

// traverse expands seeds breadth-first for at most maxHops hops. It returns the
// entities at the far end of the relations it followed and the triples not seen
// by earlier steps. Visited entities bound the walk, so cycles terminate.
func (r *run) traverse(ctx context.Context, seeds, relTypes []string, dir Direction) ([]string, []Relation, error) {
	visited := map[string]bool{}
	var frontier []string
	for _, id := range seeds {
		if !visited[id] {
			visited[id] = true
			frontier = append(frontier, id)
		}
	}
	var reached []string
	reachedSet := map[string]bool{}
	var fresh []Relation
	for hop := 0; hop < r.maxHops && len(frontier) > 0; hop++ {
		var next []string
		for _, id := range frontier {
			edges, err := r.e.Graph.Neighbors(ctx, id, relTypes, dir)
			if err != nil {
				return nil, nil, err
			}
			for _, rel := range edges {
				if k := tripleKey(rel); !r.seen[k] {
					r.seen[k] = true
					fresh = append(fresh, rel)
				}
				other := rel.To
				if other == id {
					other = rel.From
				}
				if !reachedSet[other] {
					reachedSet[other] = true
					reached = append(reached, other)
				}
				if !visited[other] {
					visited[other] = true
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	return reached, fresh, nil
}

func (r *run) addSource(s knowledge.Source) {
	if s.ID != "" {
		if r.ids[s.ID] {
			return
		}
		r.ids[s.ID] = true
	}
	r.sources = append(r.sources, s)
}

func (e *Engine) verify(ctx context.Context, question string, ans *knowledge.Answer) (err error) {
	ctx, end := callbacks.Start(ctx, callbacks.ChainStart, "kag.verify", ans.Text)
	defer func() { end(nil, err) }()
	v, err := models.Structured[Verdict](e.Model, models.MethodTool).Invoke(ctx,
		knowledge.Prompt(verifySystem, ans.Sources, knowledge.Question{
			Text: "Pergunta: " + question + "\n\nResposta a verificar:\n" + ans.Text,
		}))
	if err != nil {
		return fmt.Errorf("kag: verify: %w", err)
	}
	if v.Supported {
		return nil
	}
	detail := strings.Join(v.Unsupported, "; ")
	if detail == "" {
		detail = "resposta não suportada pela evidência"
	}
	ans.Trace = append(ans.Trace, knowledge.Step{Name: "unverified", Detail: detail})
	ans.Text = "(não verificado) " + ans.Text
	return nil
}

func parseDir(s string) (Direction, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "both":
		return Both, nil
	case "out":
		return Out, nil
	case "in":
		return In, nil
	}
	return Both, fmt.Errorf("invalid dir %q (want out, in or both)", s)
}

func tripleKey(r Relation) string { return r.From + "|" + r.Type + "|" + r.To }

func tripleText(r Relation) string { return fmt.Sprintf("%s —%s→ %s", r.From, r.Type, r.To) }

func tripleSource(r Relation) knowledge.Source {
	return knowledge.Source{ID: tripleKey(r), Content: tripleText(r), Metadata: map[string]any{"source": r.Source}}
}
