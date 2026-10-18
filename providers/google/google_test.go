package google_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/providers/google"
)

type args struct {
	City string `json:"city"`
}

func TestGenerateToolsAndStream(t *testing.T) {
	var req map[string]any
	var key, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, path = r.Header.Get("x-goog-api-key"), r.URL.Path+"?"+r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(r.URL.Path, "streamGenerateContent") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hel\"}]}}]}\n\n")
			fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"},{\"functionCall\":{\"name\":\"weather\",\"args\":{\"city\":\"Rome\"}}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n")
			return
		}
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"ok"},{"functionCall":{"name":"weather","args":{"city":"Oslo"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`)
	}))
	defer srv.Close()
	m := &google.Chat{Model: "gemini-x", APIKey: "gk", BaseURL: srv.URL}
	w := tools.New("weather", "w", func(context.Context, args) (any, error) { return "", nil })
	hist := []messages.Message{
		messages.System("sys"), messages.Human("hi"),
		messages.AI("", messages.ToolCall{ID: "t1", Name: "weather", Args: map[string]any{"city": "X"}}),
		messages.Tool("rain", "t1"),
	}
	out, err := m.Generate(context.Background(), hist, models.WithTools(w), models.WithTemperature(0.1))
	if err != nil {
		t.Fatal(err)
	}
	if key != "gk" || !strings.HasSuffix(path, "/models/gemini-x:generateContent?") {
		t.Fatal(key, path)
	}
	cs := req["contents"].([]any)
	if len(cs) != 3 || cs[1].(map[string]any)["role"] != "model" {
		t.Fatalf("contents: %v", cs)
	}
	fr := cs[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "weather" {
		t.Fatalf("function response must carry the tool name: %v", fr)
	}
	if req["systemInstruction"] == nil || req["tools"] == nil {
		t.Fatal(req)
	}
	if out.Content != "ok" || len(out.ToolCalls) != 1 || out.ToolCalls[0].Args["city"] != "Oslo" || out.Usage.TotalTokens != 3 {
		t.Fatalf("%+v", out)
	}

	st, err := models.Collect(m.Stream(context.Background(), []messages.Message{messages.Human("hi")}))
	if err != nil || st.Content != "Hello" || len(st.ToolCalls) != 1 || st.ToolCalls[0].Args["city"] != "Rome" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestVertexAndEmbeddings(t *testing.T) {
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		if strings.Contains(r.URL.Path, "batchEmbedContents") {
			fmt.Fprint(w, `{"embeddings":[{"values":[1,0]},{"values":[0,1]}]}`)
			return
		}
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"vertex"}]}}]}`)
	}))
	defer srv.Close()
	m := &google.Chat{Model: "gemini-x", BaseURL: srv.URL, Vertex: &google.Vertex{
		Project: "p", Location: "l", Token: func(context.Context) (string, error) { return "tok", nil }}}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")})
	if err != nil || out.Content != "vertex" || auth != "Bearer tok" || path != "/projects/p/locations/l/publishers/google/models/gemini-x:generateContent" {
		t.Fatal(out, err, auth, path)
	}
	e := &google.Embeddings{APIKey: "k", BaseURL: srv.URL}
	vs, err := e.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil || len(vs) != 2 || vs[1][1] != 1 {
		t.Fatal(vs, err)
	}
}
