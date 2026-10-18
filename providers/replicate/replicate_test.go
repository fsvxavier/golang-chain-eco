package replicate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/providers/replicate"
)

func TestGenerateSync(t *testing.T) {
	var got map[string]any
	var auth, prefer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, prefer = r.Header.Get("Authorization"), r.Header.Get("Prefer")
		if r.URL.Path != "/models/meta/llama/predictions" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"id":"p1","status":"succeeded","output":["Hel","lo"],"metrics":{"input_token_count":4,"output_token_count":2}}`)
	}))
	defer srv.Close()
	m := &replicate.Chat{Model: "meta/llama", APIKey: "r8_x", BaseURL: srv.URL}
	out, err := m.Generate(context.Background(), []messages.Message{messages.System("be brief"), messages.Human("hi")}, models.WithMaxTokens(50), models.WithTemperature(0.2))
	if err != nil || out.Content != "Hello" || out.Usage.TotalTokens != 6 {
		t.Fatalf("%+v %v", out, err)
	}
	in := got["input"].(map[string]any)
	if auth != "Bearer r8_x" || prefer != "wait" || in["prompt"] != "hi" || in["system_prompt"] != "be brief" || in["max_tokens"].(float64) != 50 {
		t.Fatalf("%s %s %v", auth, prefer, got)
	}
	st, err := models.Collect(m.Stream(context.Background(), []messages.Message{messages.Human("hi")}))
	if err != nil || st.Content != "Hello" {
		t.Fatal(st, err)
	}
}

func TestPollingAndFailure(t *testing.T) {
	var polls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprintf(w, `{"id":"p2","status":"processing","urls":{"get":"%s/predictions/p2"}}`, srv.URL)
			return
		}
		if polls.Add(1) < 2 {
			fmt.Fprint(w, `{"id":"p2","status":"processing"}`)
			return
		}
		fmt.Fprint(w, `{"id":"p2","status":"succeeded","output":"done"}`)
	}))
	defer srv.Close()
	m := &replicate.Chat{Model: "a/b", APIKey: "k", BaseURL: srv.URL, PollEvery: time.Millisecond}
	out, err := m.Generate(context.Background(), []messages.Message{messages.Human("x"), messages.AI("y"), messages.Human("z")})
	if err != nil || out.Content != "done" || polls.Load() != 2 {
		t.Fatalf("%+v %v polls=%d", out, err, polls.Load())
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"p3","status":"failed","error":"model exploded"}`)
	}))
	defer bad.Close()
	if _, err := (&replicate.Chat{Model: "a/b", BaseURL: bad.URL}).Generate(context.Background(), []messages.Message{messages.Human("x")}); err == nil {
		t.Fatal("failed prediction must error")
	}
}
