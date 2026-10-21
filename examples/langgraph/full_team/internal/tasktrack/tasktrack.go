// Package tasktrack gives the engineering examples one vocabulary for where a
// task is and who has it, and tells people when it moves: on the console, in
// Slack, by email or in a chat webhook.
//
// Moves are checked against a table, so a task cannot jump from development to
// "committed" without passing review and QA.
package tasktrack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Status is a stage of the delivery.
type Status string

const (
	Received        Status = "received"
	InSpecification Status = "in_specification"
	RiskSecReview   Status = "risk_sec_review"
	TaskAssignment  Status = "task_assignment"
	InDevelopment   Status = "in_development"
	InCodeReview    Status = "in_code_review"
	InQATesting     Status = "in_qa_testing"
	QAApproved      Status = "qa_approved"
	QARejected      Status = "qa_rejected"
	ReviewApproved  Status = "review_approved"
	ReviewRejected  Status = "review_rejected"
	Committed       Status = "committed"
	Completed       Status = "completed"
)

// Kind tells which flow a subject follows.
type Kind string

const (
	Request Kind = "request" // the whole ask: received .. task_assignment .. completed
	Task    Kind = "task"    // one unit of work: task_assignment .. completed
)

// flows lists, per kind, the moves allowed from each status ("" = creation).
var flows = map[Kind]map[Status][]Status{
	Request: {
		"":              {Received},
		Received:        {InSpecification},
		InSpecification: {RiskSecReview},
		RiskSecReview:   {TaskAssignment},
		TaskAssignment:  {Completed},
	},
	Task: {
		"":             {TaskAssignment},
		TaskAssignment: {InDevelopment},
		InDevelopment:  {InCodeReview},
		InCodeReview:   {ReviewApproved, ReviewRejected},
		ReviewRejected: {InDevelopment}, // back to the engineer, with feedback
		ReviewApproved: {InQATesting},
		InQATesting:    {QAApproved, QARejected},
		QARejected:     {InDevelopment},
		QAApproved:     {Committed},
		Committed:      {Completed},
	},
}

// Event is one move.
type Event struct {
	Time    time.Time `json:"time"`
	Kind    Kind      `json:"kind"`
	Subject string    `json:"subject"` // request id or task id
	From    Status    `json:"from,omitempty"`
	To      Status    `json:"to"`
	Agent   string    `json:"agent"`  // who holds the work now
	Detail  string    `json:"detail"` // what happened, e.g. the feedback of a rejection
}

// String is the one-line form used by the console and by chat messages.
func (e Event) String() string {
	from := string(e.From)
	if from == "" {
		from = "new"
	}
	s := fmt.Sprintf("[%s] %s -> %s", e.Subject, from, e.To)
	if e.Agent != "" {
		s += " (" + e.Agent + ")"
	}
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

// Notifier receives every event the tracker accepts.
type Notifier interface {
	Notify(ctx context.Context, e Event) error
}

// Tracker holds the current status of every subject and the history of moves.
// A failing notifier never blocks the work: its error goes to OnError.
type Tracker struct {
	OnError func(error)

	mu        sync.Mutex
	current   map[string]Status
	history   []Event
	notifiers []Notifier
}

// New returns a tracker that notifies ns.
func New(ns ...Notifier) *Tracker {
	return &Tracker{current: map[string]Status{}, notifiers: ns, OnError: func(error) {}}
}

// Move sends a subject to a new status. It fails when the flow does not allow
// the move; nothing is recorded or notified then.
func (t *Tracker) Move(ctx context.Context, kind Kind, subject string, to Status, agent, detail string) error {
	t.mu.Lock()
	from := t.current[subject]
	if !slices.Contains(flows[kind][from], to) {
		t.mu.Unlock()
		return fmt.Errorf("tasktrack: %s %s cannot go from %q to %q", kind, subject, from, to)
	}
	e := Event{Time: time.Now(), Kind: kind, Subject: subject, From: from, To: to, Agent: agent, Detail: detail}
	t.current[subject] = to
	t.history = append(t.history, e)
	t.mu.Unlock()
	for _, n := range t.notifiers {
		if err := n.Notify(ctx, e); err != nil {
			t.OnError(fmt.Errorf("tasktrack: notify %T: %w", n, err))
		}
	}
	return nil
}

// Status returns the current status of a subject ("" when unknown).
func (t *Tracker) Status(subject string) Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current[subject]
}

