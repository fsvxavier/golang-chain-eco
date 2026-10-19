// Package fakeslack is an in-process Slack Web API for the Open SWE examples
// and tests: chat.postMessage, chat.update and reactions.add, with bot-token
// checks and a record of what was posted. The client it hands out is the real
// openswe/slack client pointed at the fake.
package fakeslack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/openswe/slack"
)

// Post is one message the bot sent (or updated).
type Post struct {
	Channel  string
	TS       string
	ThreadTS string
	Text     string
	Blocks   []any
}

// Reaction is one reactions.add call.
type Reaction struct {
	Channel, TS, Name string
}

// Slack is the fake server; its fields are guarded by Mu.
type Slack struct {
	Mu        sync.Mutex
	Server    *httptest.Server
	Token     string
	Posts     []Post
	Updates   []Post
	Reactions []Reaction
	seq       int
}

// New starts a fake that accepts the given bot token.
func New(token string) *Slack {
	s := &Slack{Token: token}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Client returns the real Slack client pointed at the fake.
func (s *Slack) Client() *slack.Client {
	c := slack.NewClient(s.Token)
	c.BaseURL = s.Server.URL
	return c
}

// Close stops the server.
func (s *Slack) Close() { s.Server.Close() }

func (s *Slack) ts() string {
	s.seq++
	return fmt.Sprintf("1700000000.%06d", s.seq)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Slack) serve(w http.ResponseWriter, r *http.Request) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	if r.Header.Get("Authorization") != "Bearer "+s.Token {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid_auth"})
		return
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid_json"})
		return
	}
	str := func(k string) string { v, _ := req[k].(string); return v }
	blocks, _ := req["blocks"].([]any)
	switch strings.TrimPrefix(r.URL.Path, "/") {
	case "chat.postMessage":
		if str("channel") == "" {
			writeJSON(w, map[string]any{"ok": false, "error": "channel_not_found"})
			return
		}
		p := Post{Channel: str("channel"), TS: s.ts(), ThreadTS: str("thread_ts"), Text: str("text"), Blocks: blocks}
		s.Posts = append(s.Posts, p)
		writeJSON(w, map[string]any{"ok": true, "channel": p.Channel, "ts": p.TS})
	case "chat.update":
		p := Post{Channel: str("channel"), TS: str("ts"), Text: str("text"), Blocks: blocks}
		s.Updates = append(s.Updates, p)
		writeJSON(w, map[string]any{"ok": true, "channel": p.Channel, "ts": p.TS})
	case "reactions.add":
		for _, rc := range s.Reactions {
			if rc.Channel == str("channel") && rc.TS == str("timestamp") && rc.Name == str("name") {
				writeJSON(w, map[string]any{"ok": false, "error": "already_reacted"})
				return
			}
		}
		s.Reactions = append(s.Reactions, Reaction{Channel: str("channel"), TS: str("timestamp"), Name: str("name")})
		writeJSON(w, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"ok": false, "error": "unknown_method"})
	}
}
