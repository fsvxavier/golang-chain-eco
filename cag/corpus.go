package cag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/knowledge"
)

// Corpus is an immutable, deterministically ordered set of documents rendered
// as one text block. Equal documents always produce equal Text, whatever the
// input order, so the prompt prefix stays byte-stable for provider caches.
type Corpus struct {
	docs []documents.Document
	text string
	hash string
}

// NewCorpus copies docs and sorts them by metadata["source"], then ID, then content.
func NewCorpus(docs []documents.Document) *Corpus {
	sorted := slices.Clone(docs)
	slices.SortStableFunc(sorted, func(a, b documents.Document) int {
		if c := strings.Compare(sourceOf(a), sourceOf(b)); c != 0 {
			return c
		}
		if c := strings.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return strings.Compare(a.PageContent, b.PageContent)
	})
	var sb strings.Builder
	for i, d := range sorted {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "<document source=%q>\n%s\n</document>", sourceOf(d), d.PageContent)
	}
	text := sb.String()
	sum := sha256.Sum256([]byte(text))
	return &Corpus{docs: sorted, text: text, hash: hex.EncodeToString(sum[:])}
}

// sourceOf returns metadata["source"] as text, or "" when absent.
func sourceOf(d documents.Document) string {
	if v, ok := d.Metadata["source"]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// Text returns the corpus as <document source="...">...</document> blocks.
// A nil Corpus has empty text.
func (c *Corpus) Text() string {
	if c == nil {
		return ""
	}
	return c.text
}

// Hash returns the sha256 hex digest of Text.
func (c *Corpus) Hash() string {
	if c == nil {
		return ""
	}
	return c.hash
}

// Tokens estimates the size of Text with tc (knowledge.Heuristic when nil).
func (c *Corpus) Tokens(tc knowledge.TokenCounter) int {
	if tc == nil {
		tc = knowledge.Heuristic{}
	}
	return tc.Count(c.Text())
}

// Documents returns the sorted documents.
func (c *Corpus) Documents() []documents.Document {
	if c == nil {
		return nil
	}
	return c.docs
}