// Snapshot returns the current status of every subject.
func (t *Tracker) Snapshot() map[string]Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]Status, len(t.current))
	for k, v := range t.current {
		out[k] = v
	}
	return out
}

// History returns the moves in order.
func (t *Tracker) History() []Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.history)
}

// ---------------------------------------------------------------- notifiers

// Console prints every event to w.
type Console struct{ W io.Writer }

// Notify implements Notifier.
func (c Console) Notify(_ context.Context, e Event) error {
	_, err := fmt.Fprintf(c.W, "status %s\n", e)
	return err
}

// Filter passes to N only the events whose new status is in Only (all when empty).
type Filter struct {
	N    Notifier
	Only []Status
}

// Notify implements Notifier.
func (f Filter) Notify(ctx context.Context, e Event) error {
	if len(f.Only) > 0 && !slices.Contains(f.Only, e.To) {
		return nil
	}
	return f.N.Notify(ctx, e)
}

// webhook POSTs a JSON body built from the event.
type webhook struct {
	url  string
	body func(Event) any
	http *http.Client
}

func (w webhook) Notify(ctx context.Context, e Event) error {
	b, err := json.Marshal(w.body(e))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}

// Slack posts to a Slack incoming webhook.
func Slack(webhookURL string) Notifier {
	return webhook{url: webhookURL, http: &http.Client{Timeout: 10 * time.Second},
		body: func(e Event) any { return map[string]string{"text": ":clipboard: " + e.String()} }}
}

// Chat posts the event as JSON ({"text": "...", "event": {...}}) to any chat
// or automation endpoint that accepts a webhook (Mattermost, Teams, Discord
// relays, your own service).
func Chat(webhookURL string) Notifier {
	return webhook{url: webhookURL, http: &http.Client{Timeout: 10 * time.Second},
		body: func(e Event) any { return map[string]any{"text": e.String(), "event": e} }}
}

// SMTP sends one email per event.
type SMTP struct {
	Addr, User, Pass string // Addr is host:port; User and Pass may be empty
	From             string
	To               []string
}

// Message builds the RFC 5322 message for e.
func (s SMTP) Message(e Event) []byte {
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: [%s] %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		s.From, strings.Join(s.To, ", "), e.Subject, e.To, e))
}

// Notify implements Notifier.
func (s SMTP) Notify(_ context.Context, e Event) error {
	var auth smtp.Auth
	if s.User != "" {
		host, _, _ := strings.Cut(s.Addr, ":")
		auth = smtp.PlainAuth("", s.User, s.Pass, host)
	}
	return smtp.SendMail(s.Addr, auth, s.From, s.To, s.Message(e))
}

// FromEnv builds the tracker's notifiers from the environment.
//
//	NOTIFY            comma list of console, slack, email, chat (default console)
//	NOTIFY_STATUSES   statuses that go to slack, email and chat (default all; the console always shows all)
//	SLACK_WEBHOOK_URL, CHAT_WEBHOOK_URL
//	SMTP_ADDR (host:port), SMTP_USER, SMTP_PASS, MAIL_FROM, MAIL_TO (comma list)
func FromEnv(console io.Writer) (*Tracker, error) {
	var only []Status
	for _, s := range strings.Split(os.Getenv("NOTIFY_STATUSES"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			only = append(only, Status(s))
		}
	}
	list := os.Getenv("NOTIFY")
	if list == "" {
		list = "console"
	}
	var missing []string // reported together, so one run fixes every variable
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			missing = append(missing, k)
		}
		return v
	}
	var ns []Notifier
	for _, name := range strings.Split(list, ",") {
		var n Notifier
		switch name = strings.TrimSpace(name); name {
		case "console":
			ns = append(ns, Console{W: console})
			continue
		case "slack":
			n = Slack(need("SLACK_WEBHOOK_URL"))
		case "chat":
			n = Chat(need("CHAT_WEBHOOK_URL"))
		case "email":
			n = SMTP{Addr: need("SMTP_ADDR"), User: os.Getenv("SMTP_USER"), Pass: os.Getenv("SMTP_PASS"),
				From: need("MAIL_FROM"), To: strings.Split(need("MAIL_TO"), ",")}
		case "":
			continue
		default:
			return nil, fmt.Errorf("tasktrack: unknown NOTIFY target %q (console, slack, email, chat)", name)
		}
		ns = append(ns, Filter{N: n, Only: only})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("tasktrack: set %s", strings.Join(missing, ", "))
	}
	return New(ns...), nil
}
