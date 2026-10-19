package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/openswe/guards"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

const whSecret = "lin_wh_test_secret"

// vector computed outside this package with Python's hmac over the raw body.
var vectorBody = []byte(`{"action":"create","type":"Issue","webhookTimestamp":1700000000000,"data":{"id":"iss-1"}}`)

const vectorSig = "3312597b54aa2e0b08c8bcecd4ef710ffd7715177ca0943439d7f5b328fedb8c"

func TestVerifySignatureVector(t *testing.T) {
	if err := VerifySignature(whSecret, vectorSig, vectorBody); err != nil {
		t.Fatalf("valid vector rejected: %v", err)
	}
	if err := VerifySignature("nope", vectorSig, vectorBody); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong secret accepted: %v", err)
	}
	if err := VerifySignature(whSecret, "zz-not-hex", vectorBody); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("non-hex accepted: %v", err)
	}
	if Sign(whSecret, vectorBody) != vectorSig {
		t.Fatal("Sign disagrees with the vector")
	}
}

type fixture struct {
	t     *testing.T
	wh    *Webhook
	d     *triggers.Dispatcher
	mu    sync.Mutex
	tasks []triggers.Task
	now   time.Time
	gql   *httptest.Server
	gqlMu sync.Mutex
	gqlOp []map[string]any
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, now: time.UnixMilli(1700000000000)}
	f.gql = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.gqlMu.Lock()
		defer f.gqlMu.Unlock()
		if r.Header.Get("Authorization") != "lin_api_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Authentication required"}]}`))
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.gqlOp = append(f.gqlOp, req)
		_, _ = w.Write([]byte(`{"data":{"commentCreate":{"success":true,"comment":{"id":"c-1"}}}}`))
	}))
	t.Cleanup(f.gql.Close)
	rec := triggers.RunnerFunc(func(_ context.Context, task triggers.Task) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tasks = append(f.tasks, task)
		return nil
	})
	f.d = &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: rec}, Queue: &guards.MemoryQueue{}}
	f.wh = &Webhook{Secret: whSecret, Dispatcher: f.d, BotUserID: "bot-user", BotName: "openswe", TriggerLabel: "openswe",
		Now: func() time.Time { return f.now }}
	return f
}

func (f *fixture) send(body []byte, sig string, delivery string) (int, string) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/linear/webhook", strings.NewReader(string(body)))
	req.Header.Set("Linear-Signature", sig)
	if delivery != "" {
		req.Header.Set("Linear-Delivery", delivery)
	}
	rec := httptest.NewRecorder()
	f.wh.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// signed builds a body with the given webhookTimestamp (ms) and signs it.
func (f *fixture) signed(t *testing.T, ts int64, obj map[string]any) ([]byte, string) {
	t.Helper()
	obj["webhookTimestamp"] = ts
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return b, Sign(whSecret, b)
}

func (f *fixture) runs() []triggers.Task {
	f.d.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]triggers.Task(nil), f.tasks...)
}

func (f *fixture) nowMS() int64 { return f.now.UnixMilli() }

func TestBadAndStaleWebhooksRejected(t *testing.T) {
	f := newFixture(t)
	body, sig := f.signed(t, f.nowMS(), map[string]any{"action": "create", "type": "Issue", "data": map[string]any{"id": "iss-1", "assigneeId": "bot-user"}})
	if code, _ := f.send(body, "00"+sig[2:], ""); code != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", code)
	}
	old, oldSig := f.signed(t, f.nowMS()-61_000, map[string]any{"action": "create", "type": "Issue", "data": map[string]any{"id": "iss-1", "assigneeId": "bot-user"}})
	if code, body := f.send(old, oldSig, ""); code != http.StatusUnauthorized || !strings.Contains(body, "outside") {
		t.Fatalf("stale webhook: %d %s", code, body)
	}
	if n := len(f.runs()); n != 0 {
		t.Fatalf("rejected webhooks started %d runs", n)
	}
}

func TestAssignedIssueStartsRunOnIssueThread(t *testing.T) {
	f := newFixture(t)
	body, sig := f.signed(t, f.nowMS(), map[string]any{"action": "create", "type": "Issue", "data": map[string]any{
		"id": "iss-1", "identifier": "ENG-1", "title": "Login crashes", "description": "on empty user", "assigneeId": "bot-user"}})
	code, resp := f.send(body, sig, "d-1")
	if code != http.StatusOK || !strings.Contains(resp, "dispatched") {
		t.Fatalf("%d %s", code, resp)
	}
	runs := f.runs()
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if want := triggers.ThreadKey(triggers.KindAgent, "linear:issue:iss-1"); runs[0].ThreadID != want {
		t.Fatalf("thread %s, want %s", runs[0].ThreadID, want)
	}
	if runs[0].Source != "linear" || runs[0].Metadata["linear_issue_id"] != "iss-1" {
		t.Fatalf("metadata: %+v", runs[0])
	}
	if !strings.Contains(runs[0].Messages[0].Text(), `<untrusted_linear`) {
		t.Fatalf("description not fenced: %s", runs[0].Messages[0].Text())
	}
}

func TestLabelOnUpdateOnlyWhenLabelsChanged(t *testing.T) {
	f := newFixture(t)
	labelled := map[string]any{"id": "iss-2", "identifier": "ENG-2", "title": "x", "labels": []any{map[string]any{"name": "openswe"}}}
	// title edit on a labelled issue: no label change, no run
	body, sig := f.signed(t, f.nowMS(), map[string]any{"action": "update", "type": "Issue", "data": labelled,
		"updatedFrom": map[string]any{"title": "old"}})
	if _, resp := f.send(body, sig, "d-2"); !strings.Contains(resp, "ignored") {
		t.Fatalf("title edit should be ignored: %s", resp)
	}
	// label added: run
	body, sig = f.signed(t, f.nowMS(), map[string]any{"action": "update", "type": "Issue", "data": labelled,
		"updatedFrom": map[string]any{"labelIds": []any{}}})
	if _, resp := f.send(body, sig, "d-3"); !strings.Contains(resp, "dispatched") {
		t.Fatalf("label added: %s", resp)
	}
	if n := len(f.runs()); n != 1 {
		t.Fatalf("want 1 run, got %d", n)
	}
}

func TestMentionCommentStartsRunUnlessBotWrote(t *testing.T) {
	f := newFixture(t)
	mk := func(user, text string) ([]byte, string) {
		return f.signed(t, f.nowMS(), map[string]any{"action": "create", "type": "Comment", "data": map[string]any{
			"id": "c-9", "issueId": "iss-3", "body": text, "user": map[string]any{"id": user, "name": "Ana"}}})
	}
	b, s := mk("bot-user", "@openswe loop?")
	if _, resp := f.send(b, s, "d-4"); !strings.Contains(resp, "ignored") {
		t.Fatalf("bot's own comment ran: %s", resp)
	}
	b, s = mk("u-ana", "no mention here")
	if _, resp := f.send(b, s, "d-5"); !strings.Contains(resp, "ignored") {
		t.Fatalf("unmentioned comment ran: %s", resp)
	}
	b, s = mk("u-ana", "hey @openswe can you take this?")
	if _, resp := f.send(b, s, "d-6"); !strings.Contains(resp, "dispatched") {
		t.Fatalf("mention ignored: %s", resp)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].ThreadID != triggers.ThreadKey(triggers.KindAgent, "linear:issue:iss-3") {
		t.Fatalf("runs: %+v", runs)
	}
}

func TestDuplicateDeliveryRunsOnce(t *testing.T) {
	f := newFixture(t)
	body, sig := f.signed(t, f.nowMS(), map[string]any{"action": "create", "type": "Issue", "data": map[string]any{"id": "iss-1", "assigneeId": "bot-user"}})
	f.send(body, sig, "same-delivery")
	code, resp := f.send(body, sig, "same-delivery")
	if code != http.StatusOK || !strings.Contains(resp, "duplicate") {
		t.Fatalf("redelivery: %d %s", code, resp)
	}
	if n := len(f.runs()); n != 1 {
		t.Fatalf("redelivery ran %d times", n)
	}
}

func TestNotifierCommentsOutcomeOnIssue(t *testing.T) {
	f := newFixture(t)
	c := NewClient("lin_api_test")
	c.Endpoint = f.gql.URL
	n := Notifier{Client: c, Inner: triggers.RunnerFunc(func(context.Context, triggers.Task) error { return nil }),
		Describe: func(_ context.Context, _ triggers.Task, final string, err error) string {
			return fmt.Sprintf("Opened PR https://github.com/acme/api/pull/101 (answer=%q, err=%v)", final, err)
		}}
	task := triggers.Task{Metadata: map[string]any{"linear_issue_id": "iss-1"}}
	if err := n.Run(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	f.gqlMu.Lock()
	defer f.gqlMu.Unlock()
	if len(f.gqlOp) != 1 {
		t.Fatalf("want 1 GraphQL call, got %d", len(f.gqlOp))
	}
	q, _ := f.gqlOp[0]["query"].(string)
	in := f.gqlOp[0]["variables"].(map[string]any)["input"].(map[string]any)
	if !strings.Contains(q, "commentCreate") || in["issueId"] != "iss-1" || !strings.Contains(in["body"].(string), "pull/101") {
		t.Fatalf("comment: %s %+v", q, in)
	}
}

func TestGraphQLErrorsMapped(t *testing.T) {
	f := newFixture(t)
	c := NewClient("wrong-key")
	c.Endpoint = f.gql.URL
	_, err := c.CreateComment(context.Background(), "iss-1", "hi")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized || !strings.Contains(ae.Message, "Authentication required") {
		t.Fatalf("auth error: %#v", err)
	}
	// a 200 response with an errors array is still an error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Entity not found: Issue"}]}`))
	}))
	defer srv.Close()
	c = NewClient("lin_api_test")
	c.Endpoint = srv.URL
	if _, err := c.Issue(context.Background(), "nope"); err == nil || !strings.Contains(err.Error(), "Entity not found") {
		t.Fatalf("graphql error lost: %v", err)
	}
}

