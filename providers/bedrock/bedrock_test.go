package bedrock_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

// Official AWS SigV4 example (IAM ListUsers, 2015-08-30).
func TestSigV4KnownVector(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	bedrock.Sign(req, nil, bedrock.Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"},
		"us-east-1", "iam", time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, SignedHeaders=content-type;host;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

type args struct {
	City string `json:"city"`
}

func TestConverse(t *testing.T) {
	var req map[string]any
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.EscapedPath()
		_ = json.NewDecoder(r.Body).Decode(&req)
		fmt.Fprint(w, `{"output":{"message":{"role":"assistant","content":[{"text":"hi"},{"toolUse":{"toolUseId":"u1","name":"weather","input":{"city":"Rome"}}}]}},"stopReason":"tool_use","usage":{"inputTokens":4,"outputTokens":5,"totalTokens":9}}`)
	}))
	defer srv.Close()
	m := &bedrock.Chat{Model: "anthropic.claude-3-5-sonnet-20241022-v2:0", Region: "eu-west-1", BaseURL: srv.URL,
		Credentials: bedrock.Credentials{AccessKeyID: "AK", SecretAccessKey: "SK"}}
	w := tools.New("weather", "w", func(context.Context, args) (any, error) { return "", nil })
	hist := []messages.Message{messages.System("sys"), messages.Human("hi"),
		messages.AI("", messages.ToolCall{ID: "o1", Name: "weather", Args: map[string]any{"city": "X"}}), messages.Tool("rain", "o1")}
	out, err := m.Generate(context.Background(), hist, models.WithTools(w), models.WithMaxTokens(100))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AK/") || !strings.Contains(auth, "/eu-west-1/bedrock/aws4_request") {
		t.Fatal(auth)
	}
	if !strings.Contains(path, "/converse") || req["system"] == nil || req["toolConfig"] == nil {
		t.Fatal(path, req)
	}
	ms := req["messages"].([]any)
	if len(ms) != 3 || ms[2].(map[string]any)["content"].([]any)[0].(map[string]any)["toolResult"] == nil {
		t.Fatalf("%v", ms)
	}
	if out.Content != "hi" || len(out.ToolCalls) != 1 || out.ToolCalls[0].Args["city"] != "Rome" || out.Usage.TotalTokens != 9 {
		t.Fatalf("%+v", out)
	}
}
