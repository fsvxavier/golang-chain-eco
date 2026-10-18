// Package graphretriever is a graph-traversal retriever (the "Graph RAG"
// pattern): start from the documents most similar to the query, then follow
// edges defined over metadata fields to pull in related documents, ranking
// each hop by similarity to the query. It works on any
// vectorstores.VectorStore; no graph database is needed.
//
//	r := &graphretriever.Retriever{Store: vs, K: 6, StartK: 3, Depth: 2,
//		Edges: []graphretriever.Edge{{From: "mentions", To: "id"}, {From: "url", To: "links_to"}}}
package graphretriever

import (
	"context"
	"fmt"
	"slices"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/retrievers"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
)

// Edge links a document to every document whose To metadata field shares a
// value with this document's From field. Either field may hold a single
// value or a list, so {From: "mentions", To: "id"} follows a list of ids.
type Edge struct{ From, To string }

// Retriever implements retrievers.Retriever.
type Retriever struct {
	Store     vectorstores.VectorStore
	Edges     []Edge
	K         int // total documents returned (default 4)
	StartK    int // initial similarity hits (default K/2, at least 1)
	AdjacentK int // neighbours taken per document and edge (default 2)
	Depth     int // hops to follow (default 1)
}

var _ retrievers.Retriever = (*Retriever)(nil)

func values(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		return x
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = fmt.Sprint(e)
		}
		return out
	default:
		return []string{fmt.Sprint(x)}
	}
}

// key identifies a document: its ID, or its content when it has none.
func key(d documents.Document) string {
	if d.ID != "" {
		return d.ID
	}
	return d.PageContent
}

func (r *Retriever) Invoke(ctx context.Context, query string) ([]documents.Document, error) {
	k := cmpOr(r.K, 4)
	startK := cmpOr(r.StartK, max(k/2, 1))
	adj := cmpOr(r.AdjacentK, 2)
	depth := cmpOr(r.Depth, 1)

	start, err := vectorstores.SimilaritySearch(ctx, r.Store, query, startK, nil)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []documents.Document
	frontier := start
	for _, d := range start {
		if !seen[key(d)] {
			seen[key(d)] = true
			out = append(out, d)
		}
	}
	for hop := 0; hop < depth && len(out) < k && len(frontier) > 0; hop++ {
		var next []documents.Document
		for _, d := range frontier {
			for _, e := range r.Edges {
				want := values(d.Metadata[e.From])
				if len(want) == 0 {
					continue
				}
				e := e
				nb, err := vectorstores.SimilaritySearch(ctx, r.Store, query, adj+len(out), func(c documents.Document) bool {
					for _, v := range values(c.Metadata[e.To]) {
						if slices.Contains(want, v) {
							return true
						}
					}
					return false
				})
				if err != nil {
					return nil, err
				}
				taken := 0
				for _, n := range nb {
					if seen[key(n)] || taken >= adj {
						continue
					}
					seen[key(n)] = true
					out = append(out, n)
					next = append(next, n)
					taken++
				}
			}
		}
		frontier = next
	}
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func cmpOr(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}
