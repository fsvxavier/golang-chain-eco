// Package rag implements retrieval-augmented generation: an incremental
// indexer and a hybrid (vector + BM25) answering pipeline. It composes
// core/retrievers and integrations/retrieval and answers as knowledge.Answerer.
package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/textsplitters"
	"github.com/fsvxavier/golang-chain-eco/core/vectorstores"
)

// IndexStats counts chunks added, left untouched and removed by one Index call.
type IndexStats struct{ Added, Skipped, Removed int }

// sourceState remembers what was written for one source so that an unchanged
// source is skipped and a changed one can delete exactly its old chunks.
type sourceState struct {
	hash   string
	chunks []documents.Document // carry the IDs assigned by the store
}

// Indexer loads documents, splits them and writes the chunks to a vector store.
// Re-indexing skips sources whose content hash is unchanged.
// ponytail: hash state lives in memory only; a restart re-embeds everything. Persist it when corpora get big.
type Indexer struct {
	store vectorstores.VectorStore
	sp    textsplitters.Splitter

	mu     sync.Mutex
	states map[string]*sourceState
}

// NewIndexer returns an Indexer writing to store. A nil splitter means
// textsplitters.NewRecursive(800, 100).
func NewIndexer(store vectorstores.VectorStore, sp textsplitters.Splitter) *Indexer {
	if sp == nil {
		sp = textsplitters.NewRecursive(800, 100)
	}
	return &Indexer{store: store, sp: sp, states: map[string]*sourceState{}}
}

// Index loads l and brings the store up to date. Counts are in chunks.
// Documents are grouped by metadata["source"]; a group whose content hash is
// unchanged is skipped, a changed group has its old chunks deleted and is
// split and indexed again.
func (i *Indexer) Index(ctx context.Context, l documents.Loader) (IndexStats, error) {
	var st IndexStats
	if l == nil {
		return st, errors.New("rag: nil loader")
	}
	loaded, err := l.Load(ctx)
	if err != nil {
		return st, err
	}
	groups, keys := groupBySource(loaded)

	i.mu.Lock()
	defer i.mu.Unlock()
	for _, key := range keys {
		group := groups[key]
		hash := hashContents(group)
		old, seen := i.states[key]
		if seen && old.hash == hash {
			st.Skipped += len(old.chunks)
			continue
		}
		if seen {
			ids := make([]string, len(old.chunks))
			for n, c := range old.chunks {
				ids[n] = c.ID
			}
			if err := i.store.Delete(ctx, ids); err != nil {
				return st, err
			}
			st.Removed += len(ids)
			delete(i.states, key)
		}
		chunks := i.split(key, hash, group)
		if len(chunks) > 0 {
			ids, err := i.store.AddDocuments(ctx, chunks)
			if err != nil {
				return st, err
			}
			for n := range chunks {
				chunks[n].ID = ids[n]
			}
		}
		i.states[key] = &sourceState{hash: hash, chunks: chunks}
		st.Added += len(chunks)
	}
	return st, nil
}

// Documents returns every indexed chunk, ordered by source. It feeds the BM25 side of the pipeline.
func (i *Indexer) Documents() []documents.Document {
	i.mu.Lock()
	defer i.mu.Unlock()
	keys := make([]string, 0, len(i.states))
	for k := range i.states {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []documents.Document
	for _, k := range keys {
		out = append(out, i.states[k].chunks...)
	}
	return out
}

// split chunks one source group and adds provenance metadata: source, chunk
// index, content_hash (the splitter already sets start_index).
func (i *Indexer) split(source, hash string, group []documents.Document) []documents.Document {
	chunks := textsplitters.SplitDocuments(i.sp, group)
	for n := range chunks {
		chunks[n].ID = ""
		chunks[n].Metadata["source"] = source
		chunks[n].Metadata["chunk"] = n
		chunks[n].Metadata["content_hash"] = hash
	}
	return chunks
}

// groupBySource groups documents by metadata["source"], keeping first-seen
// order. A document without a source gets its own group keyed by position.
func groupBySource(docs []documents.Document) (map[string][]documents.Document, []string) {
	groups := map[string][]documents.Document{}
	var keys []string
	for n, d := range docs {
		key := fmt.Sprintf("#%d", n)
		if s, ok := d.Metadata["source"]; ok && s != nil {
			key = fmt.Sprint(s)
		}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], d)
	}
	return groups, keys
}

func hashContents(group []documents.Document) string {
	h := sha256.New()
	for _, d := range group {
		h.Write([]byte(d.PageContent))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
