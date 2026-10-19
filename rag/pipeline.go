package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/integrations/retrieval"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
)

// Pipeline answers questions over an indexed corpus: vector search, optionally
// fused with BM25 (RRF), optional MultiQuery, rerank, token budget and a
// prompt that asks the model to cite [n]. It implements knowledge.Answerer.
type Pipeline struct {
	Store vectorstores.VectorStore
	// Docs is the corpus for the BM25 side, normally Indexer.Documents().
	Docs         []documents.Document
	Model        models.ChatModel
	K            int // results returned, default 4
	FetchK       int // candidates fetched before rerank/truncation, default K
	Hybrid       bool
	MultiQueries int // extra rephrasings of the question; 0 disables
	Reranker     retrieval.Reranker
	// System overrides knowledge.DefaultSystem.
	System string
	// MaxContextTokens drops sources from the end until the context fits. 0 means no limit.
	MaxContextTokens int
	// Counter measures sources for the budget. Nil means knowledge.Heuristic{}.
	Counter knowledge.TokenCounter
}

var _ knowledge.Answerer = (*Pipeline)(nil)

// Answer retrieves sources and asks the model for a cited answer. With no
// sources it returns an empty answer without calling the model.
func (p *Pipeline) Answer(ctx context.Context, q knowledge.Question) (knowledge.Answer, error) {
	if strings.TrimSpace(q.Text) == "" {
		return knowledge.Answer{}, knowledge.ErrEmptyQuestion
	}
	if p.Model == nil {
		return knowledge.Answer{}, errors.New("rag: no model")
	}
	srcs, err := p.Retrieve(ctx, q)
	if err != nil {
		return knowledge.Answer{}, err
	}
	trace := []knowledge.Step{{Name: "retrieve", Detail: fmt.Sprintf("%d sources", len(srcs))}}
	if len(srcs) == 0 {
		trace = append(trace, knowledge.Step{Name: "generate", Detail: "skipped: no sources"})
		return knowledge.Answer{Strategy: "rag", Trace: trace}, nil
	}

	system := p.System
	if system == "" {
		system = knowledge.DefaultSystem
	}
	resp, err := p.Model.Generate(ctx, knowledge.Prompt(system, srcs, q))
	if err != nil {
		return knowledge.Answer{}, err
	}
	trace = append(trace, knowledge.Step{Name: "generate", Detail: p.Model.ModelID()})
	return knowledge.Answer{
		Text:     resp.Text(),
		Sources:  srcs,
		Strategy: "rag",
		Usage:    resp.Usage,
		Trace:    trace,
	}, nil
}

// Retrieve returns the sources for q after fusion, rerank, K truncation and the token budget.
func (p *Pipeline) Retrieve(ctx context.Context, q knowledge.Question) ([]knowledge.Source, error) {
	if strings.TrimSpace(q.Text) == "" {
		return nil, knowledge.ErrEmptyQuestion
	}
	if q.Force != "" && q.Force != "rag" {
		return nil, fmt.Errorf("rag: cannot answer with strategy %q", q.Force)
	}
	if p.Store == nil {
		return nil, errors.New("rag: no vector store")
	}
	if p.MultiQueries > 0 && p.Model == nil {
		return nil, errors.New("rag: MultiQueries needs a model")
	}
	k := p.K
	if k <= 0 {
		k = 4
	}
	fetch := max(p.FetchK, k)

	var r retrievers.Retriever = retrievers.FromVectorStore(p.Store, retrievers.VectorStoreOptions{K: fetch, Filter: q.Filter})
	if p.Hybrid {
		r = retrievers.Ensemble([]retrievers.Retriever{r, p.keyword(q.Filter, k)}, nil)
	}
	if p.MultiQueries > 0 {
		r = retrievers.MultiQuery(r, p.Model, p.MultiQueries)
	}
	if p.Reranker != nil {
		r = retrieval.WithRerank(r, p.Reranker)
	}

	docs, err := r.Invoke(ctx, q.Text)
	if err != nil {
		return nil, err
	}
	if len(docs) > k {
		docs = docs[:k]
	}
	return p.fitBudget(knowledge.SourcesFromDocs(docs)), nil
}

// keyword is the BM25 side. The filter is applied after scoring, so BM25
// scores the whole corpus and the first k eligible hits are kept.
// ponytail: BM25 index is rebuilt per question; cache it once the corpus is stable.
func (p *Pipeline) keyword(f vectorstores.Filter, k int) retrievers.Retriever {
	bm := retrieval.NewBM25(p.Docs)
	bm.K = max(len(p.Docs), 1)
	return retrievers.Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		docs, err := bm.Invoke(ctx, q)
		if err != nil {
			return nil, err
		}
		out := docs[:0]
		for _, d := range docs {
			if f != nil && !f(d) {
				continue
			}
			out = append(out, d)
			if len(out) == k {
				break
			}
		}
		return out, nil
	})
}

// fitBudget keeps the longest prefix of srcs whose contents fit MaxContextTokens.
func (p *Pipeline) fitBudget(srcs []knowledge.Source) []knowledge.Source {
	if p.MaxContextTokens <= 0 {
		return srcs
	}
	c := p.Counter
	if c == nil {
		c = knowledge.Heuristic{}
	}
	total := 0
	for i, s := range srcs {
		total += c.Count(s.Content)
		if total > p.MaxContextTokens {
			return srcs[:i]
		}
	}
	return srcs
}
