package tasktrack

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func walk(t *testing.T, tr *Tracker, id string, path ...Status) {
	t.Helper()
	for _, to := range path {
		if err := tr.Move(context.Background(), Task, id, to, "a", ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTaskFlowAllowsRework(t *testing.T) {
	tr := New()
	walk(t, tr, "T1", TaskAssignment, InDevelopment, InCodeReview, ReviewRejected, InDevelopment,
		InCodeReview, ReviewApproved, InQATesting, QARejected, InDevelopment,
		InCodeReview, ReviewApproved, InQATesting, QAApproved, Committed, Completed)
	if tr.Status("T1") != Completed || len(tr.History()) != 16 {
		t.Fatalf("%s / %d", tr.Status("T1"), len(tr.History()))
	}
}

func TestIllegalMoveIsRejectedAndNotRecorded(t *testing.T) {
	var out bytes.Buffer
	tr := New(Console{&out})
	walk(t, tr, "T1", TaskAssignment, InDevelopment)
	if err := tr.Move(context.Background(), Task, "T1", Committed, "a", ""); err == nil {
		t.Fatal("development must not jump to committed")
	}
	if tr.Status("T1") != InDevelopment || strings.Contains(out.String(), "committed") {
		t.Fatal("a rejected move must leave no trace")
	}
	if err := tr.Move(context.Background(), Request, "R", TaskAssignment, "m", ""); err == nil {
		t.Fatal("a request starts at received")
	}
}

func TestWebhookSinksAndFilter(t *testing.T) {
	var slack, chat []map[string]any
	post := func(into *[]map[string]any) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			*into = append(*into, m)
		}))
	}
	s, c := post(&slack), post(&chat)
	defer s.Close()
	defer c.Close()
	tr := New(Filter{N: Slack(s.URL), Only: []Status{ReviewRejected, QARejected, Completed}}, Chat(c.URL))
	walk(t, tr, "T1", TaskAssignment, InDevelopment, InCodeReview, ReviewRejected)
	if len(slack) != 1 || !strings.Contains(slack[0]["text"].(string), "review_rejected") {
		t.Fatalf("slack must get only the filtered statuses: %v", slack)
	}
	if len(chat) != 4 || chat[3]["event"].(map[string]any)["to"] != "review_rejected" {
		t.Fatalf("chat must get every move: %v", chat)
	}
}

func TestNotifierFailureDoesNotBlock(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	var errs int
	tr := New(Slack(bad.URL))
	tr.OnError = func(error) { errs++ }
	walk(t, tr, "T1", TaskAssignment)
	if errs != 1 || tr.Status("T1") != TaskAssignment {
		t.Fatal("work must go on when a sink fails")
	}
}

func TestFromEnvAndEmailMessage(t *testing.T) {
	t.Setenv("NOTIFY", "console,slack,email")
	if _, err := FromEnv(io.Discard); err == nil || !strings.Contains(err.Error(), "SLACK_WEBHOOK_URL") || !strings.Contains(err.Error(), "SMTP_ADDR") {
		t.Fatalf("must name every missing variable: %v", err)
	}
	t.Setenv("NOTIFY", "bogus")
	if _, err := FromEnv(io.Discard); err == nil {
		t.Fatal("unknown target must fail")
	}
	m := string(SMTP{From: "team@x", To: []string{"a@x", "b@x"}}.Message(Event{Subject: "T3", To: QARejected, Detail: "AC2 fails"}))
	if !strings.Contains(m, "Subject: [T3] qa_rejected") || !strings.Contains(m, "To: a@x, b@x") || !strings.Contains(m, "AC2 fails") {
		t.Fatal(m)
	}
}
