package vectorstore

import (
	"context"
	"net/http"
	"net/url"
)

// Upstash stores vectors in an Upstash Vector index (REST API). Create the
// index in the Upstash console with the embedder's dimension; the metric is
// the index's own setting.
type Upstash struct {
	URL       string // https://<id>-<region>.upstash.io
	Token     string
	Namespace string // optional
	HTTP      *http.Client
}

func (u *Upstash) rest() *rest {
	return &rest{base: u.URL, hc: u.HTTP, headers: map[string]string{"Authorization": "Bearer " + u.Token}}
}

func (u *Upstash) path(op string) string {
	if u.Namespace == "" {
		return "/" + op
	}
	return "/" + op + "/" + url.PathEscape(u.Namespace)
}

func (u *Upstash) Upsert(ctx context.Context, recs []Record) error {
	rows := make([]map[string]any, len(recs))
	for i, r := range recs {
		rows[i] = map[string]any{"id": r.ID, "vector": r.Vector, "metadata": map[string]any{"text": r.Text, "metadata": r.Metadata}}
	}
	st, err := u.rest().do(ctx, http.MethodPost, u.path("upsert"), rows, nil)
	return need404(st, err, "upstash index")
}

func (u *Upstash) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	var resp struct {
		Result []struct {
			ID       string    `json:"id"`
			Score    float64   `json:"score"`
			Vector   []float32 `json:"vector"`
			Metadata struct {
				Text     string         `json:"text"`
				Metadata map[string]any `json:"metadata"`
			} `json:"metadata"`
		} `json:"result"`
	}
	st, err := u.rest().do(ctx, http.MethodPost, u.path("query"),
		map[string]any{"vector": vec, "topK": k, "includeMetadata": true, "includeVectors": true}, &resp)
	if err := need404(st, err, "upstash index"); err != nil {
		return nil, err
	}
	hits := make([]Hit, len(resp.Result))
	for i, r := range resp.Result {
		hits[i] = Hit{Record{r.ID, r.Vector, r.Metadata.Text, r.Metadata.Metadata}, r.Score}
	}
	return hits, nil
}

func (u *Upstash) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := u.rest().do(ctx, http.MethodPost, u.path("delete"), ids, nil)
	return err
}
