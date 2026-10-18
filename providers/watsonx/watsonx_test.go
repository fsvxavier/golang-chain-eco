package watsonx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/watsonx"
)

func server(t *testing.T) (*httptest.Server, *atomic.Int32, *[]map[string]any) {
	var iamCalls atomic.Int32
	var bodies []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/token", func(w http.ResponseWriter, r *http.Request) {
		iamCalls.Add(1)
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "apikey=KEY") || !strings.Contains(string(b), "grant_type=urn") {
			http.Error(w, "bad form", 400)
			return
		}
		fmt.Fprint(w, `{"access_token":"tok-1","expires_in":3600}`)
	})
	check := func(w http.ResponseWriter, r *http.Request) map[string]any {
		if r.Header.Get("Authorization") != "Bearer tok-1" || r.URL.Query().Get("version") == "" {
			http.Error(w, "unauthorized", 401)
			return nil
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		return body
	}
	mux.HandleFunc("/ml/v1/text/chat", func(w http.ResponseWriter, r *http.Request) {
		if check(w, r) != nil {
			fmt.Fprint(w, `{"id":"c1","model_id":"ibm/granite","choices":[{"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
		}
	})
	mux.HandleFunc("/ml/v1/text/chat_stream", func(w http.ResponseWriter, r *http.Request) {
		if check(w, r) != nil {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"s\",\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\ndata: {\"id\":\"s\",\"choices\":[{\"delta\":{\"content\":\"llo\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
	})
	mux.HandleFunc("/ml/v1/text/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if b := check(w, r); b != nil {
			n := len(b["inputs"].([]any))
			res := make([]string, n)
			for i := range res {
				res[i] = fmt.Sprintf(`{"embedding":[%d,0.5]}`, i)
			}
			fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(res, ","))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &iamCalls, &bodies
}

func TestChat(t *testing.T) {
	srv, iam, bodies := server(t)
	auth := &watsonx.Auth{APIKey: "KEY", IAMURL: srv.URL + "/identity/token"}
	m := watsonx.New("ibm/granite", watsonx.Config{URL: srv.URL, ProjectID: "proj-9", Auth: auth})
	ctx := context.Background()

	out, err := m.Generate(ctx, []messages.Message{messages.Human("hello")}, models.WithMaxTokens(64), models.WithToolChoice("required"))
	if err != nil || out.Content != "hi there" || out.Usage.TotalTokens != 5 {
		t.Fatalf("%+v %v", out, err)
	}
	b := (*bodies)[0]
	if b["model_id"] != "ibm/granite" || b["project_id"] != "proj-9" || b["max_tokens"].(float64) != 64 || b["tool_choice_option"] != "required" {
		t.Fatalf("dialect not applied: %v", b)
	}
	if _, bad := b["model"]; bad {
		t.Fatalf("model must be renamed: %v", b)
	}
	if m.ModelID() != "watsonx:ibm/granite" {
		t.Fatal(m.ModelID())
	}

	st, err := models.Collect(m.Stream(ctx, []messages.Message{messages.Human("hi")}))
	if err != nil || st.Content != "hello" {
		t.Fatalf("%+v %v", st, err)
	}
	if got := iam.Load(); got != 1 {
		t.Fatalf("IAM token must be cached across calls, got %d requests", got)
	}
}

func TestEmbeddingsAndAuthError(t *testing.T) {
	srv, _, _ := server(t)
	auth := &watsonx.Auth{APIKey: "KEY", IAMURL: srv.URL + "/identity/token"}
	e := &watsonx.Embeddings{Model: "ibm/slate", ProjectID: "p", URL: srv.URL, Auth: auth}
	vs, err := e.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil || len(vs) != 2 || vs[1][0] != 1 {
		t.Fatalf("%v %v", vs, err)
	}
	q, err := e.EmbedQuery(context.Background(), "x")
	if err != nil || len(q) != 2 {
		t.Fatal(q, err)
	}
	bad := &watsonx.Auth{APIKey: "WRONG", IAMURL: srv.URL + "/identity/token"}
	if _, err := bad.Token(context.Background()); err == nil {
		t.Fatal("bad key must fail")
	}
}
