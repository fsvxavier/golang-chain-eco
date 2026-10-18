// Package textsplitters splits long text into overlapping chunks
// (langchain_text_splitters).
package textsplitters

import (
	"maps"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
)

// Splitter splits text into chunks.
type Splitter interface {
	SplitText(text string) []string
}

// LengthFunc measures a chunk (defaults to rune count).
type LengthFunc func(string) int

func runeLen(s string) int { return len([]rune(s)) }

// Recursive is RecursiveCharacterTextSplitter: it tries separators in order,
// recursing into pieces that are still too long.
type Recursive struct {
	ChunkSize     int        // default 1000
	ChunkOverlap  int        // default 200
	Separators    []string   // default {"\n\n", "\n", " ", ""}
	Length        LengthFunc // default rune count
	KeepSeparator bool
}

// NewRecursive creates a recursive splitter.
func NewRecursive(size, overlap int) *Recursive {
	return &Recursive{ChunkSize: size, ChunkOverlap: overlap}
}

// Language separators for code-aware splitting (from_language).
var Language = map[string][]string{
	"go":       {"\nfunc ", "\nvar ", "\nconst ", "\ntype ", "\nif ", "\nfor ", "\nswitch ", "\ncase ", "\n\n", "\n", " ", ""},
	"python":   {"\nclass ", "\ndef ", "\n\tdef ", "\n\n", "\n", " ", ""},
	"markdown": {"\n# ", "\n## ", "\n### ", "\n#### ", "\n```\n", "\n\n***+\n", "\n\n", "\n", " ", ""},
	"js":       {"\nfunction ", "\nconst ", "\nlet ", "\nvar ", "\nclass ", "\nif ", "\nfor ", "\n\n", "\n", " ", ""},
}

// ForLanguage returns a recursive splitter using the language separators.
func ForLanguage(lang string, size, overlap int) *Recursive {
	r := NewRecursive(size, overlap)
	r.Separators = Language[lang]
	r.KeepSeparator = true
	return r
}

func (r *Recursive) defaults() (int, int, []string, LengthFunc) {
	size, overlap, seps, l := r.ChunkSize, r.ChunkOverlap, r.Separators, r.Length
	if size <= 0 {
		size = 1000
	}
	if overlap < 0 || overlap >= size {
		overlap = 0
	}
	if len(seps) == 0 {
		seps = []string{"\n\n", "\n", " ", ""}
	}
	if l == nil {
		l = runeLen
	}
	return size, overlap, seps, l
}

// SplitText implements Splitter.
func (r *Recursive) SplitText(text string) []string {
	size, overlap, seps, l := r.defaults()
	return r.split(text, seps, size, overlap, l)
}

func (r *Recursive) split(text string, seps []string, size, overlap int, l LengthFunc) []string {
	sep, rest := seps[len(seps)-1], []string(nil)
	for i, s := range seps {
		if s == "" || strings.Contains(text, s) {
			sep, rest = s, seps[i+1:]
			break
		}
	}
	pieces := splitKeep(text, sep, r.KeepSeparator)
	joiner := sep
	if r.KeepSeparator {
		joiner = ""
	}
	var out, good []string
	for _, p := range pieces {
		if l(p) < size {
			good = append(good, p)
			continue
		}
		if len(good) > 0 {
			out = append(out, mergeSplits(good, joiner, size, overlap, l)...)
			good = nil
		}
		if len(rest) == 0 {
			out = append(out, p)
		} else {
			out = append(out, r.split(p, rest, size, overlap, l)...)
		}
	}
	if len(good) > 0 {
		out = append(out, mergeSplits(good, joiner, size, overlap, l)...)
	}
	return out
}

func splitKeep(text, sep string, keep bool) []string {
	var parts []string
	if sep == "" {
		for _, r := range text {
			parts = append(parts, string(r))
		}
		return parts
	}
	if !keep {
		for _, p := range strings.Split(text, sep) {
			if p != "" {
				parts = append(parts, p)
			}
		}
		return parts
	}
	raw := strings.Split(text, sep)
	for i, p := range raw {
		if i > 0 {
			p = sep + p
		}
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// mergeSplits greedily packs splits into chunks up to size, carrying an
// overlap window between chunks (TextSplitter._merge_splits).
func mergeSplits(splits []string, sep string, size, overlap int, l LengthFunc) []string {
	sepLen := l(sep)
	var docs, cur []string
	total := 0
	for _, s := range splits {
		n := l(s)
		extra := 0
		if len(cur) > 0 {
			extra = sepLen
		}
		if total+n+extra > size && len(cur) > 0 {
			if d := strings.TrimSpace(strings.Join(cur, sep)); d != "" {
				docs = append(docs, d)
			}
			for total > overlap || (total+n+sepLen > size && total > 0) {
				drop := l(cur[0])
				if len(cur) > 1 {
					drop += sepLen
				}
				total -= drop
				cur = cur[1:]
				if len(cur) == 0 {
					total = 0
					break
				}
			}
		}
		if len(cur) > 0 {
			total += sepLen
		}
		cur = append(cur, s)
		total += n
	}
	if d := strings.TrimSpace(strings.Join(cur, sep)); d != "" {
		docs = append(docs, d)
	}
	return docs
}

// Character splits on a single separator then merges (CharacterTextSplitter).
type Character struct {
	Separator    string // default "\n\n"
	ChunkSize    int
	ChunkOverlap int
	Length       LengthFunc
}

// SplitText implements Splitter.
func (c *Character) SplitText(text string) []string {
	r := &Recursive{ChunkSize: c.ChunkSize, ChunkOverlap: c.ChunkOverlap, Length: c.Length}
	size, overlap, _, l := r.defaults()
	sep := c.Separator
	if sep == "" {
		sep = "\n\n"
	}
	return mergeSplits(splitKeep(text, sep, false), sep, size, overlap, l)
}

// SplitDocuments splits each document, copying metadata to every chunk and
// recording "start_index" when the chunk is found verbatim.
func SplitDocuments(s Splitter, docs []documents.Document) []documents.Document {
	var out []documents.Document
	for _, d := range docs {
		offset := 0
		for _, chunk := range s.SplitText(d.PageContent) {
			md := maps.Clone(d.Metadata)
			if md == nil {
				md = map[string]any{}
			}
			if i := strings.Index(d.PageContent[offset:], chunk); i >= 0 {
				md["start_index"] = offset + i
				offset += i
			}
			out = append(out, documents.Document{PageContent: chunk, Metadata: md})
		}
	}
	return out
}

// MarkdownHeaders splits markdown on headers and records the header path in
// metadata (MarkdownHeaderTextSplitter). Headers maps a prefix such as "##"
// to the metadata key, e.g. {"#": "h1", "##": "h2"}.
func MarkdownHeaders(text string, headers map[string]string) []documents.Document {
	var out []documents.Document
	cur := map[string]any{}
	var buf []string
	flush := func() {
		if body := strings.TrimSpace(strings.Join(buf, "\n")); body != "" {
			out = append(out, documents.Document{PageContent: body, Metadata: maps.Clone(cur)})
		}
		buf = nil
	}
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}
		matched := false
		if !inFence {
			for prefix, key := range headers {
				if strings.HasPrefix(trimmed, prefix+" ") {
					flush()
					level := len(prefix)
					for p, k := range headers {
						if len(p) >= level {
							delete(cur, k)
						}
					}
					cur[key] = strings.TrimSpace(trimmed[len(prefix):])
					matched = true
					break
				}
			}
		}
		if !matched {
			buf = append(buf, line)
		}
	}
	flush()
	return out
}
