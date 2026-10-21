package team

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/cag"
	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/kb"
	"github.com/fsvxavier/golang-chain-eco/kag"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
	"github.com/fsvxavier/golang-chain-eco/mcp/server"
	"github.com/fsvxavier/golang-chain-eco/rag"
)

// triples is the architecture knowledge graph (KAG): what depends on what and
// which rule guards what. Multi-hop questions ("what must hold for the coupon
// when a payment races?") walk these edges.
var triples = [][3]string{
	{"Checkout", "USES", "Payment"},
	{"Payment", "CONSUMES", "Coupon"},
	{"Payment", "REQUIRES", "Idempotency Key"},
	{"Coupon", "GUARDED_BY", "Atomic Reservation"},
	{"Coupon", "GUARDED_BY", "Rate Limit"},
	{"Atomic Reservation", "EXPIRES_WITH", "TTL"},
	{"Rate Limit", "PREVENTS", "Code Enumeration"},
	{"Payment Failure", "TRIGGERS", "Coupon Release"},
	{"Coupon Release", "RESTORES", "Coupon"},
}

func entityNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range triples {
		for _, n := range []string{t[0], t[2]} {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

func loadDocs(names ...string) ([]documents.Document, error) {
	if len(names) == 0 {
		entries, err := fs.Glob(kb.FS, "*.md")
		if err != nil {
			return nil, err
		}
		names = entries
	}
	var docs []documents.Document
	for _, n := range names {
		b, err := kb.FS.ReadFile(n)
		if err != nil {
			return nil, err
		}
		docs = append(docs, documents.Document{PageContent: string(b), Metadata: map[string]any{"source": n}})
	}
	return docs, nil
}

// NewKnowledge builds the three techniques over the kb/ folder:
//   - RAG: every document, chunked and searched by vector + BM25;
//   - CAG: the two checklists, small and stable, kept whole in the context;
//   - KAG: the architecture graph, with RAG as fallback when a hop finds nothing.
func NewKnowledge(ctx context.Context, chat models.ChatModel, emb embeddings.Embedder) (knowledge.Toolkit, error) {
	docs, err := loadDocs()
	if err != nil {
		return knowledge.Toolkit{}, err
	}
	vs := vectorstores.NewMemory(emb)
	ix := rag.NewIndexer(vs, nil)
	if _, err := ix.Index(ctx, documents.LoaderFunc(func(context.Context) ([]documents.Document, error) { return docs, nil })); err != nil {
		return knowledge.Toolkit{}, err
	}
	pipe := &rag.Pipeline{Store: vs, Docs: ix.Documents(), Model: chat, K: 3, Hybrid: true}

	checklists, err := loadDocs("review-checklist.md", "qa-checklist.md")
	if err != nil {
		return knowledge.Toolkit{}, err
	}
	corpus := cag.NewCorpus(checklists)
	cagEngine := &cag.Engine{Model: chat, Corpus: corpus, Window: 200000}

	g := kag.NewMemory()
	var ents []kag.Entity
	var rels []kag.Relation
	for _, n := range entityNames() {
		ents = append(ents, kag.Entity{ID: kag.NormalizeID(n), Name: n, Type: "CONCEPT"})
	}
	for _, t := range triples {
		rels = append(rels, kag.Relation{From: kag.NormalizeID(t[0]), Type: t[1], To: kag.NormalizeID(t[2]), Source: "architecture.graph"})
	}
	if err := g.UpsertEntities(ctx, ents); err != nil {
		return knowledge.Toolkit{}, err
	}
	if err := g.UpsertRelations(ctx, rels); err != nil {
		return knowledge.Toolkit{}, err
	}
	kagEngine := &kag.Engine{Model: chat, Graph: g, Chunks: pipe}

	router := &knowledge.Router{
		RAG: pipe, CAG: cagEngine, KAG: kagEngine, Fallback: true,
		Profile: knowledge.Profile{CorpusTokens: corpus.Tokens(knowledge.Heuristic{}), ContextWindow: 200000, HasGraph: true},
	}
	return knowledge.Toolkit{RAG: pipe, CAG: cagEngine, KAG: kagEngine, Router: router}, nil
}

type memPut struct {
	Scope string `json:"scope" description:"Request id the memory belongs to"`
	Key   string `json:"key" description:"Unique key, e.g. the job id"`
	Text  string `json:"text" description:"What to remember: a decision, a constraint, a verdict"`
}

type memSearch struct {
	Scope string `json:"scope" description:"Request id to search in"`
	Query string `json:"query" description:"What you want to recall"`
	Limit int    `json:"limit,omitempty" description:"Maximum results (default 5)"`
}

// MemoryTools are the team memory: decisions recorded per request, ranked by
// similarity when recalled. They live in the hub, so every service sees them.
func MemoryTools(emb embeddings.Embedder) []tools.Tool {
	st := langmem.NewIndexedStore(store.NewMemory(), emb)
	st.Text = func(v map[string]any) string { return fmt.Sprint(v["text"]) }
	ns := func(scope string) []string { return []string{"team", scope} }
	return []tools.Tool{
		tools.New("memory_put", "Record a decision, constraint or verdict so the next agents on this request can recall it.",
			func(ctx context.Context, a memPut) (any, error) {
				if a.Scope == "" || a.Key == "" {
					return nil, tools.Errorf("scope and key are required")
				}
				if err := st.Put(ctx, ns(a.Scope), a.Key, map[string]any{"text": a.Text}); err != nil {
					return nil, tools.Errorf("memory_put: %v", err)
				}
				return "stored", nil
			}),
		tools.New("memory_search", "Recall what the team decided for this request, most relevant first.",
			func(ctx context.Context, a memSearch) (any, error) {
				if a.Limit <= 0 {
					a.Limit = 5
				}
				hits, err := st.SemanticSearch(ctx, ns(a.Scope), a.Query, nil, a.Limit)
				if err != nil {
					return nil, tools.Errorf("memory_search: %v", err)
				}
				var b strings.Builder
				for _, h := range hits {
					fmt.Fprintf(&b, "- [%s] %v\n", h.Key, h.Value["text"])
				}
				if b.Len() == 0 {
					return "(nothing recorded yet)", nil
				}
				return b.String(), nil
			}),
	}
}

// NewMCP serves knowledge (RAG, CAG, KAG, router) and team memory as one MCP
// server. It is the only channel agents use to share context.
func NewMCP(ctx context.Context, chat models.ChatModel, emb embeddings.Embedder) (*server.Server, error) {
	tk, err := NewKnowledge(ctx, chat, emb)
	if err != nil {
		return nil, err
	}
	s := tk.MCPServer(nil)
	s.AddTool(MemoryTools(emb)...)
	return s, nil
}

// offlineKnowledgeModel stands in for the knowledge model without a key: it
// answers with the top numbered source and plans KAG walks by naming the
// first graph entity the question mentions.
func offlineKnowledgeModel() models.ChatModel {
	return &models.Fake{Name: "offline:echo", Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		if len(o.Tools) > 0 { // kag plan, forced as a tool call
			q := strings.ToLower(msgs[len(msgs)-1].Text())
			entity := "Coupon"
			for _, n := range entityNames() {
				if strings.Contains(q, strings.ToLower(n)) {
					entity = n
					break
				}
			}
			return messages.AI("", messages.ToolCall{ID: "plan", Name: o.Tools[0].Name, Args: map[string]any{
				"steps": []any{map[string]any{"id": "1", "goal": q, "entity": entity, "dir": "both"}},
			}}), nil
		}
		var top []string // the whole first numbered source, up to the second
		for _, line := range strings.Split(msgs[0].Text(), "\n") {
			if rest, ok := strings.CutPrefix(line, "[1] "); ok {
				top = append(top, rest)
			} else if strings.HasPrefix(line, "[2] ") {
				break
			} else if len(top) > 0 {
				top = append(top, line)
			}
		}
		if len(top) > 0 {
			return messages.AI(strings.TrimSpace(strings.Join(top, "\n")) + " [1]"), nil
		}
		return messages.AI("No matching context was found."), nil
	}}
}
