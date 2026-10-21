// decider_laya_http: asks a laya-serve endpoint (POST /v1/systemone) for the
// history and shell answers through compress.HTTPDecider and NewGuarded.
// Offline it starts an in-process fake server; with LAYA_URL it uses the real one.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

func main() {
	ctx := context.Background()

	endpoint := os.Getenv("LAYA_URL")
	if endpoint == "" {
		srv := httptest.NewServer(http.HandlerFunc(fakeSystemOne))
		defer srv.Close()
		endpoint = srv.URL + "/v1/systemone"
		fmt.Println("LAYA_URL not set: using the in-process fake server")
	} else {
		fmt.Println("using LAYA_URL:", endpoint)
	}

	inner := compress.HTTPDecider{
		URL:    endpoint,
		APIKey: os.Getenv("LAYA_API_KEY"),
		Model:  os.Getenv("LAYA_MODEL"),
	}
	g := compress.NewGuarded(inner)

	msgs := []messages.Message{
		messages.Human("Please check the build and tell me what fails."),
		messages.AI("Running the tests now."),
		messages.Human("Thanks, and summarise the result briefly."),
	}
	st := compress.Summarize(msgs, compress.SummaryOptions{}).State()
	answers, err := g.Decide(ctx, st, compress.HistoryQuestions())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("== history questions ==")
	printAnswers(answers)

	shell, err := g.Decide(ctx, compress.ShellState("go test ./...", 5000), []compress.Question{compress.ShellQuestion()})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("== shell question ==")
	printAnswers(shell)
}

func printAnswers(answers []compress.Answer) {
	for _, a := range answers {
		switch a.Kind {
		case compress.KindChoice:
			fmt.Printf("%-16s choice %s (p=%.2f)\n", a.ID, a.Pick, a.Dist[a.Pick])
		default:
			fmt.Printf("%-16s noul   %v (p=%.2f)\n", a.ID, a.True, a.P)
		}
	}
}

// fakeSystemOne answers every question in the request with a plausible reply
// in the systemone shape, so the example runs without laya-serve.
func fakeSystemOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Questions map[string]struct {
			Type     string            `json:"type"`
			Criteria map[string]string `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	answers := make(map[string]any, len(req.Questions))
	for id, q := range req.Questions {
		if q.Type == "choice" {
			probs := map[string]float64{}
			for k := range q.Criteria {
				probs[k] = 0.05
			}
			probs["full"] = 0.9
			answers[id] = map[string]any{"type": "choice", "choice": "full", "probabilities": probs, "confidence": 0.9}
			continue
		}
		answers[id] = map[string]any{"type": "noul", "noul": true, "confidence": 0.91}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"model": "fake", "answers": answers, "usage": map[string]any{}, "routing": map[string]any{}})
}
