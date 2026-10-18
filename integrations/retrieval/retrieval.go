// Package retrieval adds retrievers that need no vector database: BM25
// keyword search, and reranking of any retriever's results.
package retrieval

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
)

// Tokenize lowercases and splits on non-letter/digit runes.
func Tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// BM25 is an Okapi BM25 retriever over a fixed corpus (BM25Retriever).
type BM25 struct {
	docs []documents.Document
	tf   []map[string]int
	dl   []float64
	df   map[string]int
	avg  float64
	K1   float64 // default 1.5
	B    float64 // default 0.75
	K    int     // results per query, default 4
	// Preprocess overrides Tokenize (e.g. stemming, stop words).
	Preprocess func(string) []string
}

// NewBM25 indexes docs.
func NewBM25(docs []documents.Document) *BM25 {
	b := &BM25{docs: docs, df: map[string]int{}, K1: 1.5, B: 0.75, K: 4}
	b.index()
	return b
}

func (b *BM25) tok(s string) []string {
	if b.Preprocess != nil {
		return b.Preprocess(s)
	}
	return Tokenize(s)
}

func (b *BM25) index() {
	b.tf, b.dl = make([]map[string]int, len(b.docs)), make([]float64, len(b.docs))
	total := 0.0
	for i, d := range b.docs {
		m := map[string]int{}
		for _, t := range b.tok(d.PageContent) {
			m[t]++
		}
		b.tf[i] = m
		for t, n := range m {
			b.df[t]++
			b.dl[i] += float64(n)
		}
		total += b.dl[i]
	}
	if len(b.docs) > 0 {
		b.avg = total / float64(len(b.docs))
	}
}

// Scores returns the BM25 score of every document for query.
func (b *BM25) Scores(query string) []float64 {
	n := float64(len(b.docs))
	sc := make([]float64, len(b.docs))
	for _, t := range b.tok(query) {
		df := float64(b.df[t])
		if df == 0 {
			continue
		}
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		for i := range b.docs {
			f := float64(b.tf[i][t])
			if f == 0 {
				continue
			}
			sc[i] += idf * f * (b.K1 + 1) / (f + b.K1*(1-b.B+b.B*b.dl[i]/b.avg))
		}
	}
	return sc
}

// Invoke implements retrievers.Retriever; only documents that match at least
// one query term are returned.
func (b *BM25) Invoke(_ context.Context, query string) ([]documents.Document, error) {
	sc := b.Scores(query)
	idx := make([]int, 0, len(sc))
	for i, s := range sc {
		if s > 0 {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(x, y int) bool { return sc[idx[x]] > sc[idx[y]] })
	k := b.K
	if k <= 0 {
		k = 4
	}
	if len(idx) > k {
		idx = idx[:k]
	}
	out := make([]documents.Document, len(idx))
	for i, j := range idx {
		d := b.docs[j]
		md := map[string]any{"bm25_score": sc[j]}
		for k, v := range d.Metadata {
			md[k] = v
		}
		d.Metadata = md
		out[i] = d
	}
	return out, nil
}

// Reranker reorders documents for a query (cohere.Reranker satisfies it, as
// does chatmodels.NewReranker's result).
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []documents.Document) ([]documents.Document, error)
}

// WithRerank fetches candidates from base and returns them reranked
// (ContextualCompressionRetriever + reranker).
func WithRerank(base retrievers.Retriever, rr Reranker) retrievers.Retriever {
	return retrievers.Func(func(ctx context.Context, q string) ([]documents.Document, error) {
		docs, err := base.Invoke(ctx, q)
		if err != nil || len(docs) == 0 {
			return docs, err
		}
		return rr.Rerank(ctx, q, docs)
	})
}
