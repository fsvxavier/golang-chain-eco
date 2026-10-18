package vectorstore

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

// Qdrant stores vectors in a Qdrant collection (REST API).
type Qdrant struct {
	URL        string // e.g. http://localhost:6333
	APIKey     string
	Collection string
	HTTP       *http.Client

	once sync.Once
}

func (q *Qdrant) rest() *rest {
	return &rest{base: q.URL, hc: q.HTTP, headers: map[string]string{"api-key": q.APIKey}}
}

// pointID maps any string ID to a UUID (Qdrant accepts only UUIDs/uints).
func pointID(id string) string {
	h := sha1.Sum([]byte("golang-chain-eco:" + id))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func (q *Qdrant) ensure(ctx context.Context, dim int) error {
	r, col := q.rest(), "/collections/"+url.PathEscape(q.Collection)
	st, err := r.do(ctx, http.MethodGet, col, nil, nil)
	if err != nil || st != http.StatusNotFound {
		return err
	}
	_, err = r.do(ctx, http.MethodPut, col, map[string]any{"vectors": map[string]any{"size": dim, "distance": "Cosine"}}, nil)
	return err
}

func (q *Qdrant) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := q.ensure(ctx, len(recs[0].Vector)); err != nil {
		return err
	}
	pts := make([]map[string]any, len(recs))
	for i, r := range recs {
		pts[i] = map[string]any{"id": pointID(r.ID), "vector": r.Vector,
			"payload": map[string]any{"_id": r.ID, "page_content": r.Text, "metadata": r.Metadata}}
	}
	st, err := q.rest().do(ctx, http.MethodPut, "/collections/"+url.PathEscape(q.Collection)+"/points?wait=true", map[string]any{"points": pts}, nil)
	return need404(st, err, "qdrant collection")
}

func (q *Qdrant) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	var resp struct {
		Result []struct {
			Score   float64   `json:"score"`
			Vector  []float32 `json:"vector"`
			Payload struct {
				ID       string         `json:"_id"`
				Content  string         `json:"page_content"`
				Metadata map[string]any `json:"metadata"`
			} `json:"payload"`
		} `json:"result"`
	}
	st, err := q.rest().do(ctx, http.MethodPost, "/collections/"+url.PathEscape(q.Collection)+"/points/search",
		map[string]any{"vector": vec, "limit": k, "with_payload": true, "with_vector": true}, &resp)
	if st == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, len(resp.Result))
	for i, x := range resp.Result {
		hits[i] = Hit{Record{x.Payload.ID, x.Vector, x.Payload.Content, x.Payload.Metadata}, x.Score}
	}
	return hits, nil
}

func (q *Qdrant) Delete(ctx context.Context, ids []string) error {
	pts := make([]string, len(ids))
	for i, id := range ids {
		pts[i] = pointID(id)
	}
	_, err := q.rest().do(ctx, http.MethodPost, "/collections/"+url.PathEscape(q.Collection)+"/points/delete?wait=true", map[string]any{"points": pts}, nil)
	return err
}
