package slack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/openswe/guards"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

const secret = "8f742231b10e8888abcd99yyyzzz85a5"

// vector computed outside this package with Python's hmac over
// "v0:1531420618:" + body.
var vectorBody = []byte(`{"type":"url_verification","challenge":"3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P"}`)

const vectorTS = "1531420618"
const vectorSig = "v0=2b617e8d1eba789c3e90a54b772adadfc772137987c87d3c1e4129005632e406"

func headers(ts, sig string) http.Header {
	h := http.Header{}
	h.Set("X-Slack-Request-Timestamp", ts)
	h.Set("X-Slack-Signature", sig)
	return h
}

func TestVerifyRequest(t *testing.T) {
	now := time.Unix(1531420618+10, 0)
	if err := VerifyRequest(secret, headers(vectorTS, vectorSig), vectorBody, now); err != nil {
		t.Fatalf("valid vector rejected: %v", err)
	}
	if err := VerifyRequest("other-secret", headers(vectorTS, vectorSig), vectorBody, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong secret: %v", err)
	}
	tampered := append([]byte(nil), vectorBody...)
	tampered[len(tampered)-2] = 'X'
	if err := VerifyRequest(secret, headers(vectorTS, vectorSig), tampered, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered body: %v", err)
	}
	if err := VerifyRequest(secret, headers(vectorTS, "v1=abc"), vectorBody, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong version: %v", err)
	}
	// valid signature, but the request is six minutes old
	old := strconv.FormatInt(time.Now().Add(-6*time.Minute).Unix(), 10)
	if err := VerifyRequest(secret, headers(old, Sign(secret, old, vectorBody)), vectorBody, time.Now()); !errors.Is(err, ErrStale) {
		t.Fatalf("stale request: %v", err)
	}
	future := strconv.FormatInt(time.Now().Add(6*time.Minute).Unix(), 10)
	if err := VerifyRequest(secret, headers(future, Sign(secret, future, vectorBody)), vectorBody, time.Now()); !errors.Is(err, ErrStale) {
		t.Fatalf("future request: %v", err)
	}
}

// fixture is a handler wired to a recording runner and a fake Slack that
// records what the reply step posts.
type fixture struct {
	t       *testing.T
	h       *Handler
	d       *triggers.Dispatcher
	mu      sync.Mutex
	tasks   []triggers.Task
	posts   []map[string]any
	srv     *httptest.Server
	nowUnix int64
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, nowUnix: 1700000000}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer xoxb-test" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
			return
		}
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		f.posts = append(f.posts, m)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"1700000001.000001"}`))
	}))
	t.Cleanup(f.srv.Close)
	rec := triggers.RunnerFunc(func(_ context.Context, task triggers.Task) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tasks = append(f.tasks, task)
		return nil
	})
	f.d = &triggers.Dispatcher{Runners: map[string]triggers.Runner{triggers.KindAgent: Notifier{
		Client: f.client(), Inner: rec,
	}}, Queue: &guards.MemoryQueue{}}
	f.h = &Handler{SigningSecret: secret, BotUserID: "UBOT", Channels: []string{"C1"}, Dispatcher: f.d,
		Now: func() time.Time { return time.Unix(f.nowUnix, 0) }}
	return f
}

func (f *fixture) client() *Client {
	c := NewClient("xoxb-test")
	c.BaseURL = f.srv.URL
	return c
}

// post sends a signed request to the handler and returns status and body.
func (f *fixture) post(body string, extra ...string) (int, string) {
	f.t.Helper()
	ts := strconv.FormatInt(f.nowUnix, 10)
	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", Sign(secret, ts, []byte(body)))
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (f *fixture) runs() []triggers.Task {
	f.d.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]triggers.Task(nil), f.tasks...)
}

func mention(eventID, channel, ts, threadTS string) string {
	tt := ""
	if threadTS != "" {
		tt = `,"thread_ts":"` + threadTS + `"`
	}
	return `{"type":"event_callback","event_id":"` + eventID + `","event":{"type":"app_mention","channel":"` + channel +
		`","user":"U1","text":"<@UBOT> fix the login bug","ts":"` + ts + `"` + tt + `}}`
}

func TestURLVerification(t *testing.T) {
	f := newFixture(t)
	code, body := f.post(string(vectorBody))
	if code != http.StatusOK || !strings.Contains(body, "3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P") {
		t.Fatalf("url_verification: %d %s", code, body)
	}
	if n := len(f.runs()); n != 0 {
		t.Fatalf("challenge started %d runs", n)
	}
}

func TestSignatureRejections(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(mention("E1", "C1", "1.1", "")))
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(f.nowUnix, 10))
	req.Header.Set("X-Slack-Signature", "v0=00")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", rec.Code)
	}
	stale := f.nowUnix - 301
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(mention("E1", "C1", "1.1", "")))
	ts := strconv.FormatInt(stale, 10)
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", Sign(secret, ts, []byte(mention("E1", "C1", "1.1", ""))))
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale: %d", rec.Code)
	}
	if n := len(f.runs()); n != 0 {
		t.Fatalf("rejected requests started %d runs", n)
	}
}

