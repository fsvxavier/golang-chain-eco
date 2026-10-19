package ciam_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/ciam"
)

func TestHTTPAPI(t *testing.T) {
	ctx := context.Background()
	root := djangoProject(t)
	m := newManager()
	if _, err := m.Index(ctx, root, ciam.IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	const key = "k3y"
	srv := httptest.NewServer(ciam.Toolkit{M: m, Project: root}.Handler(key))
	t.Cleanup(srv.Close)

	do := func(method, path, apiKey, body string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if apiKey != "" {
			req.Header.Set("X-Api-Key", apiKey)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp, buf.Bytes()
	}

	if resp, _ := do("POST", "/tools/ciam_search", "", `{"query":"invoice"}`); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong/missing key: got %d, want 401", resp.StatusCode)
	}
	if resp, _ := do("POST", "/tools/ciam_search", "wrong", `{"query":"invoice"}`); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key: got %d, want 401", resp.StatusCode)
	}
	if resp, _ := do("GET", "/healthz", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz must not need a key: %d", resp.StatusCode)
	}

	resp, body := do("POST", "/tools/ciam_search", key, `{"query":"invoice total cents"}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "billing/models.py") {
		t.Fatalf("ciam_search: %d %s", resp.StatusCode, body)
	}

	if resp, body := do("GET", "/status", key, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, body)
	} else {
		var st ciam.Status
		if err := json.Unmarshal(body, &st); err != nil || len(st.Projects) != 1 || st.Searches < 1 {
			t.Fatalf("status counters: %v %s", err, body)
		}
	}

	if resp, _ := do("POST", "/tools/ciam_nope", key, `{}`); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown tool: got %d, want 404", resp.StatusCode)
	}
	if resp, _ := do("POST", "/tools/ciam_search", key, `{"query":42}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad args: got %d, want 400", resp.StatusCode)
	}
	if resp, body := do("GET", "/tools", key, ""); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ciam_search") {
		t.Fatalf("list tools: %d %s", resp.StatusCode, body)
	}
}
