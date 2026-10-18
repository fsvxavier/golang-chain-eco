package vectorstore

import (
	"context"
	"net/http"
)

// Pinecone stores vectors in a Pinecone index. URL is the index host shown in
// the console (https://<index>-<project>.svc.<env>.pinecone.io); create the
// index beforehand with the right dimension.
type Pinecone struct {
	URL       string
	APIKey    string
	Namespace string
	HTTP      *http.Client
}

func (p *Pinecone) rest() *rest {
	return &rest{base: p.URL, hc: p.HTTP, headers: map[string]string{"Api-Key": p.APIKey, "X-Pinecone-API-Version": "2025-01"}}
}

func (p *Pinecone) Upsert(ctx context.Context, recs []Record) error {
	for start := 0; start < len(recs); start += 100 { // API limit per request
		end := min(start+100, len(recs))
		vs := make([]map[string]any, 0, end-start)
		for _, r := range recs[start:end] {
			md := scalarMeta(r.Metadata)
			md["text"] = r.Text
			vs = append(vs, map[string]any{"id": r.ID, "values": r.Vector, "metadata": md})
		}
		if _, err := p.rest().do(ctx, http.MethodPost, "/vectors/upsert", map[string]any{"vectors": vs, "namespace": p.Namespace}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pinecone) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	var out struct {
		Matches []struct {
			ID       string         `json:"id"`
			Score    float64        `json:"score"`
			Values   []float32      `json:"values"`
			Metadata map[string]any `json:"metadata"`
		} `json:"matches"`
	}
	if _, err := p.rest().do(ctx, http.MethodPost, "/query", map[string]any{
		"vector": vec, "topK": k, "includeMetadata": true, "includeValues": true, "namespace": p.Namespace}, &out); err != nil {
		return nil, err
	}
	hits := make([]Hit, len(out.Matches))
	for i, m := range out.Matches {
		text, _ := m.Metadata["text"].(string)
		delete(m.Metadata, "text")
		hits[i] = Hit{Record{m.ID, m.Values, text, m.Metadata}, m.Score}
	}
	return hits, nil
}

func (p *Pinecone) Delete(ctx context.Context, ids []string) error {
	_, err := p.rest().do(ctx, http.MethodPost, "/vectors/delete", map[string]any{"ids": ids, "namespace": p.Namespace}, nil)
	return err
}
