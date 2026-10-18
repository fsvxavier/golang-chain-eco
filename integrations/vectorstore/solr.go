package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Solr stores vectors in an Apache Solr 9+ core/collection (DenseVectorField
// and the {!knn} query parser). Opensolr is managed Solr, so it works too.
// The core must exist; Upsert adds the schema fields it needs.
type Solr struct {
	URL      string // http://localhost:8983
	Core     string
	Username string
	Password string
	HTTP     *http.Client
}

func (s *Solr) base() string {
	return strings.TrimRight(s.URL, "/") + "/solr/" + url.PathEscape(s.Core)
}

func (s *Solr) call(ctx context.Context, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	return post(ctx, s.HTTP, s.base()+path, "application/json", b, s.Username, s.Password, nil)
}

func (s *Solr) schema(ctx context.Context, dim int) error {
	for _, cmd := range []map[string]any{
		{"add-field-type": map[string]any{"name": "lcg_knn", "class": "solr.DenseVectorField", "vectorDimension": dim, "similarityFunction": "cosine"}},
		{"add-field": []map[string]any{
			{"name": "embedding", "type": "lcg_knn", "indexed": true, "stored": true},
			{"name": "text", "type": "string", "indexed": false, "stored": true},
			{"name": "metadata", "type": "string", "indexed": false, "stored": true},
		}},
	} {
		if _, err := s.call(ctx, "/schema", cmd); err != nil && !strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	return nil
}

func (s *Solr) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := s.schema(ctx, len(recs[0].Vector)); err != nil {
		return err
	}
	docs := make([]map[string]any, len(recs))
	for i, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		docs[i] = map[string]any{"id": r.ID, "text": r.Text, "metadata": string(md), "embedding": r.Vector}
	}
	_, err := s.call(ctx, "/update?commit=true", docs)
	return err
}

func (s *Solr) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	vb, _ := json.Marshal(vec)
	raw, err := s.call(ctx, "/select", map[string]any{"query": fmt.Sprintf("{!knn f=embedding topK=%d}%s", k, vb), "fields": "id,text,metadata,embedding,score", "limit": k})
	if err != nil {
		return nil, err
	}
	var out struct {
		Response struct {
			Docs []struct {
				ID        string
				Text      string
				Metadata  string
				Embedding []float32
				Score     float64
			}
		}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	hits := make([]Hit, len(out.Response.Docs))
	for i, d := range out.Response.Docs {
		hits[i] = Hit{Record: Record{ID: d.ID, Text: d.Text, Vector: d.Embedding}, Score: d.Score}
		_ = json.Unmarshal([]byte(d.Metadata), &hits[i].Metadata)
	}
	return hits, nil
}

func (s *Solr) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.call(ctx, "/update?commit=true", map[string]any{"delete": ids})
	return err
}
