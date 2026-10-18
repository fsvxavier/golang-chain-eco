package ollama_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/ollama"
)

func TestGenerateAndStream(t *testing.T) {
	var req map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		if req["stream"] == true {
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hel"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"lo"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"weather","arguments":{"city":"Rome"}}}]},"done":true,"prompt_eval_count":4,"eval_count":6}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hello"},"done":true,"prompt_eval_count":1,"eval_count":2}`)
	}))
	defer srv.Close()
	m := &ollama.Chat{Model: "llama3", BaseURL: srv.URL}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("hi")}, models.WithTemperature(0.5))
	if err != nil || out.Content != "hello" || req["model"] != "llama3" {
		t.Fatalf("%+v %v %v", out, err, req)
	}
	out, err = models.Collect(m.Stream(context.Background(), []messages.Message{messages.Human("hi")}))
	if err != nil || out.Content != "Hello" || len(out.ToolCalls) != 1 || out.ToolCalls[0].Args["city"] != "Rome" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestEmbeddings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"embeddings":[[1,0],[0,1]]}`)
	}))
	defer srv.Close()
	e := &ollama.Embeddings{Model: "nomic", BaseURL: srv.URL}
	vs, err := e.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil || len(vs) != 2 || vs[1][1] != 1 {
		t.Fatalf("%v %v", vs, err)
	}
}
