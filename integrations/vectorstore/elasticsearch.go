package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Elasticsearch stores vectors in an index with a dense_vector field
// (also works with OpenSearch-compatible kNN search bodies on ES 8+).
type Elasticsearch struct {
	URL    string
	Index  string
	APIKey string // "Authorization: ApiKey <APIKey>"
	// Username/Password use basic auth when APIKey is empty.
	Username, Password string
	HTTP               *http.Client
}

func (e *Elasticsearch) rest() *rest {
	h := map[string]string{}
	if e.APIKey != "" {
		h["Authorization"] = "ApiKey " + e.APIKey
	} else if e.Username != "" {
		req, _ := http.NewRequest("GET", "http://x", nil)
		req.SetBasicAuth(e.Username, e.Password)
		h["Authorization"] = req.Header.Get("Authorization")
	}
	return &rest{base: e.URL, hc: e.HTTP, headers: h}
}

func (e *Elasticsearch) ensure(ctx context.Context, dim int) error {
	r, idx := e.rest(), "/"+url.PathEscape(e.Index)
	st, err := r.do(ctx, http.MethodHead, idx, nil, nil)
	if err != nil || st != http.StatusNotFound {
		return err
	}
	_, err = r.do(ctx, http.MethodPut, idx, map[string]any{"mappings": map[string]any{"properties": map[string]any{
		"vector":   map[string]any{"type": "dense_vector", "dims": dim, "similarity": "cosine"},
		"text":     map[string]any{"type": "text"},
		"metadata": map[string]any{"type": "object", "dynamic": true},
	}}}, nil)
	return err
}

func (e *Elasticsearch) bulk(ctx context.Context, lines []any) error {
	var buf bytes.Buffer
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	var resp struct {
		Errors bool `json:"errors"`
	}
	if _, err := e.rest().do(ctx, http.MethodPost, "/_bulk?refresh=true", buf.Bytes(), &resp); err != nil {
		return err
	}
	if resp.Errors {
		return &bulkError{}
	}
	return nil
}

type bulkError struct{}

func (*bulkError) Error() string { return "elasticsearch: bulk request reported item errors" }

func (e *Elasticsearch) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := e.ensure(ctx, len(recs[0].Vector)); err != nil {
		return err
	}
	var lines []any
	for _, r := range recs {
		lines = append(lines,
			map[string]any{"index": map[string]any{"_index": e.Index, "_id": r.ID}},
			map[string]any{"vector": r.Vector, "text": r.Text, "metadata": r.Metadata})
	}
	return e.bulk(ctx, lines)
}

func (e *Elasticsearch) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	var resp struct {
		Hits struct {
			Hits []struct {
				ID     string  `json:"_id"`
				Score  float64 `json:"_score"`
				Source struct {
					Vector   []float32      `json:"vector"`
					Text     string         `json:"text"`
					Metadata map[string]any `json:"metadata"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	st, err := e.rest().do(ctx, http.MethodPost, "/"+url.PathEscape(e.Index)+"/_search", map[string]any{
		"size": k, "knn": map[string]any{"field": "vector", "query_vector": vec, "k": k, "num_candidates": max(k*10, 50)},
	}, &resp)
	if st == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, len(resp.Hits.Hits))
	for i, h := range resp.Hits.Hits {
		hits[i] = Hit{Record{h.ID, h.Source.Vector, h.Source.Text, h.Source.Metadata}, h.Score}
	}
	return hits, nil
}

func (e *Elasticsearch) Delete(ctx context.Context, ids []string) error {
	lines := make([]any, len(ids))
	for i, id := range ids {
		lines[i] = map[string]any{"delete": map[string]any{"_index": e.Index, "_id": id}}
	}
	return e.bulk(ctx, lines)
}
