package cohere_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/cohere"
)

func TestChatEmbedRerank(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.Header.Get("Authorization") != "Bearer ck" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v2/chat":
			if req["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, d := range []string{
					`{"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"Hi "}}}}`,
					`{"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"you"}}}}`,
					`{"type":"tool-call-start","index":0,"delta":{"message":{"tool_calls":{"id":"c1","type":"function","function":{"name":"w","arguments":""}}}}}`,
					`{"type":"tool-call-delta","index":0,"delta":{"message":{"tool_calls":{"function":{"arguments":"{\"a\":1}"}}}}}`,
					`{"type":"message-end","delta":{"usage":{"tokens":{"input_tokens":2,"output_tokens":3}}}}`,
				} {
					fmt.Fprintf(w, "data: %s\n\n", d)
				}
				return
			}
			fmt.Fprint(w, `{"id":"x","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"tool_calls":[{"id":"c9","type":"function","function":{"name":"w","arguments":"{\"b\":2}"}}]},"finish_reason":"TOOL_CALL","usage":{"tokens":{"input_tokens":1,"output_tokens":2}}}`)
		case "/v2/embed":
			fmt.Fprint(w, `{"embeddings":{"float":[[1,0],[0,1]]}}`)
		case "/v2/rerank":
			fmt.Fprint(w, `{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	m := &cohere.Chat{Model: "command", APIKey: "ck", BaseURL: srv.URL}
	out, err := m.Generate(ctx, []messages.Message{messages.System("s"), messages.Human("hi")})
	if err != nil || out.Content != "hello" || len(out.ToolCalls) != 1 || out.ToolCalls[0].Args["b"] != float64(2) || out.Usage.TotalTokens != 3 {
		t.Fatalf("%+v %v", out, err)
	}
	st, err := models.Collect(m.Stream(ctx, []messages.Message{messages.Human("hi")}))
	if err != nil || st.Content != "Hi you" || len(st.ToolCalls) != 1 || st.ToolCalls[0].Args["a"] != float64(1) {
		t.Fatalf("%+v %v", st, err)
	}
	e := &cohere.Embeddings{APIKey: "ck", BaseURL: srv.URL}
	if vs, err := e.EmbedDocuments(ctx, []string{"a", "b"}); err != nil || len(vs) != 2 {
		t.Fatal(vs, err)
	}
	rr := &cohere.Reranker{APIKey: "ck", BaseURL: srv.URL}
	docs, err := rr.Rerank(ctx, "q", []documents.Document{{PageContent: "low"}, {PageContent: "high"}})
	if err != nil || docs[0].PageContent != "high" || docs[0].Metadata["relevance_score"] != 0.9 {
		t.Fatalf("%+v %v", docs, err)
	}
}
