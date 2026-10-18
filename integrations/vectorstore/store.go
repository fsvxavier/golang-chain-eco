// Package vectorstore connects the vectorstores.VectorStore interface to
// external databases: Qdrant, Chroma, Pinecone, Elasticsearch, Weaviate,
// Milvus (REST) and PostgreSQL/pgvector (database/sql).
//
// Every integration implements the small Backend interface; Store adds
// embedding, client-side filtering and MMR on top, so all of them behave the
// same way:
//
//	store := vectorstore.New(embedder, vectorstore.Qdrant{URL: "http://localhost:6333", Collection: "docs"})
//	store.AddDocuments(ctx, docs)
//	hits, _ := vectorstores.SimilaritySearch(ctx, store, "query", 4, nil)
package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
)

// Record is one stored chunk.
type Record struct {
	ID       string
	Vector   []float32
	Text     string
	Metadata map[string]any
}

// Hit is a search result; Score is higher for more similar records. Vector
// may be nil when the backend did not return it.
type Hit struct {
	Record
	Score float64
}

// Backend is the storage-specific part of an integration.
type Backend interface {
	// Upsert stores records, creating the collection/index/table if needed.
	Upsert(ctx context.Context, recs []Record) error
	// Query returns the k nearest records to vec.
	Query(ctx context.Context, vec []float32, k int) ([]Hit, error)
	Delete(ctx context.Context, ids []string) error
}

// Store implements vectorstores.VectorStore on top of a Backend.
type Store struct {
	emb embeddings.Embedder
	be  Backend
}

var _ vectorstores.VectorStore = (*Store)(nil)

// New wraps be with embedding and search helpers.
func New(emb embeddings.Embedder, be Backend) *Store { return &Store{emb: emb, be: be} }

// AddDocuments embeds and stores docs, returning their IDs.
func (s *Store) AddDocuments(ctx context.Context, docs []documents.Document) ([]string, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.PageContent
	}
	vecs, err := s.emb.EmbedDocuments(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(docs) {
		return nil, fmt.Errorf("vectorstore: embedder returned %d vectors for %d documents", len(vecs), len(docs))
	}
	recs := make([]Record, len(docs))
	ids := make([]string, len(docs))
	for i, d := range docs {
		id := d.ID
		if id == "" {
			id = messages.NewID()
		}
		ids[i] = id
		recs[i] = Record{ID: id, Vector: vecs[i], Text: d.PageContent, Metadata: d.Metadata}
	}
	return ids, s.be.Upsert(ctx, recs)
}

func (s *Store) search(ctx context.Context, query string, k, fetch int, filter vectorstores.Filter) ([]float32, []Hit, error) {
	q, err := s.emb.EmbedQuery(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	if filter != nil {
		fetch = max(fetch, k*4) // over-fetch: the filter runs client-side
	}
	hits, err := s.be.Query(ctx, q, fetch)
	if err != nil {
		return nil, nil, err
	}
	if filter != nil {
		kept := hits[:0]
		for _, h := range hits {
			if filter(toDoc(h.Record)) {
				kept = append(kept, h)
			}
		}
		hits = kept
	}
	return q, hits, nil
}

func toDoc(r Record) documents.Document {
	return documents.Document{ID: r.ID, PageContent: r.Text, Metadata: r.Metadata}
}

// SimilaritySearchWithScore implements vectorstores.VectorStore.
func (s *Store) SimilaritySearchWithScore(ctx context.Context, query string, k int, filter vectorstores.Filter) ([]vectorstores.Scored, error) {
	_, hits, err := s.search(ctx, query, k, k, filter)
	if err != nil {
		return nil, err
	}
	if len(hits) > k {
		hits = hits[:k]
	}
	out := make([]vectorstores.Scored, len(hits))
	for i, h := range hits {
		out[i] = vectorstores.Scored{Document: toDoc(h.Record), Score: h.Score}
	}
	return out, nil
}

// MaxMarginalRelevance implements vectorstores.VectorStore. Candidate
// vectors come from the backend when it returns them, otherwise they are
// re-embedded.
func (s *Store) MaxMarginalRelevance(ctx context.Context, query string, k, fetchK int, lambda float64, filter vectorstores.Filter) ([]documents.Document, error) {
	if fetchK <= 0 {
		fetchK = 20
	}
	q, hits, err := s.search(ctx, query, k, fetchK, filter)
	if err != nil {
		return nil, err
	}
	var missing []int
	var texts []string
	for i, h := range hits {
		if len(h.Vector) == 0 {
			missing = append(missing, i)
			texts = append(texts, h.Text)
		}
	}
	if len(missing) > 0 {
		vs, err := s.emb.EmbedDocuments(ctx, texts)
		if err != nil {
			return nil, err
		}
		for j, i := range missing {
			hits[i].Vector = vs[j]
		}
	}
	var picked []int
	used := make([]bool, len(hits))
	for len(picked) < min(k, len(hits)) {
		best, bestScore := -1, -1e18
		for i := range hits {
			if used[i] {
				continue
			}
			red := 0.0
			for _, p := range picked {
				red = max(red, embeddings.Cosine(hits[i].Vector, hits[p].Vector))
			}
			if sc := lambda*embeddings.Cosine(q, hits[i].Vector) - (1-lambda)*red; sc > bestScore {
				best, bestScore = i, sc
			}
		}
		used[best] = true
		picked = append(picked, best)
	}
	out := make([]documents.Document, len(picked))
	for i, p := range picked {
		out[i] = toDoc(hits[p].Record)
	}
	return out, nil
}

// Delete implements vectorstores.VectorStore.
func (s *Store) Delete(ctx context.Context, ids []string) error { return s.be.Delete(ctx, ids) }

// ---------------------------------------------------------------- REST helper

type rest struct {
	base    string
	headers map[string]string
	hc      *http.Client
}

func (r *rest) do(ctx context.Context, method, path string, body any, out any) (status int, err error) {
	var rd io.Reader
	ctype := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ctype = bytes.NewReader(b), "application/x-ndjson"
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.base, "/")+path, rd)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range r.headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	hc := r.hc
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, nil // callers decide whether 404 matters
	}
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("%s %s: http %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("%s %s: bad response: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// need404 turns a 404 from do into an error for calls that must succeed.
func need404(status int, err error, what string) error {
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("%s: not found", what)
	}
	return nil
}

// metaJSON flattens metadata values for stores that only take scalars.
func scalarMeta(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch v.(type) {
		case string, bool, int, int64, float32, float64:
			out[k] = v
		default:
			b, _ := json.Marshal(v)
			out[k] = string(b)
		}
	}
	return out
}
