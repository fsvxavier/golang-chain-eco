package vectorstore

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Milvus stores vectors in a Milvus / Zilliz collection (REST API v2).
type Milvus struct {
	URL        string // e.g. http://localhost:19530
	Token      string
	Collection string
	HTTP       *http.Client
}

type milvusResp struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []map[string]any `json:"data"`
}

func (m *Milvus) call(ctx context.Context, path string, body map[string]any) (milvusResp, error) {
	body["collectionName"] = m.Collection
	h := map[string]string{}
	if m.Token != "" {
		h["Authorization"] = "Bearer " + m.Token
	}
	var out milvusResp
	_, err := (&rest{base: m.URL, hc: m.HTTP, headers: h}).do(ctx, http.MethodPost, path, body, &out)
	if err == nil && out.Code != 0 && out.Code != 200 {
		err = fmt.Errorf("milvus: %s (code %d)", out.Message, out.Code)
	}
	return out, err
}

func (m *Milvus) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if _, err := m.call(ctx, "/v2/vectordb/collections/has", map[string]any{}); err != nil {
		return err
	}
	// create is idempotent for an identical schema; ignore "already exist"
	if _, err := m.call(ctx, "/v2/vectordb/collections/create", map[string]any{
		"dimension": len(recs[0].Vector), "metricType": "COSINE", "idType": "VarChar",
		"primaryFieldName": "id", "vectorFieldName": "vector", "params": map[string]any{"max_length": 512},
	}); err != nil && !strings.Contains(err.Error(), "already exist") {
		return err
	}
	data := make([]map[string]any, len(recs))
	for i, r := range recs {
		data[i] = map[string]any{"id": r.ID, "vector": r.Vector, "text": r.Text, "metadata": r.Metadata}
	}
	_, err := m.call(ctx, "/v2/vectordb/entities/upsert", map[string]any{"data": data})
	return err
}

func (m *Milvus) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	out, err := m.call(ctx, "/v2/vectordb/entities/search", map[string]any{
		"data": [][]float32{vec}, "annsField": "vector", "limit": k, "outputFields": []string{"id", "text", "metadata", "vector"}})
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(out.Data))
	for _, d := range out.Data {
		h := Hit{}
		h.ID, _ = d["id"].(string)
		h.Text, _ = d["text"].(string)
		h.Metadata, _ = d["metadata"].(map[string]any)
		h.Score, _ = d["distance"].(float64) // COSINE: larger is closer
		if vs, ok := d["vector"].([]any); ok {
			for _, x := range vs {
				f, _ := x.(float64)
				h.Vector = append(h.Vector, float32(f))
			}
		}
		hits = append(hits, h)
	}
	return hits, nil
}

func (m *Milvus) Delete(ctx context.Context, ids []string) error {
	quoted := ""
	for i, id := range ids {
		if i > 0 {
			quoted += ","
		}
		quoted += fmt.Sprintf("%q", id)
	}
	_, err := m.call(ctx, "/v2/vectordb/entities/delete", map[string]any{"filter": "id in [" + quoted + "]"})
	return err
}
