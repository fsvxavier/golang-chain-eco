package dev

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func TestServeRejectsNilGraph(t *testing.T) {
	err := Serve("127.0.0.1:0", map[string]*graph.Compiled{"a": nil})
	if err == nil || !strings.Contains(err.Error(), `"a"`) {
		t.Fatalf("want nil-graph error, got %v", err)
	}
}

func TestLogRequestsRecordsStatus(t *testing.T) {
	var buf bytes.Buffer
	h := logRequests(log.New(&buf, "", 0), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusTeapot || !strings.Contains(buf.String(), "GET /x 418") {
		t.Fatalf("code=%d log=%q", rec.Code, buf.String())
	}
}
