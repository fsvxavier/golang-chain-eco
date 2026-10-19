package ciam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/mcp/server"
)

// Toolkit exposes a Manager as tools: usable by in-process agents and, through
// MCPServer, by editors. Tool names keep the original ciam_ prefix.
type Toolkit struct {
	M *Manager
	// Project is the default project root when a call omits project_path.
	Project string
}

type projectArg struct {
	ProjectPath string `json:"project_path,omitempty" description:"Project root (defaults to the configured project)"`
}

func (k Toolkit) root(p string) (string, error) {
	if p == "" {
		p = k.Project
	}
	if p == "" {
		return "", tools.Errorf("project_path is required (no default project configured)")
	}
	return p, nil
}

func (k Toolkit) project(p string) (string, error) {
	r, err := k.root(p)
	if err != nil {
		return "", err
	}
	return ProjectID(r), nil
}

type indexArgs struct {
	projectArg
	ProjectType string `json:"project_type,omitempty" description:"django, go, node, python or generic (auto-detected when empty)"`
	Force       bool   `json:"force,omitempty" description:"Re-embed every file"`
}

type searchArgs struct {
	projectArg
	Query     string `json:"query" description:"Natural language or code search query"`
	ChunkType string `json:"chunk_type,omitempty" description:"Filter: model, view, url, serializer, task, test, handler, config, doc, adr, prd, plan, research, generic"`
	Limit     int    `json:"limit,omitempty" description:"Max results (default 5)"`
	Compress  bool   `json:"compress,omitempty" description:"Strip comments and docstrings to save tokens"`
}

type queryArgs struct {
	projectArg
	Query string `json:"query" description:"What you are looking for"`
	Limit int    `json:"limit,omitempty" description:"Max results"`
}

type rememberArgs struct {
	Content string `json:"content" description:"What to remember, written to be understood out of context"`
	Type    string `json:"type,omitempty" description:"decision, note, bug, convention..."`
}

type recallArgs struct {
	Query string `json:"query" description:"Topic to recall"`
	Limit int    `json:"limit,omitempty"`
}

type compressArgs struct {
	Content   string `json:"content" description:"Source code to compress"`
	Extension string `json:"extension,omitempty" description:"File extension such as .py or .go (selects comment syntax)"`
}

type docArgs struct {
	projectArg
	Kind  string `json:"kind" jsonschema:"enum=adr|prd|plan|research,description=Document kind"`
	Title string `json:"title"`
	PRD   string `json:"prd,omitempty" description:"For plans: the PRD id this implements, e.g. PRD-001"`
}

