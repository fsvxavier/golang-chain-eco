package retrieval_test

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/integrations/retrieval"
)

var corpus = []documents.Document{
	{PageContent: "Go goroutines and channels make concurrency simple"},
	{PageContent: "Python is great for data science"},
	{PageContent: "Concurrency in Go: goroutines goroutines goroutines", Metadata: map[string]any{"id": 3}},
	{PageContent: "Rust ownership and borrowing"},
}

func TestBM25(t *testing.T) {
	b := retrieval.NewBM25(corpus)
	docs, err := b.Invoke(context.Background(), "goroutines concurrency")
	if err != nil || len(docs) != 2 {
		t.Fatalf("%+v %v", docs, err)
	}
	if docs[0].Metadata["id"] != 3 || docs[0].Metadata["bm25_score"].(float64) <= docs[1].Metadata["bm25_score"].(float64) {
		t.Fatalf("ranking: %+v", docs)
	}
	if none, _ := b.Invoke(context.Background(), "haskell"); len(none) != 0 {
		t.Fatal("no match must return nothing")
	}
	b.K = 1
	if one, _ := b.Invoke(context.Background(), "go"); len(one) != 1 {
		t.Fatal("K must cap results")
	}
}

type reverse struct{}

func (reverse) Rerank(_ context.Context, _ string, d []documents.Document) ([]documents.Document, error) {
	out := make([]documents.Document, len(d))
	for i := range d {
		out[len(d)-1-i] = d[i]
	}
	return out, nil
}

func TestWithRerank(t *testing.T) {
	base := retrievers.Func(func(context.Context, string) ([]documents.Document, error) { return corpus[:2], nil })
	got, _ := retrieval.WithRerank(base, reverse{}).Invoke(context.Background(), "q")
	if got[0].PageContent != corpus[1].PageContent {
		t.Fatal(got)
	}
}
