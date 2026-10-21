package compress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// DefaultHTTPDeciderURL is the laya-serve endpoint used when HTTPDecider.URL is empty.
const DefaultHTTPDeciderURL = "http://localhost:8000/v1/systemone"

// defaultHTTPClient bounds calls made without a Client, so a hung endpoint
// cannot block a model call even when the decider is used without Guarded.
var defaultHTTPClient = &http.Client{Timeout: 10 * time.Second}

// HTTPDecider asks a laya-serve / TypeSafe Jev endpoint (POST <URL>) to answer
// the questions. Model and MaxLen are omitted from the request when zero, so
// the server defaults apply. Wrap it in NewGuarded so a failing endpoint falls
// back to the rules.
type HTTPDecider struct {
	URL    string // full endpoint, e.g. http://localhost:8099/decide
	APIKey string // sent as "Authorization: Bearer <APIKey>" when set
	Model  string
	MaxLen int
	Client *http.Client // nil uses a client with a 10s timeout; ctx bounds each call
}

type httpQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

type httpRequest struct {
	State     State                   `json:"state"`
	Questions map[string]httpQuestion `json:"questions"`
	Model     string                  `json:"model,omitempty"`
	MaxLen    int                     `json:"max_len,omitempty"`
}

type httpResponse struct {
	Answers map[string]httpAnswer `json:"answers"`
}

type httpAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Noul          json.RawMessage    `json:"noul"`
	Confidence    *float64           `json:"confidence"`
}

// Decide implements Decider.
func (d HTTPDecider) Decide(ctx context.Context, st State, qs []Question) ([]Answer, error) {
	if len(qs) == 0 {
		return nil, nil
	}
	req := httpRequest{State: st, Questions: make(map[string]httpQuestion, len(qs)), Model: d.Model, MaxLen: d.MaxLen}
	for _, q := range qs {
		hq := httpQuestion{Instructions: q.Text}
		if q.Kind == KindChoice {
			hq.Type = "choice"
			hq.Criteria = make(map[string]string, len(q.Options))
			for _, o := range q.Options {
				hq.Criteria[o] = o
			}
		} else {
			hq.Type = "noul"
		}
		req.Questions[q.ID] = hq
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := d.URL
	if url == "" {
		url = DefaultHTTPDeciderURL
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if d.APIKey != "" {
		hr.Header.Set("Authorization", "Bearer "+d.APIKey)
	}
	client := d.Client
	if client == nil {
		client = defaultHTTPClient
	}
	resp, err := client.Do(hr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("compress: decider returned %s: %.200s", resp.Status, body)
	}

	var out httpResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("compress: decider response: %w", err)
	}

	answers := make([]Answer, 0, len(qs))
	for _, q := range qs {
		ha, ok := out.Answers[q.ID]
		if !ok {
			return nil, fmt.Errorf("compress: decider gave no answer for %q", q.ID)
		}
		a, err := toAnswer(q, ha)
		if err != nil {
			return nil, err
		}
		answers = append(answers, a)
	}
	return answers, nil
}

// toAnswer maps one wire answer to an Answer. The noul shape is unverified:
// a number is P(true); a bool is P(true) = confidence when true, 1-confidence
// when false, or NaN (not confident) when no confidence is sent. True is
// always derived from P, never taken from the wire bool.
func toAnswer(q Question, ha httpAnswer) (Answer, error) {
	a := Answer{ID: q.ID, Kind: q.Kind}
	switch {
	case q.Kind == KindChoice && ha.Type == "choice":
		a.Pick = ha.Choice
		a.Dist = ha.Probabilities
	case q.Kind == KindNoul && ha.Type == "noul":
		var f float64
		var b bool
		switch {
		case json.Unmarshal(ha.Noul, &f) == nil:
			a.P = f
			a.True = f >= 0.5
		case json.Unmarshal(ha.Noul, &b) == nil:
			a.True = b
			switch {
			case ha.Confidence == nil:
				a.P = math.NaN()
			case b:
				a.P = *ha.Confidence
			default:
				a.P = 1 - *ha.Confidence
			}
		default:
			return Answer{}, fmt.Errorf("compress: decider noul for %q is neither bool nor number", q.ID)
		}
	default:
		return Answer{}, fmt.Errorf("compress: decider answer type %q for question %q (%s)", ha.Type, q.ID, q.Kind)
	}
	if q.Kind == KindNoul {
		// True follows P so both shapes agree. NaN gives false, which is
		// harmless: a NaN answer is never confident.
		a.True = a.P >= 0.5
	}
	return a, nil
}
