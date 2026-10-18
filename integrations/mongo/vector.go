package mongo

import (
	"context"
	"math"
	"sort"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
)

// Vector is a vectorstore.Backend on a collection (default "vectors").
//
// With Index set it queries an Atlas Vector Search index ($vectorSearch on
// path "embedding"; create the index in Atlas, it cannot be created from
// here). With Index empty it scans the collection and ranks by cosine
// similarity client-side, which works on any MongoDB but is O(n).
type Vector struct {
	C          *Client
	Collection string
	Index      string
}

var _ vectorstore.Backend = (*Vector)(nil)

func (v *Vector) coll() string {
	if v.Collection == "" {
		return "vectors"
	}
	return v.Collection
}

func (v *Vector) Upsert(ctx context.Context, recs []vectorstore.Record) error {
	for _, r := range recs {
		md := r.Metadata
		if md == nil {
			md = map[string]any{}
		}
		if err := v.C.upsert(ctx, v.coll(), r.ID, M{"text": r.Text, "metadata": md, "embedding": r.Vector}); err != nil {
			return err
		}
	}
	return nil
}

func (v *Vector) Query(ctx context.Context, vec []float32, k int) ([]vectorstore.Hit, error) {
	if v.Index == "" {
		return v.scan(ctx, vec, k)
	}
	res, err := v.C.Command(ctx, D{{"aggregate", v.coll()}, {"cursor", D{}}, {"pipeline", []any{
		D{{"$vectorSearch", D{{"index", v.Index}, {"path", "embedding"}, {"queryVector", vec},
			{"numCandidates", int64(max(k*10, 100))}, {"limit", int64(k)}}}},
		D{{"$set", D{{"score", D{{"$meta", "vectorSearchScore"}}}}}},
	}}})
	if err != nil {
		return nil, err
	}
	cur, _ := res["cursor"].(M)
	batch, _ := cur["firstBatch"].([]any)
	hits := make([]vectorstore.Hit, 0, len(batch))
	for _, d := range batch {
		h := toHit(d.(M))
		h.Score = num(d.(M)["score"])
		hits = append(hits, h)
	}
	return hits, nil
}

func toHit(d M) vectorstore.Hit {
	h := vectorstore.Hit{}
	h.ID, _ = d["_id"].(string)
	h.Text, _ = d["text"].(string)
	h.Metadata, _ = d["metadata"].(M)
	emb, _ := d["embedding"].([]any)
	h.Vector = make([]float32, len(emb))
	for i, f := range emb {
		h.Vector[i] = float32(num(f))
	}
	return h
}

func (v *Vector) scan(ctx context.Context, q []float32, k int) ([]vectorstore.Hit, error) {
	docs, err := v.C.find(ctx, v.coll(), M{}, nil, 0)
	if err != nil {
		return nil, err
	}
	hits := make([]vectorstore.Hit, 0, len(docs))
	for _, d := range docs {
		h := toHit(d)
		h.Score = cosine(q, h.Vector)
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := 0; i < len(a) && i < len(b); i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func (v *Vector) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	in := make([]any, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	return v.C.remove(ctx, v.coll(), M{"_id": M{"$in": in}})
}
