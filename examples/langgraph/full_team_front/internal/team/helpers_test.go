package team

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, h http.Handler) *httptest.Server {
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}
