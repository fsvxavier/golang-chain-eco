package knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// sourced answers with one source, so the sources list is exercised.
type sourced struct{}

func (sourced) Answer(_ context.Context, q Question) (Answer, error) {
	return Answer{Text: "resposta [1]", Sources: []Source{{ID: "doc-1", Content: "x"}}, Strategy: "rag"}, nil
}

func names(ts []tools.Tool) map[string]bool {
	m := map[string]bool{}
	for _, t := range ts {
		m[t.Name()] = true
	}
	return m
}

func TestToolkitListsOnlyAvailable(t *testing.T) {
	if n := len((Toolkit{}).All()); n != 0 {
		t.Fatalf("empty toolkit exposes %d tools", n)
	}
	got := names(Toolkit{RAG: ragStub}.All())
	if !got["rag_answer"] || got["cag_answer"] || got["kag_query"] || got["knowledge_answer"] || got["knowledge_ingest"] {
		t.Fatalf("only rag_answer expected, got %v", got)
	}
	k := Toolkit{RAG: ragStub, CAG: cagStub, KAG: kagStub, Router: &Router{RAG: ragStub, CAG: cagStub, KAG: kagStub}, Ingest: func(context.Context, []documents.Document) error { return nil }}
	got = names(k.All())
	for _, want := range []string{"rag_answer", "cag_answer", "kag_query", "knowledge_answer", "knowledge_ingest"} {
		if !got[want] {
			t.Errorf("%s missing", want)
		}
	}
	if len(got) != 5 {
		t.Errorf("want 5 tools, got %v", got)
	}
}

func TestToolArgsAreAnySlice(t *testing.T) {
	var seen []documents.Document
	k := Toolkit{Ingest: func(_ context.Context, docs []documents.Document) error { seen = docs; return nil }}
	ing, _ := tools.Find(k.All(), "knowledge_ingest")
	args := map[string]any{"documents": []any{
		map[string]any{"id": "a", "content": "Varfarina interage com Aspirina."},
		map[string]any{"content": "sem id", "metadata": map[string]any{"fonte": "b.md"}},
	}}
	if _, err := ing.Call(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0].ID != "a" || seen[0].PageContent != "Varfarina interage com Aspirina." || seen[1].Metadata["fonte"] != "b.md" {
		t.Fatalf("decoded docs: %+v", seen)
	}
	if _, err := ing.Call(context.Background(), map[string]any{"documents": []any{"texto solto"}}); !tools.IsToolError(err) {
		t.Fatalf("non-object document must be a tool error, got %v", err)
	}
}

func TestToolReturnsArtifact(t *testing.T) {
	ans, _ := tools.Find((Toolkit{RAG: sourced{}}).All(), "rag_answer")
	out, err := ans.Call(context.Background(), map[string]any{"question": "q"})
	if err != nil {
		t.Fatal(err)
	}
	ca, ok := out.(tools.ContentAndArtifact)
	if !ok {
		t.Fatalf("want ContentAndArtifact, got %T", out)
	}
	text := ca.Content.(string)
	if !strings.Contains(text, "resposta [1]") || !strings.Contains(text, "doc-1") {
		t.Errorf("content lacks answer or source: %q", text)
	}
	if a, ok := ca.Artifact.(Answer); !ok || a.Strategy != "rag" {
		t.Errorf("artifact %#v", ca.Artifact)
	}
}

func TestMCPServerToolsList(t *testing.T) {
	srv := (Toolkit{RAG: ragStub}).MCPServer(nil)
	reply := string(srv.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	if !strings.Contains(reply, `"rag_answer"`) || strings.Contains(reply, "knowledge_answer") {
		t.Fatalf("tools/list: %s", reply)
	}
}

func TestMCPCall(t *testing.T) {
	var calls []string
	srv := (Toolkit{RAG: sourced{}}).MCPServer(func(name string, _ map[string]any, _ time.Duration, _ error) {
		calls = append(calls, name)
	})
	reply := string(srv.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"rag_answer","arguments":{"question":"q"}}}`)))
	if !strings.Contains(reply, "resposta [1]") || !strings.Contains(reply, "doc-1") {
		t.Fatalf("tools/call: %s", reply)
	}
	if len(calls) != 1 || calls[0] != "rag_answer" {
		t.Fatalf("hook calls %v", calls)
	}
	var r struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(reply), &r); err != nil || len(r.Result.Content) != 1 {
		t.Fatalf("reply shape: %v %s", err, reply)
	}
}
