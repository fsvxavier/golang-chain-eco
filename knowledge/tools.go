package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/mcp/server"
)

// Toolkit exposes the answerers as tools for agents (in-process) and, through
// MCPServer, for editors. A tool exists only when its Answerer is set.
type Toolkit struct {
	RAG, CAG, KAG Answerer
	// Router backs knowledge_answer, which picks the technique per question.
	Router *Router
	// Ingest adds documents to the knowledge base; knowledge_ingest needs it.
	Ingest func(ctx context.Context, docs []documents.Document) error
}

type questionArgs struct {
	Question string `json:"question" description:"The question to answer from the knowledge base"`
	Force    string `json:"force,omitempty" description:"Optional strategy: rag, cag or kag (router decides when empty)"`
}

type ingestItem struct {
	ID       string         `json:"id,omitempty"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ingestArgs struct {
	// []any, not []string: tool schemas validate lists as []any.
	Documents []any `json:"documents" description:"Documents to add: objects with id (optional), content and metadata (optional)"`
}

// All returns the tools for every configured answerer.
func (k Toolkit) All() []tools.Tool {
	var ts []tools.Tool
	if k.RAG != nil {
		ts = append(ts, answerTool("rag_answer", "Answer a question with retrieval-augmented generation over the vector index. Returns the answer with numbered sources.", k.RAG))
	}
	if k.CAG != nil {
		ts = append(ts, answerTool("cag_answer", "Answer a question from a small, stable corpus held in the model context (cache-augmented generation). Returns the answer with numbered sources.", k.CAG))
	}
	if k.KAG != nil {
		ts = append(ts, answerTool("kag_query", "Answer a multi-hop question by walking the knowledge graph (relations between entities). Returns the answer with numbered sources.", k.KAG))
	}
	if k.Router != nil {
		ts = append(ts, answerTool("knowledge_answer", "[PRIORITY] Answer a question from the knowledge base; the router picks RAG, CAG or KAG. Use this when unsure which tool fits.", k.Router))
	}
	if k.Ingest != nil {
		ts = append(ts, tools.New("knowledge_ingest", "Add documents to the knowledge base so later questions can use them.",
			func(ctx context.Context, a ingestArgs) (any, error) {
				var items []ingestItem
				raw, err := json.Marshal(a.Documents)
				if err == nil {
					err = json.Unmarshal(raw, &items)
				}
				if err != nil {
					return nil, tools.Errorf("documents must be objects with content: %v", err)
				}
				docs := make([]documents.Document, len(items))
				for i, it := range items {
					docs[i] = documents.Document{ID: it.ID, PageContent: it.Content, Metadata: it.Metadata}
				}
				if err := k.Ingest(ctx, docs); err != nil {
					return nil, tools.Errorf("ingest: %v", err)
				}
				return fmt.Sprintf("ingested %d documents", len(docs)), nil
			}))
	}
	return ts
}

// answerTool wraps an Answerer. The model sees the text and sources; the
// artifact keeps the full Answer (trace, usage) for callers that want it.
func answerTool(name, desc string, a Answerer) tools.Tool {
	return tools.New(name, desc, func(ctx context.Context, args questionArgs) (any, error) {
		ans, err := a.Answer(ctx, Question{Text: args.Question, Force: args.Force})
		if err != nil {
			return nil, tools.Errorf("%s: %v", name, err)
		}
		return tools.ContentAndArtifact{Content: withSources(ans), Artifact: ans}, nil
	})
}

// withSources appends the numbered sources, matching the [n] markers the
// answerers put in their text.
func withSources(ans Answer) string {
	if len(ans.Sources) == 0 {
		return ans.Text
	}
	var b strings.Builder
	b.WriteString(ans.Text)
	b.WriteString("\n\nSources:")
	for i, s := range ans.Sources {
		fmt.Fprintf(&b, "\n[%d] %s", i+1, s.ID)
	}
	return b.String()
}

// MCPServer builds an MCP server named "knowledge" that exposes All. onCall
// observes every tool call when non-nil.
func (k Toolkit) MCPServer(onCall server.CallHook) *server.Server {
	s := server.New("knowledge", "0.1.0")
	s.Instructions = "Answer questions with knowledge_answer (routes automatically) or with rag_answer, cag_answer or kag_query when you know the technique. Cite the numbered sources."
	s.OnCall = onCall
	s.AddTool(k.All()...)
	return s
}