type draftArgs struct {
	projectArg
	Intent    string `json:"intent" description:"What to implement"`
	PlanID    int    `json:"plan_id,omitempty" description:"Plan number to ground the draft in"`
	Phase     string `json:"phase,omitempty" description:"Plan phase, e.g. 'Phase 1'"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

type routeArgs struct {
	Intent string `json:"intent" description:"What you want to do, in plain language"`
}

func jsonText(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep "<feature>" readable
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return strings.TrimSpace(b.String())
}

func resultsText(res []Result, empty string) string {
	if len(res) == 0 {
		return empty
	}
	return jsonText(res)
}

// All returns every tool (ciam_draft only when the manager has a Model).
func (k Toolkit) All() []tools.Tool {
	m := k.M
	ts := []tools.Tool{
		tools.New("ciam_route", "CALL THIS FIRST when unsure which ciam tool to use: describe your intent and get the tool and parameters to use.",
			func(_ context.Context, a routeArgs) (any, error) { return Route(a.Intent), nil }),
		tools.New("ciam_index", "Index a project for semantic search (incremental: unchanged files are skipped). Required before any search.",
			func(ctx context.Context, a indexArgs) (any, error) {
				root, err := k.root(a.ProjectPath)
				if err != nil {
					return nil, err
				}
				r, err := m.Index(ctx, root, IndexOptions{Type: a.ProjectType, Force: a.Force})
				if err != nil {
					return nil, err
				}
				return fmt.Sprintf("Indexed %s (%s): %d files (%d changed, %d unchanged, %d removed), %d chunks, ~%d tokens",
					r.Project, r.Type, r.Files, r.Changed, r.Unchanged, r.Removed, r.Chunks, r.Tokens), nil
			}),
		tools.New("ciam_search", "[PRIORITY] Hybrid semantic + keyword search of the indexed project. Call this BEFORE reading files: it returns only the relevant chunks.",
			func(ctx context.Context, a searchArgs) (any, error) {
				p, err := k.project(a.ProjectPath)
				if err != nil {
					return nil, err
				}
				res, err := m.Search(ctx, SearchRequest{Project: p, Query: a.Query, Types: splitTypes(a.ChunkType), Limit: a.Limit, Compress: a.Compress})
				if err != nil {
					return nil, err
				}
				return resultsText(res, "No results. Is the project indexed (ciam_index)?"), nil
			}),
		tools.New("ciam_context", "[DEFAULT SEARCH TOOL] Search and compress in one call: the cheapest way to get code context.",
			func(ctx context.Context, a searchArgs) (any, error) {
				p, err := k.project(a.ProjectPath)
				if err != nil {
					return nil, err
				}
				res, err := m.Search(ctx, SearchRequest{Project: p, Query: a.Query, Types: splitTypes(a.ChunkType), Limit: a.Limit, Compress: true})
				if err != nil {
					return nil, err
				}
				return resultsText(res, "No results. Is the project indexed (ciam_index)?"), nil
			}),
		tools.New("ciam_decision_context", "[CALL BEFORE IMPLEMENTING ANYTHING] Relevant code + ADRs + PRDs + plans + research for a query, so past decisions and requirements are respected.",
			func(ctx context.Context, a queryArgs) (any, error) {
				p, err := k.project(a.ProjectPath)
				if err != nil {
					return nil, err
				}
				return m.DecisionContext(ctx, p, a.Query, a.Limit)
			}),
		tools.New("ciam_project_map", "Structural map of the project (Django apps, Go packages, code directories). Call it when onboarding to a project.",
			func(_ context.Context, a projectArg) (any, error) {
				r, err := k.root(a.ProjectPath)
				if err != nil {
					return nil, err
				}
				return ProjectMap(r)
			}),
		tools.New("ciam_remember", "Store a decision or note in persistent memory (survives sessions and projects).",
			func(ctx context.Context, a rememberArgs) (any, error) {
				id, err := m.Remember(ctx, a.Type, a.Content)
				if err != nil {
					return nil, err
				}
				return "remembered " + id, nil
			}),
		tools.New("ciam_recall", "Recall memories relevant to a topic.",
			func(ctx context.Context, a recallArgs) (any, error) {
				mem, err := m.Recall(ctx, a.Query, a.Limit)
				if err != nil {
					return nil, err
				}
				if len(mem) == 0 {
					return "No memories found.", nil
				}
				return jsonText(mem), nil
			}),
		tools.New("ciam_compress", "Compress source code to its skeleton (drops docstrings and comments).",
			func(_ context.Context, a compressArgs) (any, error) { return Compress(a.Content, a.Extension), nil }),
		tools.New("ciam_doc_new", "Create an ADR, PRD, plan or research document from its template under docs/. Write the reasoning BEFORE the code.",
			func(_ context.Context, a docArgs) (any, error) {
				r, err := k.root(a.ProjectPath)
				if err != nil {
					return nil, err
				}
				path, err := NewDocs(r).New(DocKind(a.Kind), a.Title, a.PRD)
				if err != nil {
					return nil, tools.Errorf("%v", err)
				}
				return "created " + path + " (fill it in, then re-run ciam_index so it becomes searchable)", nil
			}),
	}
	for _, kind := range []struct {
		name, doc string
		t         DocKind
	}{
		{"ciam_adr_search", "Search architecture decision records: why the system is the way it is.", ADR},
		{"ciam_prd_search", "Search product requirement documents: what a feature must do and its acceptance criteria.", PRD},
		{"ciam_plan_search", "Search implementation plans.", Plan},
		{"ciam_research_search", "Search research documents (docs/research).", Research},
	} {
		kind := kind
		ts = append(ts, tools.New(kind.name, kind.doc, func(ctx context.Context, a queryArgs) (any, error) {
			p, err := k.project(a.ProjectPath)
			if err != nil {
				return nil, err
			}
			res, err := m.Search(ctx, SearchRequest{Project: p, Query: a.Query, Types: []string{string(kind.t)}, Limit: a.Limit})
			if err != nil {
				return nil, err
			}
			return resultsText(res, fmt.Sprintf("No %s documents found. Create them with ciam_doc_new and re-index.", kind.t)), nil
		}))
	}
	if m.Model != nil {
		ts = append(ts, tools.New("ciam_draft", "Optional: draft code with the configured (usually local) model, grounded in project context, ADRs and a plan phase. The draft is speculative: validate it.",
			func(ctx context.Context, a draftArgs) (any, error) {
				r, err := k.root(a.ProjectPath)
				if err != nil {
					return nil, err
				}
				return m.Draft(ctx, DraftRequest{Root: r, Intent: a.Intent, PlanID: a.PlanID, Phase: a.Phase, MaxTokens: a.MaxTokens})
			}))
	}
	return ts
}

func splitTypes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// RouteAdvice is the answer of Route.
type RouteAdvice struct {
	Tool   string `json:"tool"`
	Params string `json:"params"`
	Reason string `json:"reason"`
	Intent string `json:"intent"`
}

var routes = []struct {
	keywords     []string
	tool, params string
	reason       string
}{
	{[]string{"implement", "add feature", "create feature", "build", "develop", "implementar"}, "ciam_decision_context", `{"query": "<feature>"}`, "Code + ADRs + PRDs + plans before writing anything"},
	{[]string{"bug", "fix", "error", "broken", "crash", "exception", "traceback"}, "ciam_recall + ciam_adr_search", `{"query": "<bug topic>"}`, "Check memory and decisions related to the failing area first"},
	{[]string{"understand", "how does", "what is", "explain", "overview"}, "ciam_context", `{"query": "<topic>"}`, "Search + compress: cheapest way to understand code"},
	{[]string{"structure", "map", "onboard", "layout"}, "ciam_project_map", `{}`, "Structural map of the project"},
	{[]string{"architecture", "decision", "adr", "why was", "why is"}, "ciam_adr_search", `{"query": "<topic>"}`, "History of architecture decisions"},
	{[]string{"requirement", "prd", "acceptance", "spec"}, "ciam_prd_search", `{"query": "<feature>"}`, "Product requirements and acceptance criteria"},
	{[]string{"plan", "phase", "roadmap", "milestone"}, "ciam_plan_search", `{"query": "<feature>"}`, "Implementation plans"},
	{[]string{"research", "benchmark", "comparison"}, "ciam_research_search", `{"query": "<topic>"}`, "Research documents"},
	{[]string{"draft", "speculative"}, "ciam_draft", `{"intent": "<what>", "plan_id": 1}`, "Only when the user explicitly asks for a local draft"},
	{[]string{"remember", "save decision", "note"}, "ciam_remember", `{"content": "<what>", "type": "decision"}`, "Persist a decision or note"},
	{[]string{"recall", "past decision", "previous", "history"}, "ciam_recall", `{"query": "<topic>"}`, "Retrieve stored memory"},
	{[]string{"model", "schema", "field", "database"}, "ciam_search", `{"query": "<name>", "chunk_type": "model"}`, "Filtered search of data models"},
	{[]string{"endpoint", "view", "handler", "api"}, "ciam_search", `{"query": "<intent>", "chunk_type": "view,handler"}`, "Filtered search of endpoints"},
}

// Route maps a plain-language intent to the right tool.
func Route(intent string) string {
	lower := strings.ToLower(intent)
	for _, r := range routes {
		for _, kw := range r.keywords {
			if strings.Contains(lower, kw) {
				return jsonText(RouteAdvice{r.tool, r.params, r.reason, intent})
			}
		}
	}
	return jsonText(RouteAdvice{"ciam_decision_context", fmt.Sprintf(`{"query": %q}`, intent), "Default: full decision context is the safest start", intent})
}

// Instructions is the guidance sent to MCP clients and usable as an agent
// system-prompt fragment.
const Instructions = `ciam gives you cheap, precise context about this project.
- Call ciam_decision_context BEFORE implementing anything non-trivial: it returns the code plus the ADRs, PRDs and plans that explain it.
- Call ciam_search / ciam_context BEFORE reading files; read a whole file only when the chunks are not enough.
- Record why you decided something with ciam_remember, and significant decisions as an ADR (ciam_doc_new), then re-run ciam_index.
- Unsure which tool? Call ciam_route.`

// MCPServer builds an MCP server exposing the toolkit and the ciam prompts.
// Tool-call counts and latency are reported to onCall when non-nil.
func (k Toolkit) MCPServer(onCall server.CallHook) *server.Server {
	s := server.New("ciam", "0.1.0")
	s.Instructions = Instructions
	s.OnCall = onCall
	s.AddTool(k.All()...)
	feature := func(_ context.Context, a map[string]string) ([]server.PromptMessage, error) {
		return []server.PromptMessage{{Role: "user", Text: fmt.Sprintf(
			"Implement: %s\n\n1. Call ciam_decision_context with the feature description and read the ADRs/PRDs/plans it returns.\n2. Call ciam_recall for related past decisions.\n3. Make a short plan that respects them; stop and ask if it conflicts with an ADR.\n4. Implement, then record the important decisions with ciam_remember (or an ADR via ciam_doc_new).", a["feature"])}}, nil
	}
	s.AddPrompt(server.Prompt{Name: "implement-feature", Description: "Guided workflow to implement a feature respecting past decisions",
		Arguments: []server.PromptArg{{Name: "feature", Description: "The feature to implement", Required: true}}, Get: feature})
	s.AddPrompt(server.Prompt{Name: "investigate-bug", Description: "Guided workflow to investigate a bug with project memory",
		Arguments: []server.PromptArg{{Name: "bug", Description: "Symptoms or error", Required: true}},
		Get: func(_ context.Context, a map[string]string) ([]server.PromptMessage, error) {
			return []server.PromptMessage{{Role: "user", Text: fmt.Sprintf(
				"Investigate: %s\n\n1. ciam_recall and ciam_adr_search for the affected area.\n2. ciam_context to find the code; read files only if needed.\n3. Document the root cause as an ADR before fixing (ciam_doc_new, kind adr).", a["bug"])}}, nil
		}})
	return s
}

// ----------------------------------------------------------------- Draft

// DraftRequest asks for a speculative code draft.
type DraftRequest struct {
	Root      string
	Intent    string
	PlanID    int
	Phase     string
	MaxTokens int
}

// Draft asks the manager's Model (typically a small local model, to save paid
// tokens) for a first draft grounded in the project's decision context and an
// optional plan phase. It is deliberately speculative: the caller must review it.
func (m *Manager) Draft(ctx context.Context, r DraftRequest) (string, error) {
	if m.Model == nil {
		return "", tools.Errorf("draft is disabled: configure Manager.Model")
	}
	project := ProjectID(r.Root)
	dc, err := m.DecisionContext(ctx, project, r.Intent, 3)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("You are drafting code for the project below. Follow the architecture decisions and requirements shown; do not invent APIs that are not in the context. Output only code with short comments.\n\n")
	if r.PlanID > 0 {
		ex, err := NewDocs(r.Root).PlanExcerpt(r.PlanID, r.Phase)
		if err != nil {
			return "", tools.Errorf("%v", err)
		}
		b.WriteString("<plan>\n" + ex + "\n</plan>\n\n")
	}
	b.WriteString("<context>\n" + dc + "\n</context>\n\nTask: " + r.Intent)
	out, err := m.generate(ctx, b.String(), r.MaxTokens)
	if err != nil {
		return "", err
	}
	return "# Draft (speculative — validate against the plan and ADRs)\n\n" + out, nil
}
