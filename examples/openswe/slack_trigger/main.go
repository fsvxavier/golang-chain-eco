// slack_trigger: Open SWE started from Slack, offline. A signed Events API
// delivery (url_verification, then an app_mention in a thread, then Slack's
// retry of the same event) reaches the handler: the mention starts one run on
// the Slack thread's openswe thread, the retry is ignored, and when the run
// finishes the outcome is posted back into the thread. A fake Slack and a
// fake GitHub stand in for the real services; a scripted model answers.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/fakegithub"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/fakeslack"
	"github.com/fsvxavier/golang-chain-eco/langgraph/checkpoint"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe"
	"github.com/fsvxavier/golang-chain-eco/openswe/guards"
	"github.com/fsvxavier/golang-chain-eco/openswe/sandbox"
	"github.com/fsvxavier/golang-chain-eco/openswe/slack"
	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

const (
	signingSecret = "demo-signing-secret"
	botToken      = "xoxb-demo"
	botUser       = "UOPENSWE"
	codeChannel   = "C0CODE"
)

// post sends a signed Events API request to the handler.
func post(url, body string, retry bool) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", slack.Sign(signingSecret, ts, []byte(body)))
	if retry {
		req.Header.Set("X-Slack-Retry-Num", "1")
		req.Header.Set("X-Slack-Retry-Reason", "http_timeout")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return fmt.Sprintf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

func main() {
	gh := fakegithub.New("acme", "api")
	defer gh.Close()
	sl := fakeslack.New(botToken)
	defer sl.Close()

	base, _ := os.MkdirTemp("", "openswe-slack-")
	defer os.RemoveAll(base)
	sb := &sandbox.Manager{Providers: map[string]sandbox.Provider{"local": sandbox.Local{Base: base}}, Default: "local", Store: store.NewMemory()}
	links := &openswe.PRLinks{S: store.NewMemory()}

	// the scripted engineer answers the question without touching the repo
	model := &models.Fake{Responses: []messages.Message{
		messages.AI("The login handler already rejects empty users (api/auth/login.go); no change needed."),
	}}
	agent, err := openswe.NewAgent(openswe.Config{
		Model: model, GitHub: gh.Client(), Sandbox: sb, Links: links, Checkpointer: checkpoint.NewMemory(),
		DraftPRs: true, AllowedOrgs: []string{"acme"}, DefaultRepo: "acme/api",
	})
	if err != nil {
		log.Fatal(err)
	}

	d := &triggers.Dispatcher{
		Runners: map[string]triggers.Runner{triggers.KindAgent: slack.Notifier{
			Client: sl.Client(),
			Inner:  openswe.AgentRunner{Graph: agent},
			// no Describe: the run's final answer is posted as the reply

		}},
		Queue:   &guards.MemoryQueue{},
		OnError: func(t triggers.Task, err error) { log.Printf("run %q failed: %v", t.Reason, err) },
	}
	h := &slack.Handler{SigningSecret: signingSecret, BotUserID: botUser, Channels: []string{codeChannel}, Dispatcher: d}
	srv := httptest.NewServer(h)
	defer srv.Close()

	demo.Title("Slack url_verification handshake")
	fmt.Println(post(srv.URL, `{"type":"url_verification","challenge":"demo-challenge"}`, false))

	demo.Title("app_mention in #code, starting a thread")
	mention := `{"type":"event_callback","event_id":"Ev1","team_id":"T1","event":{"type":"app_mention","user":"U42","channel":"` + codeChannel +
		`","text":"<@` + botUser + `> does Login handle an empty user?","ts":"1700000100.000200","thread_ts":"1700000100.000100"}}`
	fmt.Println(post(srv.URL, mention, false))
	fmt.Println("Slack retries the same event with X-Slack-Retry-Num:")
	fmt.Println(post(srv.URL, mention, true))
	d.Wait()

	demo.Title("what the agent was given")
	sys := model.Calls[0][0].Text()
	fmt.Println("system prompt mentions the Slack source:", strings.Contains(sys, "Slack"))
	fmt.Println("user message is fenced as untrusted Slack data:", strings.Contains(model.Calls[0][len(model.Calls[0])-1].Text(), `<untrusted_slack author="U42">`))

	demo.Title("what was posted back to Slack")
	sl.Mu.Lock()
	defer sl.Mu.Unlock()
	for _, p := range sl.Posts {
		fmt.Printf("channel=%s thread_ts=%s\n%s\n", p.Channel, p.ThreadTS, p.Text)
	}
	fmt.Println("posts:", len(sl.Posts), "(one run, one reply; the retry started nothing)")
	fmt.Println("thread key:", triggers.ThreadKey(triggers.KindAgent, "slack:"+codeChannel+":1700000100.000100"))
}
