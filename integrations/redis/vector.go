package redis

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/integrations/vectorstore"
)

// Vector is a vectorstore.Backend on RediSearch (Redis Stack / Redis 8): one
// HASH per record under Prefix, indexed with a FLAT cosine vector field.
type Vector struct {
	C      *Client
	Index  string // default "lcg_idx"
	Prefix string // default "lcg:vec:"
}

var _ vectorstore.Backend = (*Vector)(nil)

func (v *Vector) index() string {
	if v.Index == "" {
		return "lcg_idx"
	}
	return v.Index
}

func (v *Vector) prefix() string {
	if v.Prefix == "" {
		return "lcg:vec:"
	}
	return v.Prefix
}

func f32bytes(vec []float32) []byte {
	b := make([]byte, 4*len(vec))
	for i, f := range vec {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func bytesF32(s string) []float32 {
	out := make([]float32, len(s)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32([]byte(s[4*i : 4*i+4])))
	}
	return out
}

func (v *Vector) ensure(ctx context.Context, dim int) error {
	_, err := v.C.Do(ctx, "FT.CREATE", v.index(), "ON", "HASH", "PREFIX", 1, v.prefix(),
		"SCHEMA", "text", "TEXT", "metadata", "TEXT",
		"embedding", "VECTOR", "FLAT", 6, "TYPE", "FLOAT32", "DIM", dim, "DISTANCE_METRIC", "COSINE")
	if err != nil && !strings.Contains(err.Error(), "Index already exists") {
		return err
	}
	return nil
}

func (v *Vector) Upsert(ctx context.Context, recs []vectorstore.Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := v.ensure(ctx, len(recs[0].Vector)); err != nil {
		return err
	}
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		if _, err := v.C.Do(ctx, "HSET", v.prefix()+r.ID, "text", r.Text, "metadata", md, "embedding", f32bytes(r.Vector)); err != nil {
			return err
		}
	}
	return nil
}

func (v *Vector) Query(ctx context.Context, vec []float32, k int) ([]vectorstore.Hit, error) {
	res, err := v.C.Do(ctx, "FT.SEARCH", v.index(), fmt.Sprintf("*=>[KNN %d @embedding $vec AS score]", k),
		"PARAMS", 2, "vec", f32bytes(vec), "SORTBY", "score", "LIMIT", 0, k, "DIALECT", 2)
	if err != nil {
		if strings.Contains(err.Error(), "No such index") || strings.Contains(err.Error(), "Unknown index") {
			return nil, nil
		}
		return nil, err
	}
	arr, _ := res.([]any)
	var hits []vectorstore.Hit
	for i := 1; i+1 < len(arr); i += 2 { // [total, key, fields, key, fields, ...]
		key, _ := arr[i].(string)
		f := map[string]string{}
		fv := strs(arr[i+1])
		for j := 0; j+1 < len(fv); j += 2 {
			f[fv[j]] = fv[j+1]
		}
		h := vectorstore.Hit{Record: vectorstore.Record{ID: strings.TrimPrefix(key, v.prefix()), Text: f["text"], Vector: bytesF32(f["embedding"])}}
		_ = json.Unmarshal([]byte(f["metadata"]), &h.Metadata)
		var dist float64
		fmt.Sscan(f["score"], &dist)
		h.Score = 1 - dist
		hits = append(hits, h)
	}
	return hits, nil
}

func (v *Vector) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{"DEL"}
	for _, id := range ids {
		args = append(args, v.prefix()+id)
	}
	_, err := v.C.Do(ctx, args...)
	return err
}
