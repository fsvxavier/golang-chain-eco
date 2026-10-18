// Package embeddings defines the Embedder interface (langchain_core.embeddings)
// plus a deterministic fake and a caching wrapper.
package embeddings

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"sync"
	"unicode"
)

// Embedder turns text into vectors.
type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// Fake is a deterministic bag-of-words hashing embedder. Similar texts get
// similar vectors, which makes it useful for tests and offline examples
// (DeterministicFakeEmbedding).
type Fake struct{ Dim int }

func (f Fake) dim() int {
	if f.Dim <= 0 {
		return 256
	}
	return f.Dim
}

func (f Fake) embed(text string) []float32 {
	v := make([]float32, f.dim())
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	for _, w := range words {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%uint32(len(v))]++
	}
	return Normalize(v)
}

// EmbedDocuments implements Embedder.
func (f Fake) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.embed(t)
	}
	return out, nil
}

// EmbedQuery implements Embedder.
func (f Fake) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	return f.embed(text), nil
}

// Normalize scales v to unit length in place and returns it.
func Normalize(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return v
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

// Cosine returns the cosine similarity of a and b.
func Cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Cached memoises document embeddings in memory (CacheBackedEmbeddings).
type Cached struct {
	Embedder
	mu sync.Mutex
	m  map[string][]float32
}

// NewCached wraps e with an in-memory cache.
func NewCached(e Embedder) *Cached { return &Cached{Embedder: e, m: map[string][]float32{}} }

// EmbedDocuments implements Embedder, only embedding unseen texts.
func (c *Cached) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missing []string
	var idx []int
	c.mu.Lock()
	for i, t := range texts {
		if v, ok := c.m[t]; ok {
			out[i] = v
		} else {
			missing, idx = append(missing, t), append(idx, i)
		}
	}
	c.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	vs, err := c.Embedder.EmbedDocuments(ctx, missing)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	for j, v := range vs {
		out[idx[j]] = v
		c.m[missing[j]] = v
	}
	c.mu.Unlock()
	return out, nil
}
