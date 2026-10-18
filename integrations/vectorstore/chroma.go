package vectorstore

import (
	"context"
	"net/http"
	"net/url"
	"sync"
)

// Chroma stores vectors in a Chroma server collection (REST API v2).
type Chroma struct {
	URL        string // e.g. http://localhost:8000
	Tenant     string // default default_tenant
	Database   string // default default_database
	Collection string
	Token      string // optional X-Chroma-Token
	HTTP       *http.Client

	mu sync.Mutex
	id string
}

func (c *Chroma) rest() *rest {
	return &rest{base: c.URL, hc: c.HTTP, headers: map[string]string{"X-Chroma-Token": c.Token}}
}

func (c *Chroma) base() string {
	t, d := c.Tenant, c.Database
	if t == "" {
		t = "default_tenant"
	}
	if d == "" {
		d = "default_database"
	}
	return "/api/v2/tenants/" + url.PathEscape(t) + "/databases/" + url.PathEscape(d) + "/collections"
}

func (c *Chroma) collectionID(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.id != "" {
		return c.id, nil
	}
	var out struct {
		ID string `json:"id"`
	}
	_, err := c.rest().do(ctx, http.MethodPost, c.base(), map[string]any{
		"name": c.Collection, "get_or_create": true,
		"configuration": map[string]any{"hnsw": map[string]any{"space": "cosine"}},
	}, &out)
	c.id = out.ID
	return out.ID, err
}

func (c *Chroma) Upsert(ctx context.Context, recs []Record) error {
	id, err := c.collectionID(ctx)
	if err != nil {
		return err
	}
	ids, vecs, docs, metas := make([]string, len(recs)), make([][]float32, len(recs)), make([]string, len(recs)), make([]map[string]any, len(recs))
	for i, r := range recs {
		ids[i], vecs[i], docs[i], metas[i] = r.ID, r.Vector, r.Text, scalarMeta(r.Metadata)
		if len(metas[i]) == 0 {
			metas[i] = nil // Chroma rejects empty metadata objects
		}
	}
	st, err := c.rest().do(ctx, http.MethodPost, c.base()+"/"+id+"/upsert", map[string]any{"ids": ids, "embeddings": vecs, "documents": docs, "metadatas": metas}, nil)
	return need404(st, err, "chroma collection")
}

func (c *Chroma) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	id, err := c.collectionID(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		IDs        [][]string         `json:"ids"`
		Documents  [][]string         `json:"documents"`
		Metadatas  [][]map[string]any `json:"metadatas"`
		Distances  [][]float64        `json:"distances"`
		Embeddings [][][]float32      `json:"embeddings"`
	}
	st, err := c.rest().do(ctx, http.MethodPost, c.base()+"/"+id+"/query", map[string]any{
		"query_embeddings": [][]float32{vec}, "n_results": k,
		"include": []string{"documents", "metadatas", "distances", "embeddings"},
	}, &out)
	if err := need404(st, err, "chroma collection"); err != nil || len(out.IDs) == 0 {
		return nil, err
	}
	hits := make([]Hit, len(out.IDs[0]))
	for i := range hits {
		h := Hit{Record: Record{ID: out.IDs[0][i]}}
		if i < len(out.Documents[0]) {
			h.Text = out.Documents[0][i]
		}
		if len(out.Metadatas) > 0 && i < len(out.Metadatas[0]) {
			h.Metadata = out.Metadatas[0][i]
		}
		if len(out.Embeddings) > 0 && i < len(out.Embeddings[0]) {
			h.Vector = out.Embeddings[0][i]
		}
		if len(out.Distances) > 0 && i < len(out.Distances[0]) {
			h.Score = 1 - out.Distances[0][i] // cosine distance -> similarity
		}
		hits[i] = h
	}
	return hits, nil
}

func (c *Chroma) Delete(ctx context.Context, ids []string) error {
	id, err := c.collectionID(ctx)
	if err != nil {
		return err
	}
	_, err = c.rest().do(ctx, http.MethodPost, c.base()+"/"+id+"/delete", map[string]any{"ids": ids}, nil)
	return err
}
