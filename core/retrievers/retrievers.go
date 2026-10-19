// Package retrievers implements document retrievers (langchain_core.retrievers)
// and the retriever tool used by agentic RAG.
package retrievers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/callbacks"
	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
)

// Retriever returns documents relevant to a query. It is a
// runnables.Runnable[string, []documents.Document].
type Retriever interface {
	Invoke(ctx context.Context, query string) ([]documents.Document, error)
}

// Func adapts a function to Retriever.
type Func func(ctx context.Context, query string) ([]documents.Document, error)

// Invoke implements Retriever.
func (f Func) Invoke(ctx context.Context, q string) ([]documents.Document, error) { return f(ctx, q) }

// SearchType selects how a vector store retriever searches.
type SearchType string

const (
	Similarity          SearchType = "similarity"
	MMR                 SearchType = "mmr"
	SimilarityThreshold SearchType = "similarity_score_threshold"
)

// VectorStoreOptions configures FromVectorStore (search_kwargs).
type VectorStoreOptions struct {
	Type           SearchType
	K              int     // default 4
	FetchK         int     // MMR candidate pool, default 20
	Lambda         float64 // MMR diversity, default 0.5
	ScoreThreshold float64
	Filter         vectorstores.Filter
}

// FromVectorStore wraps a vector store (VectorStore.as_retriever).
func FromVectorStore(vs vectorstores.VectorStore, o VectorStoreOptions) Retriever {
	if o.K <= 0 {
		o.K = 4
	}
	if o.Lambda == 0 {
		o.Lambda = 0.5
	}
	return Func(func(ctx context.Context, q string) (docs []documents.Document, err error) {
		ctx, end := callbacks.Start(ctx, callbacks.RetrieverStart, "VectorStoreRetriever", q)
		defer func() { end(docs, err) }()
		switch o.Type {
		case MMR:
			return vs.MaxMarginalRelevance(ctx, q, o.K, o.FetchK, o.Lambda, o.Filter)
		case SimilarityThreshold:
			scored, err := vs.SimilaritySearchWithScore(ctx, q, o.K, o.Filter)
			if err != nil {
				return nil, err
			}
			for _, s := range scored {
				if s.Score >= o.ScoreThreshold {
					docs = append(docs, s.Document)
				}
			}
			return docs, nil
		}
		return vectorstores.SimilaritySearch(ctx, vs, q, o.K, o.Filter)
	})
}

// Ensemble merges several retrievers with weighted Reciprocal Rank Fusion
// (EnsembleRetriever). Weights default to equal.
func Ensemble(rs []Retriever, weights []float64) Retriever {
	const c = 60.0
	return Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		scores := map[string]float64{}
		docs := map[string]documents.Document{}
		var keys []string // first-seen order, so ties rank deterministically
		for i, r := range rs {
			w := 1.0
			if i < len(weights) {
				w = weights[i]
			}
			res, err := r.Invoke(ctx, q)
			if err != nil {
				return nil, err
			}
			for rank, d := range res {
				key := d.ID
				if key == "" {
					key = d.PageContent
				}
				if _, seen := scores[key]; !seen {
					keys = append(keys, key)
				}
				scores[key] += w / (float64(rank+1) + c)
				docs[key] = d
			}
		}
		sort.SliceStable(keys, func(a, b int) bool { return scores[keys[a]] > scores[keys[b]] })
		out := make([]documents.Document, len(keys))
		for i, k := range keys {
			out[i] = docs[k]
		}
		return out, nil
	})
}

// MultiQuery asks the model for n rephrasings of the query, retrieves for
// each and returns the de-duplicated union (MultiQueryRetriever).
func MultiQuery(r Retriever, m models.ChatModel, n int) Retriever {
	return Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		prompt := fmt.Sprintf("You are an AI language model assistant. Generate %d different versions of the given user question to retrieve relevant documents from a vector database. Provide these alternative questions separated by newlines, with no numbering.\nOriginal question: %s", n, q)
		resp, err := m.Generate(ctx, []messages.Message{messages.Human(prompt)})
		if err != nil {
			return nil, err
		}
		queries := []string{q}
		for _, l := range strings.Split(resp.Text(), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				queries = append(queries, l)
			}
		}
		seen := map[string]bool{}
		var out []documents.Document
		for _, qq := range queries {
			docs, err := r.Invoke(ctx, qq)
			if err != nil {
				return nil, err
			}
			for _, d := range docs {
				if !seen[d.PageContent] {
					seen[d.PageContent] = true
					out = append(out, d)
				}
			}
		}
		return out, nil
	})
}

// FormatDocuments joins document contents with blank lines.
func FormatDocuments(docs []documents.Document) string {
	parts := make([]string, len(docs))
	for i, d := range docs {
		parts[i] = d.PageContent
	}
	return strings.Join(parts, "\n\n")
}

// AsTool exposes a retriever as a tool (create_retriever_tool).
func AsTool(r Retriever, name, description string) tools.Tool {
	type args struct {
		Query string `json:"query" description:"query to look up in retriever"`
	}
	return tools.New(name, description, func(ctx context.Context, a args) (any, error) {
		docs, err := r.Invoke(ctx, a.Query)
		if err != nil {
			return nil, err
		}
		return tools.ContentAndArtifact{Content: FormatDocuments(docs), Artifact: docs}, nil
	})
}
