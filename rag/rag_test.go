package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
)

// loader builds a loader from (source, content) pairs.
func loader(pairs ...string) documents.Loader {
	return documents.LoaderFunc(func(context.Context) ([]documents.Document, error) {
		var out []documents.Document
		for i := 0; i+1 < len(pairs); i += 2 {
			out = append(out, documents.Document{
				PageContent: pairs[i+1],
				Metadata:    map[string]any{"source": pairs[i]},
			})
		}
		return out, nil
	})
}

func newStore() vectorstores.VectorStore {
	return vectorstores.NewMemory(embeddings.Fake{Dim: 128})
}

func answerModel(usage *messages.Usage) *models.Fake {
	return &models.Fake{Func: func([]messages.Message, models.Options) (messages.Message, error) {
		m := messages.AI("ok [1]")
		m.Usage = usage
		return m, nil
	}}
}

func TestIndexIncremental(t *testing.T) {
	ctx := context.Background()
	ix := NewIndexer(newStore(), nil)

	st, err := ix.Index(ctx, loader("a.txt", "Go is a compiled language.", "b.txt", "Channels pass messages."))
	if err != nil || st.Added != 2 {
		t.Fatalf("first index: stats=%+v err=%v, want Added=2", st, err)
	}

	st, err = ix.Index(ctx, loader("a.txt", "Go is a compiled language.", "b.txt", "Channels pass messages."))
	if err != nil || st.Added != 0 || st.Skipped != 2 || st.Removed != 0 {
		t.Fatalf("unchanged sources: stats=%+v err=%v, want Skipped=2 only", st, err)
	}

	st, err = ix.Index(ctx, loader("a.txt", "Go is a compiled language with goroutines.", "b.txt", "Channels pass messages."))
	if err != nil || st.Removed != 1 || st.Added != 1 || st.Skipped != 1 {
		t.Fatalf("changed source: stats=%+v err=%v, want Removed=1 Added=1 Skipped=1", st, err)
	}
	if n := len(ix.Documents()); n != 2 {
		t.Fatalf("Documents() = %d chunks, want 2", n)
	}
}

func TestAnswerReturnsSources(t *testing.T) {
	ctx := context.Background()
	ix := NewIndexer(newStore(), nil)
	if _, err := ix.Index(ctx, loader(
		"a.txt", "Go is a compiled language.",
		"b.txt", "Goroutines are lightweight threads.",
		"c.txt", "Channels let goroutines talk.",
	)); err != nil {
		t.Fatal(err)
	}
	p := &Pipeline{
		Store: ix.store, Docs: ix.Documents(),
		Model: answerModel(&messages.Usage{InputTokens: 7}), K: 2,
	}

	ans, err := p.Answer(ctx, knowledge.Question{Text: "What is Go?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Sources) != 2 {
		t.Fatalf("sources = %d, want K=2", len(ans.Sources))
	}
	if ans.Strategy != "rag" || ans.Text != "ok [1]" {
		t.Fatalf("strategy=%q text=%q", ans.Strategy, ans.Text)
	}
	if ans.Usage == nil || ans.Usage.InputTokens != 7 {
		t.Fatalf("usage not passed through: %+v", ans.Usage)
	}
}

// missVector hides one document from similarity search, standing in for an
// embedder that never ranks it. Everything else delegates to the real store.
type missVector struct {
	vectorstores.VectorStore
	hide string
}

func (m missVector) SimilaritySearchWithScore(ctx context.Context, q string, k int, f vectorstores.Filter) ([]vectorstores.Scored, error) {
	all, err := m.VectorStore.SimilaritySearchWithScore(ctx, q, k+1, f)
	if err != nil {
		return nil, err
	}
	var out []vectorstores.Scored
	for _, s := range all {
		if s.Document.PageContent != m.hide && len(out) < k {
			out = append(out, s)
		}
	}
	return out, nil
}

func TestHybridFindsKeywordOnlyHit(t *testing.T) {
	ctx := context.Background()
	ix := NewIndexer(newStore(), nil)
	if _, err := ix.Index(ctx, loader(
		"d1.txt", "alpha beta gamma delta",
		"d2.txt", "alpha beta gamma delta epsilon",
		"d3.txt", "zyxqwv",
	)); err != nil {
		t.Fatal(err)
	}
	const query = "alpha beta gamma delta zyxqwv"

	has := func(hybrid bool) bool {
		store := missVector{ix.store, "zyxqwv"}
		p := &Pipeline{Store: store, Docs: ix.Documents(), Model: answerModel(nil), K: 3, Hybrid: hybrid}
		srcs, err := p.Retrieve(ctx, knowledge.Question{Text: query})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range srcs {
			if s.Content == "zyxqwv" {
				return true
			}
		}
		return false
	}
	if has(false) {
		t.Fatal("vector-only retrieval unexpectedly found the keyword document")
	}
	if !has(true) {
		t.Fatal("hybrid retrieval did not find the keyword-only document")
	}
}

func TestMaxContextTokensTrims(t *testing.T) {
	ctx := context.Background()
	ix := NewIndexer(newStore(), nil)
	var pairs []string
	for i := 0; i < 4; i++ {
		// 100 runes each: 25 tokens with the default heuristic.
		pairs = append(pairs, fmt.Sprintf("s%d.txt", i), fmt.Sprintf("%d", i)+strings.Repeat("x", 99))
	}
	if _, err := ix.Index(ctx, loader(pairs...)); err != nil {
		t.Fatal(err)
	}
	p := &Pipeline{Store: ix.store, Docs: ix.Documents(), Model: answerModel(nil), K: 4, MaxContextTokens: 60}

	srcs, err := p.Retrieve(ctx, knowledge.Question{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 {
		t.Fatalf("sources after budget = %d, want 2 (50 of 60 tokens)", len(srcs))
	}
}

func TestEmptyQuestion(t *testing.T) {
	p := &Pipeline{Store: newStore(), Model: answerModel(nil)}
	_, err := p.Answer(context.Background(), knowledge.Question{Text: "   "})
	if !errors.Is(err, knowledge.ErrEmptyQuestion) {
		t.Fatalf("err = %v, want ErrEmptyQuestion", err)
	}
}

func TestEmptyStore(t *testing.T) {
	model := answerModel(nil)
	p := &Pipeline{Store: newStore(), Model: model, Hybrid: true}
	ans, err := p.Answer(context.Background(), knowledge.Question{Text: "anything"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Sources) != 0 {
		t.Fatalf("sources = %d, want 0", len(ans.Sources))
	}
	if len(model.Calls) != 0 {
		t.Fatalf("model called %d times without sources, want 0", len(model.Calls))
	}
}

func TestConcurrentIndexAnswer(t *testing.T) {
	ctx := context.Background()
	ix := NewIndexer(newStore(), nil)
	if _, err := ix.Index(ctx, loader("a.txt", "Go is a compiled language.")); err != nil {
		t.Fatal(err)
	}
	model := answerModel(nil)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for g := 0; g < 4; g++ {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for n := 0; n < 5; n++ {
				src := fmt.Sprintf("f%d.txt", g)
				if _, err := ix.Index(ctx, loader(src, fmt.Sprintf("content %d %d", g, n))); err != nil {
					errs <- err
				}
			}
		}(g)
		go func() {
			defer wg.Done()
			for n := 0; n < 5; n++ {
				p := &Pipeline{Store: ix.store, Docs: ix.Documents(), Model: model, K: 2, Hybrid: true}
				if _, err := p.Answer(ctx, knowledge.Question{Text: "Go language"}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