func TestMentionStartsRunOnThreadKey(t *testing.T) {
	f := newFixture(t)
	if code, _ := f.post(mention("E1", "C1", "1700.2", "1700.1")); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	runs := f.runs()
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	r := runs[0]
	if want := triggers.ThreadKey(triggers.KindAgent, "slack:C1:1700.1"); r.ThreadID != want {
		t.Fatalf("thread id %s, want %s", r.ThreadID, want)
	}
	if r.Source != "slack" || r.Metadata["slack_thread_ts"] != "1700.1" || r.Metadata["slack_channel"] != "C1" {
		t.Fatalf("metadata: %+v", r)
	}
	if !strings.Contains(r.Messages[0].Text(), "fix the login bug") || strings.Contains(r.Messages[0].Text(), "<@UBOT>") {
		t.Fatalf("text not cleaned: %s", r.Messages[0].Text())
	}
	if !strings.Contains(r.Messages[0].Text(), `<untrusted_slack author="U1">`) {
		t.Fatalf("text not fenced: %s", r.Messages[0].Text())
	}
}

func TestTopLevelMentionStartsItsOwnThread(t *testing.T) {
	f := newFixture(t)
	f.post(mention("E1", "C1", "1700.5", ""))
	runs := f.runs()
	if len(runs) != 1 || runs[0].ThreadID != triggers.ThreadKey(triggers.KindAgent, "slack:C1:1700.5") {
		t.Fatalf("top-level mention: %+v", runs)
	}
}

func TestOtherChannelAndBotMessagesIgnored(t *testing.T) {
	f := newFixture(t)
	f.post(mention("E1", "C9", "1.1", ""))
	f.post(`{"type":"event_callback","event_id":"E2","event":{"type":"message","channel":"C1","user":"UBOT","text":"hi","ts":"2.1","thread_ts":"1.0"}}`)
	f.post(`{"type":"event_callback","event_id":"E3","event":{"type":"message","channel":"C1","bot_id":"B1","text":"<@UBOT> x","ts":"3.1","thread_ts":"1.0"}}`)
	if n := len(f.runs()); n != 0 {
		t.Fatalf("ignored events started %d runs", n)
	}
}

func TestThreadRepliesNeedMentionOrKnownThread(t *testing.T) {
	f := newFixture(t)
	reply := func(id, ts, text string) string {
		return `{"type":"event_callback","event_id":"` + id + `","event":{"type":"message","channel":"C1","user":"U2","text":"` + text + `","ts":"` + ts + `","thread_ts":"1700.1"}}`
	}
	// a plain reply in a thread the bot knows nothing about: ignored
	f.post(reply("R0", "1700.3", "just chatting"))
	if n := len(f.runs()); n != 0 {
		t.Fatalf("unknown thread reply started a run")
	}
	f.post(mention("E1", "C1", "1700.2", "1700.1"))
	// now the thread is known, so a plain reply is a follow-up of the same thread
	_, body := f.post(reply("R1", "1700.4", "and also the tests"))
	if !strings.Contains(body, `"dispatched"`) && !strings.Contains(body, `"queued"`) {
		t.Fatalf("follow-up not dispatched: %s", body)
	}
	runs := f.runs()
	if len(runs) < 1 {
		t.Fatal("no runs")
	}
	for _, r := range runs {
		if r.ThreadID != triggers.ThreadKey(triggers.KindAgent, "slack:C1:1700.1") {
			t.Fatalf("run on wrong thread: %s", r.ThreadID)
		}
	}
}

