package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/outputparsers"
	"github.com/fsvxavier/golang-chain-eco/core/prompts"
	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/textsplitters"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
)

func TestChain(t *testing.T) {
	p := prompts.Chat([2]string{"human", "hi {name}"})
	m := models.AsRunnable(models.NewFake(messages.AI("hello bob")))
	chain := runnables.Pipe3[prompts.Values, []messages.Message, messages.Message, string](p, m, outputparsers.Str{})
	out, err := chain.Invoke(context.Background(), prompts.Values{"name": "bob"})
	if err != nil || out != "hello bob" {
		t.Fatal(out, err)
	}
	if _, err := p.Invoke(context.Background(), prompts.Values{}); err == nil {
		t.Fatal("missing variable must fail")
	}
}

func TestAddMessages(t *testing.T) {
	a := messages.Human("1")
	a.ID = "x"
	b := messages.AI("2")
	b.ID = "x"
	out, _ := messages.AddMessages([]messages.Message{a}, []messages.Message{b, messages.Human("3")})
	if len(out) != 2 || out[0].Content != "2" {
		t.Fatalf("%+v", out)
	}
	out, _ = messages.AddMessages(out, []messages.Message{messages.Remove("x")})
	if len(out) != 1 {
		t.Fatalf("%+v", out)
	}
}

func TestSplitterAndVectorStore(t *testing.T) {
	text := strings.Repeat("alpha beta gamma. ", 50)
	chunks := textsplitters.NewRecursive(100, 10).SplitText(text)
	if len(chunks) < 5 {
		t.Fatal(len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 100 {
			t.Fatalf("chunk too long: %d", len(c))
		}
	}
	vs, err := vectorstores.FromDocuments(context.Background(), embeddings.Fake{Dim: 64},
		[]documents.Document{{PageContent: "cats purr"}, {PageContent: "rockets fly"}})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := vectorstores.SimilaritySearch(context.Background(), vs, "cats purr", 1, nil)
	if err != nil || len(docs) != 1 || docs[0].PageContent != "cats purr" {
		t.Fatal(docs, err)
	}
}
