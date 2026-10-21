package compress

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// serveJSON answers every request with body and records the last request.
func serveJSON(t *testing.T, status int, body string, seen *http.Request, seenBody *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r
		}
		if seenBody != nil {
			b, _ := io.ReadAll(r.Body)
			*seenBody = b
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func TestHTTPDeciderNoulAndChoice(t *testing.T) {
	var req http.Request
	var raw []byte
	srv := serveJSON(t, 200, `{"model":"multilingual",
		"answers":{
			"compress_history":{"type":"noul","noul":true,"confidence":0.9},
			"level":{"type":"choice","choice":"full","probabilities":{"lite":0.1,"full":0.8,"ultra":0.1},"confidence":0.8}
		},"usage":{},"routing":{}}`, &req, &raw)
	defer srv.Close()

	d := HTTPDecider{URL: srv.URL + "/decide", APIKey: "secret"}
	qs := []Question{
		{ID: QCompressHistory, Kind: KindNoul, Text: "Compress the older turns?"},
		{ID: QLevel, Kind: KindChoice, Text: "Caveman level.", Options: []string{"lite", "full", "ultra"}},
	}
	st := State{"tokens": 5000}
	got, err := d.Decide(context.Background(), st, qs)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if req.URL.Path != "/decide" {
		t.Errorf("path = %s, want /decide", req.URL.Path)
	}
	if h := req.Header.Get("Authorization"); h != "Bearer secret" {
		t.Errorf("Authorization = %q", h)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if _, ok := body["model"]; ok {
		t.Errorf("model must be omitted when zero: %s", raw)
	}
	if _, ok := body["max_len"]; ok {
		t.Errorf("max_len must be omitted when zero: %s", raw)
	}
	if body["state"].(map[string]any)["tokens"].(float64) != 5000 {
		t.Errorf("state not sent: %s", raw)
	}
	qmap := body["questions"].(map[string]any)
	noul := qmap[QCompressHistory].(map[string]any)
	if noul["type"] != "noul" || noul["instructions"] != "Compress the older turns?" {
		t.Errorf("noul question = %v", noul)
	}
	choice := qmap[QLevel].(map[string]any)
	if choice["type"] != "choice" || choice["instructions"] != "Caveman level." {
		t.Errorf("choice question = %v", choice)
	}
	crit := choice["criteria"].(map[string]any)
	if len(crit) != 3 || crit["full"] != "full" {
		t.Errorf("criteria = %v, want option->option for each option", crit)
	}

	if len(got) != 2 {
		t.Fatalf("got %d answers, want 2", len(got))
	}
	if got[0].ID != QCompressHistory || got[0].Kind != KindNoul || !got[0].True || got[0].P != 0.9 {
		t.Errorf("noul answer = %+v", got[0])
	}
	if got[1].ID != QLevel || got[1].Kind != KindChoice || got[1].Pick != "full" || got[1].Dist["full"] != 0.8 {
		t.Errorf("choice answer = %+v", got[1])
	}
}

func TestHTTPDeciderNoulBoolNoConfidenceIsNaN(t *testing.T) {
	srv := serveJSON(t, 200, `{"answers":{"compress_history":{"type":"noul","noul":true}}}`, nil, nil)
	defer srv.Close()

	got, err := HTTPDecider{URL: srv.URL}.Decide(context.Background(), State{}, HistoryQuestions()[:1])
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !math.IsNaN(got[0].P) {
		t.Errorf("P = %v, want NaN", got[0].P)
	}
	if !got[0].True {
		t.Errorf("True = false, want the bool value true")
	}
	if got[0].Confident(0.7) {
		t.Errorf("NaN answer must not be confident")
	}
}

func TestHTTPDeciderNoulFloat(t *testing.T) {
	srv := serveJSON(t, 200, `{"answers":{"compress_history":{"type":"noul","noul":0.8}}}`, nil, nil)
	defer srv.Close()
	got, err := HTTPDecider{URL: srv.URL}.Decide(context.Background(), State{}, HistoryQuestions()[:1])
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got[0].P != 0.8 || !got[0].True {
		t.Errorf("float noul = %+v, want P 0.8 True", got[0])
	}

	// A bool false with confidence 0.9 means P(true) = 0.1.
	srv2 := serveJSON(t, 200, `{"answers":{"compress_history":{"type":"noul","noul":false,"confidence":0.9}}}`, nil, nil)
	defer srv2.Close()
	got, err = HTTPDecider{URL: srv2.URL}.Decide(context.Background(), State{}, HistoryQuestions()[:1])
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if math.Abs(got[0].P-0.1) > 1e-9 || got[0].True {
		t.Errorf("bool false noul = %+v, want P 0.1 False", got[0])
	}
}

func TestHTTPDeciderUsesFixtures(t *testing.T) {
	noulBody, err := os.ReadFile("testdata/systemone_noul.json")
	if err != nil {
		t.Fatal(err)
	}
	choiceBody, err := os.ReadFile("testdata/systemone_choice.json")
	if err != nil {
		t.Fatal(err)
	}

	srv := serveJSON(t, 200, string(noulBody), nil, nil)
	defer srv.Close()
	got, err := HTTPDecider{URL: srv.URL}.Decide(context.Background(), State{}, HistoryQuestions()[:1])
	if err != nil {
		t.Fatalf("noul fixture: %v", err)
	}
	if got[0].Kind != KindNoul || got[0].ID != QCompressHistory {
		t.Errorf("noul fixture answer = %+v", got[0])
	}

	srv2 := serveJSON(t, 200, string(choiceBody), nil, nil)
	defer srv2.Close()
	got, err = HTTPDecider{URL: srv2.URL}.Decide(context.Background(), State{}, HistoryQuestions()[1:2])
	if err != nil {
		t.Fatalf("choice fixture: %v", err)
	}
	if got[0].Kind != KindChoice || got[0].Pick == "" {
		t.Errorf("choice fixture answer = %+v", got[0])
	}
}

func TestHTTPDeciderStatusError(t *testing.T) {
	srv := serveJSON(t, 500, `{"detail":"boom"}`, nil, nil)
	defer srv.Close()
	_, err := HTTPDecider{URL: srv.URL}.Decide(context.Background(), State{}, HistoryQuestions()[:1])
	if err == nil {
		t.Fatal("want error on 500")
	}
}

func TestHTTPDeciderBadJSON(t *testing.T) {
	srv := serveJSON(t, 200, `not json`, nil, nil)
	defer srv.Close()
	_, err := HTTPDecider{URL: srv.URL}.Decide(context.Background(), State{}, HistoryQuestions()[:1])
	if err == nil {
		t.Fatal("want error on bad JSON")
	}
}

func TestHTTPDeciderMissingAnswer(t *testing.T) {
	srv := serveJSON(t, 200, `{"answers":{"level":{"type":"choice","choice":"full","probabilities":{"full":1}}}}`, nil, nil)
	defer srv.Close()
	_, err := HTTPDecider{URL: srv.URL}.Decide(context.Background(), State{}, HistoryQuestions()[:1])
	if err == nil {
		t.Fatal("want error when an answer id is absent")
	}
}

func TestHTTPDeciderContextCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := HTTPDecider{URL: srv.URL}.Decide(ctx, State{}, HistoryQuestions()[:1])
	if err == nil {
		t.Fatal("want error on cancelled context")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("Decide did not return promptly after cancel: %v", time.Since(start))
	}
}

func TestGuardedWithHTTPDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/decide"
	srv.Close()

	st := State{"prev_output_bytes": 5000, "command": "go test ./..."}
	st["tokens"] = 5000.0
	qs := append(HistoryQuestions(), ShellQuestion())
	g := NewGuarded(HTTPDecider{URL: url})
	got, err := g.Decide(context.Background(), st, qs)
	if err != nil {
		t.Fatalf("Guarded with decider down: %v", err)
	}
	want, err := RuleDecider{}.Decide(context.Background(), st, qs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d answers, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].True != want[i].True || got[i].Pick != want[i].Pick {
			t.Errorf("answer %d = %+v, want rule answer %+v", i, got[i], want[i])
		}
	}
}