func TestRedeliveryAndDuplicateMessageRunOnce(t *testing.T) {
	f := newFixture(t)
	body := mention("E1", "C1", "1700.2", "1700.1")
	f.post(body)
	// Slack retries an unacknowledged delivery with the same event_id
	_, resp := f.post(body, "X-Slack-Retry-Num", "1", "X-Slack-Retry-Reason", "http_timeout")
	if !strings.Contains(resp, "duplicate") {
		t.Fatalf("retry not recognised: %s", resp)
	}
	// app_mention and message for the same message arrive with different event ids
	f.post(`{"type":"event_callback","event_id":"E9","event":{"type":"message","channel":"C1","user":"U1","text":"<@UBOT> fix the login bug","ts":"1700.2","thread_ts":"1700.1"}}`)
	if n := len(f.runs()); n != 1 {
		t.Fatalf("want 1 run for the duplicated mention, got %d", n)
	}
}

func TestReplyPostedWhenRunFinishes(t *testing.T) {
	f := newFixture(t)
	f.post(mention("E1", "C1", "1700.2", "1700.1"))
	f.runs()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 1 {
		t.Fatalf("want 1 reply, got %d", len(f.posts))
	}
	p := f.posts[0]
	if p["channel"] != "C1" || p["thread_ts"] != "1700.1" || p["text"] != "Run finished." {
		t.Fatalf("reply: %+v", p)
	}
}

func TestNotifierPostsFinalAnswer(t *testing.T) {
	f := newFixture(t)
	n := Notifier{Client: f.client(), Inner: triggers.RunnerFunc(func(ctx context.Context, _ triggers.Task) error {
		triggers.SetResult(ctx, "The login handler already rejects empty users.")
		return nil
	})}
	if err := n.Run(context.Background(), triggers.Task{Metadata: map[string]any{"slack_channel": "C1", "slack_thread_ts": "1.1"}}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 1 || f.posts[0]["text"] != "The login handler already rejects empty users." {
		t.Fatalf("answer not posted: %+v", f.posts)
	}
}

func TestNotifierReportsFailure(t *testing.T) {
	f := newFixture(t)
	n := Notifier{Client: f.client(), Inner: triggers.RunnerFunc(func(context.Context, triggers.Task) error {
		return errors.New("sandbox unreachable")
	})}
	err := n.Run(context.Background(), triggers.Task{Metadata: map[string]any{"slack_channel": "C1", "slack_thread_ts": "1.1"}})
	if err == nil || !strings.Contains(err.Error(), "sandbox unreachable") {
		t.Fatalf("run error lost: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 1 || !strings.Contains(f.posts[0]["text"].(string), "The run failed: sandbox unreachable") {
		t.Fatalf("failure not reported: %+v", f.posts)
	}
}

func TestAPIErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(b), "C404"):
			_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
		default:
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
		}
	}))
	defer srv.Close()
	c := NewClient("xoxb-test")
	c.BaseURL = srv.URL
	_, err := c.PostMessage(context.Background(), "C404", "x", "", nil)
	if !IsCode(err, "channel_not_found") {
		t.Fatalf("slack error code: %v", err)
	}
	err = c.AddReaction(context.Background(), "C1", "1.1", "eyes")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 429 || ae.RetryAfter != 7*time.Second {
		t.Fatalf("rate limit: %#v", err)
	}
}

func TestNeutralizeBroadcast(t *testing.T) {
	got := NeutralizeBroadcast("<!channel> ping <@U1> and <https://example.com|docs>")
	if got != "&lt;!channel> ping &lt;@U1> and <https://example.com|docs>" {
		t.Fatalf("got %q", got)
	}
}
