// Package fakelinear is an in-process Linear GraphQL API for the Open SWE
// examples and tests: issue lookup and commentCreate, with API-key checks and
// a record of the comments written. The client it hands out is the real
// openswe/linear client pointed at the fake.
package fakelinear

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/openswe/linear"
)

// Issue is an issue the fake knows about.
type Issue struct {
	ID, Identifier, Title, Description, URL string
}

// Comment is a comment written through commentCreate.
type Comment struct {
	ID, IssueID, Body string
}

// Linear is the fake server; its fields are guarded by Mu.
type Linear struct {
	Mu       sync.Mutex
	Server   *httptest.Server
	APIKey   string
	Issues   map[string]Issue
	Comments []Comment
	seq      int
}

// New starts a fake that accepts the given API key.
func New(apiKey string) *Linear {
	l := &Linear{APIKey: apiKey, Issues: map[string]Issue{}}
	l.Server = httptest.NewServer(http.HandlerFunc(l.serve))
	return l
}

// Client returns the real Linear client pointed at the fake.
func (l *Linear) Client() *linear.Client {
	c := linear.NewClient(l.APIKey)
	c.Endpoint = l.Server.URL
	return c
}

// Close stops the server.
func (l *Linear) Close() { l.Server.Close() }

// AddIssue registers an issue.
func (l *Linear) AddIssue(is Issue) {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	l.Issues[is.ID] = is
}

func gqlError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"message": msg}}})
}

func (l *Linear) serve(w http.ResponseWriter, r *http.Request) {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	if r.Header.Get("Authorization") != l.APIKey {
		gqlError(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		gqlError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	data := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"data": v}) }
	switch {
	case strings.Contains(req.Query, "commentCreate"):
		in, _ := req.Variables["input"].(map[string]any)
		issueID, _ := in["issueId"].(string)
		body, _ := in["body"].(string)
		if _, ok := l.Issues[issueID]; !ok {
			gqlError(w, http.StatusOK, "Entity not found: Issue")
			return
		}
		l.seq++
		c := Comment{ID: fmt.Sprintf("comment-%d", l.seq), IssueID: issueID, Body: body}
		l.Comments = append(l.Comments, c)
		data(map[string]any{"commentCreate": map[string]any{"success": true, "comment": map[string]any{"id": c.ID}}})
	case strings.Contains(req.Query, "issue(id"):
		id, _ := req.Variables["id"].(string)
		is, ok := l.Issues[id]
		if !ok {
			data(map[string]any{"issue": nil})
			return
		}
		data(map[string]any{"issue": map[string]any{"id": is.ID, "identifier": is.Identifier, "title": is.Title, "description": is.Description, "url": is.URL}})
	default:
		gqlError(w, http.StatusOK, "unsupported operation in fakelinear")
	}
}
