package vectorstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// AzureAISearch stores vectors in an Azure AI Search index (HNSW, cosine)
// through the REST API. The index is created on first Upsert. Azure keys only
// allow [A-Za-z0-9_-=], so the record id is stored base64url-encoded as the
// key and verbatim in a "rid" field.
type AzureAISearch struct {
	Endpoint   string // https://<service>.search.windows.net
	APIKey     string
	Index      string
	APIVersion string // default 2024-07-01
	HTTP       *http.Client
}

func (a *AzureAISearch) rest() *rest {
	return &rest{base: a.Endpoint, hc: a.HTTP, headers: map[string]string{"api-key": a.APIKey}}
}

func (a *AzureAISearch) path(suffix string) string {
	v := a.APIVersion
	if v == "" {
		v = "2024-07-01"
	}
	return "/indexes/" + url.PathEscape(a.Index) + suffix + "?api-version=" + v
}

func azKey(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }

func (a *AzureAISearch) ensure(ctx context.Context, dim int) error {
	st, err := a.rest().do(ctx, http.MethodGet, a.path(""), nil, nil)
	if err != nil || st != http.StatusNotFound {
		return err
	}
	_, err = a.rest().do(ctx, http.MethodPut, a.path(""), map[string]any{
		"name": a.Index,
		"fields": []map[string]any{
			{"name": "id", "type": "Edm.String", "key": true},
			{"name": "rid", "type": "Edm.String", "filterable": true},
			{"name": "content", "type": "Edm.String", "searchable": true},
			{"name": "metadata", "type": "Edm.String"},
			{"name": "embedding", "type": "Collection(Edm.Single)", "searchable": true, "retrievable": true, "dimensions": dim, "vectorSearchProfile": "lcg-profile"},
		},
		"vectorSearch": map[string]any{
			"algorithms": []map[string]any{{"name": "lcg-hnsw", "kind": "hnsw", "hnswParameters": map[string]any{"metric": "cosine"}}},
			"profiles":   []map[string]any{{"name": "lcg-profile", "algorithm": "lcg-hnsw"}},
		},
	}, nil)
	return err
}

func (a *AzureAISearch) index(ctx context.Context, actions []map[string]any) error {
	var resp struct {
		Value []struct {
			Key          string `json:"key"`
			Status       bool   `json:"status"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"value"`
	}
	st, err := a.rest().do(ctx, http.MethodPost, a.path("/docs/index"), map[string]any{"value": actions}, &resp)
	if err := need404(st, err, "azure search index"); err != nil {
		return err
	}
	for _, r := range resp.Value {
		if !r.Status {
			return fmt.Errorf("azure search: document %s rejected: %s", r.Key, r.ErrorMessage)
		}
	}
	return nil
}

func (a *AzureAISearch) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := a.ensure(ctx, len(recs[0].Vector)); err != nil {
		return err
	}
	acts := make([]map[string]any, len(recs))
	for i, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		acts[i] = map[string]any{"@search.action": "mergeOrUpload", "id": azKey(r.ID), "rid": r.ID, "content": r.Text, "metadata": string(md), "embedding": r.Vector}
	}
	return a.index(ctx, acts)
}

func (a *AzureAISearch) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	var resp struct {
		Value []struct {
			Score     float64   `json:"@search.score"`
			RID       string    `json:"rid"`
			Content   string    `json:"content"`
			Metadata  string    `json:"metadata"`
			Embedding []float32 `json:"embedding"`
		} `json:"value"`
	}
	st, err := a.rest().do(ctx, http.MethodPost, a.path("/docs/search"), map[string]any{
		"select": "rid,content,metadata,embedding", "top": k,
		"vectorQueries": []map[string]any{{"kind": "vector", "vector": vec, "fields": "embedding", "k": k}}}, &resp)
	if st == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, len(resp.Value))
	for i, v := range resp.Value {
		hits[i] = Hit{Record: Record{ID: v.RID, Text: v.Content, Vector: v.Embedding}, Score: v.Score}
		_ = json.Unmarshal([]byte(v.Metadata), &hits[i].Metadata)
	}
	return hits, nil
}

func (a *AzureAISearch) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	acts := make([]map[string]any, len(ids))
	for i, id := range ids {
		acts[i] = map[string]any{"@search.action": "delete", "id": azKey(id)}
	}
	return a.index(ctx, acts)
}
