package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Typesense stores vectors in a Typesense collection (REST API).
type Typesense struct {
	URL        string // e.g. http://localhost:8108
	APIKey     string
	Collection string
	HTTP       *http.Client
}

func (t *Typesense) rest() *rest {
	return &rest{base: t.URL, hc: t.HTTP, headers: map[string]string{"X-TYPESENSE-API-KEY": t.APIKey}}
}

func (t *Typesense) col() string { return "/collections/" + url.PathEscape(t.Collection) }

func (t *Typesense) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	_, err := t.rest().do(ctx, http.MethodPost, "/collections", map[string]any{"name": t.Collection, "fields": []map[string]any{
		{"name": "text", "type": "string"},
		{"name": "metadata", "type": "string", "index": false, "optional": true},
		{"name": "embedding", "type": "float[]", "num_dim": len(recs[0].Vector)},
	}}, nil)
	if err != nil && !strings.Contains(err.Error(), "http 409") {
		return err
	}
	var nd bytes.Buffer
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		line, _ := json.Marshal(map[string]any{"id": r.ID, "text": r.Text, "metadata": string(md), "embedding": r.Vector})
		nd.Write(line)
		nd.WriteByte('\n')
	}
	// import answers one JSON line per document; surface the first failure
	var res string
	if _, err := t.rest().doRaw(ctx, http.MethodPost, t.col()+"/documents/import?action=upsert", nd.Bytes(), &res); err != nil {
		return err
	}
	for _, l := range strings.Split(strings.TrimSpace(res), "\n") {
		var s struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if json.Unmarshal([]byte(l), &s) == nil && !s.Success {
			return fmt.Errorf("typesense: import: %s", s.Error)
		}
	}
	return nil
}

func (t *Typesense) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	vb, _ := json.Marshal(vec)
	var resp struct {
		Results []struct {
			Hits []struct {
				Document struct {
					ID, Text, Metadata string
					Embedding          []float32
				} `json:"document"`
				VectorDistance float64 `json:"vector_distance"`
			} `json:"hits"`
		} `json:"results"`
	}
	st, err := t.rest().do(ctx, http.MethodPost, "/multi_search", map[string]any{"searches": []map[string]any{{
		"collection": t.Collection, "q": "*", "vector_query": fmt.Sprintf("embedding:(%s, k:%d)", vb, k), "per_page": k}}}, &resp)
	if st == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for _, res := range resp.Results {
		for _, h := range res.Hits {
			x := Hit{Record: Record{ID: h.Document.ID, Text: h.Document.Text, Vector: h.Document.Embedding}, Score: 1 - h.VectorDistance}
			_ = json.Unmarshal([]byte(h.Document.Metadata), &x.Metadata)
			hits = append(hits, x)
		}
	}
	return hits, nil
}

func (t *Typesense) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = "`" + id + "`"
	}
	_, err := t.rest().do(ctx, http.MethodDelete, t.col()+"/documents?filter_by="+url.QueryEscape("id:["+strings.Join(q, ",")+"]"), nil, nil)
	return err
}