// docComment is the Comment create example from linear.app/developers/webhooks,
// verbatim except body and userId, which the test fills in. It has userId
// and no user object.
const docComment = `{
  "action": "create",
  "actor": {"id": "b5ea5f1f-8adc-4f52-b4bd-ab4e84cf51ba", "type": "user", "name": "Linear Orbit",
    "email": "orbit@linear.app", "url": "https://linear.app/company/profiles/orbit"},
  "data": {"id": "2174add1-f7c8-44e3-bbf3-2d60b5ea8bc9", "createdAt": "2020-01-23T12:53:18.084Z",
    "updatedAt": "2020-01-23T12:53:18.084Z", "archivedAt": null, "body": %q, "edited": false,
    "issueId": "539068e2-ae88-4d09-bd75-22eb4a59612f", "userId": %q},
  "type": "Comment",
  "url": "https://linear.app/issue/LIN-1778/foo-bar#comment-77217de3-fb52-4dad-bb9a-b356beb93de8",
  "createdAt": "2020-01-23T12:53:18.084Z",
  "organizationId": "dc844923-f9a4-40a3-825c-dea7747e57d6",
  "webhookTimestamp": %d,
  "webhookId": "000042e3-d123-4980-b49f-8e140eef9329"
}`

func TestDocumentedCommentPayload(t *testing.T) {
	f := newFixture(t)
	send := func(user, delivery string) string {
		b := []byte(fmt.Sprintf(docComment, "@openswe please fix", user, f.nowMS()))
		_, resp := f.send(b, Sign(whSecret, b), delivery)
		return resp
	}
	if resp := send("bot-user", "d-doc-1"); !strings.Contains(resp, "ignored") {
		t.Fatalf("bot's own comment (userId only) ran: %s", resp)
	}
	if resp := send("u-ana", "d-doc-2"); !strings.Contains(resp, "dispatched") {
		t.Fatalf("documented payload ignored: %s", resp)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Metadata["requester"] != "u-ana" ||
		runs[0].Metadata["linear_issue_id"] != "539068e2-ae88-4d09-bd75-22eb4a59612f" {
		t.Fatalf("runs: %+v", runs)
	}
}
