package openai_test

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
	"github.com/fsvxavier/golang-chain-eco/providers/openai"
)

type args struct {
	City string `json:"city"`
}

func TestGenerateWithTools(t *testing.T) {
	var req map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Rome\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`)
	}))
	defer srv.Close()

	m := &openai.Chat{Model: "gpt-x", APIKey: "sk-test", BaseURL: srv.URL}
	w := tools.New("weather", "w", func(context.Context, args) (any, error) { return "", nil })
	out, err := m.Generate(context.Background(),
		[]messages.Message{messages.System("be brief"), messages.Human("hi")},
		models.WithTools(w), models.WithTemperature(0.2))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-test" || req["model"] != "gpt-x" || req["temperature"] != 0.2 {
		t.Fatalf("request: %v auth=%q", req, auth)
	}
	if ts, _ := req["tools"].([]any); len(ts) != 1 {
		t.Fatalf("tools not sent: %v", req)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "weather" || out.ToolCalls[0].Args["city"] != "Rome" {
		t.Fatalf("%+v", out)
	}
	if out.Usage == nil || out.Usage.TotalTokens != 12 {
		t.Fatalf("usage: %+v", out.Usage)
	}
}

func TestStreamAndError(t *testing.T) {
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"slow down"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, d := range []string{
			`{"choices":[{"delta":{"role":"assistant","content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c9","function":{"name":"weather","arguments":"{\"ci"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Oslo\"}"}}]}}]}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", d)
		}
	}))
	defer srv.Close()
	m := &openai.Chat{Model: "gpt-x", APIKey: "k", BaseURL: srv.URL}
	out, err := models.Collect(m.Stream(context.Background(), []messages.Message{messages.Human("hi")}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "Hello" || len(out.ToolCalls) != 1 || out.ToolCalls[0].Args["city"] != "Oslo" {
		t.Fatalf("%+v", out)
	}
	fail = true
	if _, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want 429 error, got %v", err)
	}
}

func TestEmbeddings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`)
	}))
	defer srv.Close()
	e := &openai.Embeddings{Chat: openai.Chat{APIKey: "k", BaseURL: srv.URL}}
	vs, err := e.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil || vs[0][0] != 1 || vs[1][1] != 1 {
		t.Fatalf("%v %v", vs, err)
	}
}

func TestAzure(t *testing.T) {
	var path, query, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, key = r.URL.Path, r.URL.RawQuery, r.Header.Get("api-key")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))
	defer srv.Close()
	m := &openai.Chat{Model: "ignored", Azure: &openai.Azure{Endpoint: srv.URL, Deployment: "gpt4o", APIKey: "az"}}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("x")})
	if err != nil || out.Content != "hi" {
		t.Fatal(out, err)
	}
	if path != "/openai/deployments/gpt4o/chat/completions" || query != "api-version=2024-10-21" || key != "az" {
		t.Fatal(path, query, key)
	}
}

func TestCachedTokensUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":40,"completion_tokens":7,"total_tokens":47,"prompt_tokens_details":{"cached_tokens":12}}}`)
	}))
	defer srv.Close()
	m := &openai.Chat{Model: "gpt-x", APIKey: "sk-test", BaseURL: srv.URL}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")})
	if err != nil {
		t.Fatal(err)
	}
	u := out.Usage
	if u == nil || u.CacheReadTokens != 12 || u.InputTokens != 40 || u.TotalTokens != 47 {
		t.Fatalf("%+v", u)
	}
}
